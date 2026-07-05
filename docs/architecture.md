# 架构说明

## 目标

本文档记录当前项目中已经稳定下来的后端架构边界、HTTP API 入口分工和关键安全约束。

## 后端分层

- `backend/cmd/server`
  负责读取环境变量、组装依赖、校验 `JWT_SECRET`、启动 HTTP 服务。
- `backend/internal/httpapi`
  负责 HTTP 路由、认证校验、限流、请求解码和响应编码，以及调用领域逻辑与存储层。
- `backend/internal/ai`
  负责大模型路由、输出校验、安全检查、场景生成和面试反馈调用。
- `backend/internal/auth`
  负责密码哈希、JWT 颁发与校验。
- `backend/internal/store`
  负责用户、场景、面试、AI job、资产、审计事件等数据读写。
- `backend/internal/domain`
  负责领域对象定义和跨层共享结构。

## `httpapi` 模块边界

`backend/internal/httpapi` 已从单一巨石 `server.go` 拆成“入口 + 领域文件”的结构：

- `server.go`
  保留 `Server`、根路由 `Handler()`、鉴权入口、限流、审计、通用 HTTP 编解码和少量跨领域基础工具。
- `handlers_auth.go`
  负责 `/auth/*` 与 `/users/me/*`。
- `handlers_ai.go`
  负责 `/ai/*` 安全检查与 AI job 查询、取消、事件流。
- `handlers_assets.go`
  负责 `/assets/*` 元数据创建、文件上传和内容读取。
- `handlers_scenarios.go`
  负责 `/scenarios/*`、场景会话、场景问答处理。
- `handlers_interviews.go`
  负责 `/interviews/*`、面试提交、语音转写确认和面试报告。
- `handlers_community.go`
  负责 `/community/*`、UGC 草稿流转、讲师初审、管理员终审。
- `handlers_admin.go`
  负责 `/admin/*` 和系统状态汇总。
- `scenario_generation.go`
  负责场景生成 payload、约束校验、AI job 创建与执行。
- `community_helpers.go`
  负责社区可见性、帖子转场景、内容清洗与审核辅助。
- `interview_voice.go`
  负责面试语音答案校验、语言检测、术语提示。
- `stt.go`
  负责 STT provider、mock provider 和错误映射。
- `assets_helpers.go`
  负责资产路径、URL、语音文件校验与资产补全。
- `learning.go`
  负责学习计划、推荐和复习日历。
- `views.go`
  负责场景、面试、社区等对外视图映射和权限视图裁剪。
- `sse.go`
  负责 SSE 输出封装和流式展示辅助。
- `helpers.go`
  负责无状态通用小工具。

## 请求处理流程

1. `Handler()` 按路径分发到领域 handler。
2. 需要登录的路径统一经 `withUser()` 做 JWT 校验与用户装载。
3. 需要限流的路径通过 `allow()` 或 `allowAI()` 进行请求配额控制。
4. 领域 handler 调用 `store`、`ai`、`auth`、`assets` 等依赖完成业务处理。
5. 统一使用 `writeOK()`、`writeError()` 返回 JSON 响应，流式输出走 `sse.go`。

## 面试题库版本治理数据层

面试题库治理采用“主表保存当前生效内容，版本表保存历史快照”的数据边界：

- `backend/internal/domain/interview_bank.go`
  定义 `InterviewKnowledgeAtom`、`InterviewKnowledgeAtomVersion`、`InterviewKnowledgeBatch`、`InterviewRetrievalLog`、`InterviewBankOpsAction` 等题库治理领域对象。
- `backend/internal/store`
  通过 `SaveInterviewKnowledgeAtomVersioned` 统一写入导入、重复导入、在线编辑、归档和恢复归档产生的版本事件；MemoryStore 与 PostgresStore 必须保持同一版本推进口径。
- `interview_knowledge_atoms`
  保存题目当前版本内容、发布状态、`current_version` 和运行时索引状态。
- `interview_knowledge_atom_versions`
  保存完整标准化内容快照、版本类型、操作者、变更备注、`diff_summary` 和 `no_content_change`。
- `interview_sessions.question_snapshot`
  保存创建会话时的开场题快照，避免后续改题污染历史面试。
- `interview_sessions.selected_atom_snapshots`
  保存追问命中的轻量知识原子快照，只保留元数据，不保存大段正文。
- `interview_retrieval_logs`
  保存真实面试追问检索的轻量运营日志，只记录 `session_id`、轮次、脱敏截断 query、命中原子快照、回退状态、错误摘要和创建时间，用于管理员运营看板聚合命中率、回退率和低效资源。
- `interview_bank_ops_actions`
  保存管理员需要跟进的题库运营动作，记录动作类型、状态、优先级、来源、去重键、标题、原因、关联组合、关联原子、轻量证据和创建人；已支持管理员手工创建与 open 队列读取，并可从健康诊断、索引状态和真实检索运营聚合生成只读候选预览。候选生成不落库，也不自动修题、归档或重建索引。

版本快照只包含稳定内容字段：`id`、`title`、`subject`、`domain`、`difficulty`、`category`、`question_role`、`sourceRef`、`tags`、`principles`、`pitfalls`、`followUpPaths`、`status`。`vector_status` 和 `last_indexed_at` 属于运行时索引状态，不进入版本快照。

## 面试题库治理管理端

面试题库管理端挂在现有管理员路由下，复用 `/api/v1/admin` 的 admin-only 权限边界：

- `backend/internal/httpapi/handlers_interview_bank.go`
  负责 `/admin/interview-bank/*` 的摘要、列表、批次、导入校验、发布和索引重建。
- `GET /api/v1/admin/interview-bank/summary`
  返回题库资源数、发布数、批次数、开放组合数和索引状态摘要。
- `GET /api/v1/admin/interview-bank/health`
  返回按 `domain + category + difficulty` 聚合的题库健康诊断，区分可开放、告警和阻断组合，并暴露开场题、追问题、已索引追问资源、待索引和索引失败数量。
- `GET /api/v1/admin/interview-bank/atoms`
  支持按 `status`、`domain`、`difficulty`、`category`、`question_role`、`vector_status` 筛选当前题库资源。
- `GET /api/v1/admin/interview-bank/atoms/{id}`
  返回单条题库资源详情，供管理员在线查看与编辑。
- `GET /api/v1/admin/interview-bank/atoms/{id}/versions`
  返回单题版本历史，默认按最新版本优先展示。
- `PATCH /api/v1/admin/interview-bank/atoms/{id}`
  支持管理员在线编辑已发布题库资源内容；请求必须携带 `base_version` 与 `change_note`，保存时复用 `SaveInterviewKnowledgeAtomVersioned` 写入 `manual_edit` 版本，并将 `vector_status` 置为 `pending` 等待手动重建索引。
- `POST /api/v1/admin/interview-bank/atoms/{id}/archive`
  支持管理员归档题库资源；请求必须携带非空 `reason`，保存时写入 `archive` 版本，并清理该 atom 的题库向量文档，归档题不会进入后续新面试或追问检索。
- `POST /api/v1/admin/interview-bank/atoms/{id}/restore`
  支持管理员将归档题恢复为 `published`；恢复前复用题库硬校验，通过后写入 `restore_archived` 版本，并将 `vector_status` 置为 `pending` 等待手动重建索引。
- `GET /api/v1/admin/interview-bank/batches`
  返回最近导入批次，供前端展示导入历史。
- `POST /api/v1/admin/interview-bank/import/validate`
  只做结构化导入包校验和预览，不写入题库主表、版本表或批次表。
- `POST /api/v1/admin/interview-bank/import/publish`
  复用同一套校验逻辑，通过后调用 `SaveInterviewKnowledgeAtomVersioned` 写入 atom 与版本，再保存导入批次。
- `POST /api/v1/admin/interview-bank/index/rebuild`
  触发 admin-only 的同步限量索引重建，支持按 `atom_ids` 精确重建，或按 `vector_status=pending|failed|pending_failed` 选择候选资源。
- `POST /api/v1/admin/interview-bank/retrieval-preview`
  支持管理员按 `domain + category + difficulty` 输入模拟检索文本预览追问召回结果；该接口复用题库向量检索边界，只读取 `published + indexed + followup/mixed` 资源，不创建面试会话、不写正式检索日志、不推进版本或修改索引状态。
- `GET /api/v1/admin/interview-bank/retrieval-logs`
  返回最近真实追问检索日志，支持 `domain`、`category`、`difficulty`、`fallback_used` 和 `limit` 过滤；响应只包含脱敏截断后的 query、轻量命中原子和回退摘要。
- `GET /api/v1/admin/interview-bank/retrieval-analytics`
  从有限窗口内聚合真实检索次数、命中率、回退率、热门命中原子、低/未命中原子、回退组合排行和最近回退原因，供管理端题库运营面板使用。
- `GET /api/v1/admin/interview-bank/ops-actions`
  返回题库运营动作列表，支持按 `status`、`action_type`、`priority`、`source`、`domain`、`category`、`difficulty`、`atom_id` 和 `limit` 过滤；前端首期默认展示 open 队列。
- `POST /api/v1/admin/interview-bank/ops-actions/candidates`
  从题库健康诊断、atom 索引状态和真实检索运营聚合生成运营动作候选预览；`blocked` 组合生成 `fill_gap` 候选，`warning` 组合和 `published + pending/failed` atom 生成 `rebuild_index` 候选，真实回退组合生成 `retrieval_analytics + fill_gap/P0/P1` 候选，真实检索窗口内零命中的 published followup/mixed atom 生成 `observe/P3` 候选，并按 active 动作 `dedupe_key` 去重。该接口只读，不写动作表、不调用 LLM/embedding、不修改题库或索引状态。
- `POST /api/v1/admin/interview-bank/ops-actions`
  支持管理员手工创建题库运营动作；后端固定写入 `source=manual`、默认 `status=open`，并保存非空 `dedupe_key` 和轻量证据，不隐式修改题库内容或索引状态。

题库向量索引采用独立的 `interview_knowledge_vector_documents` 表，不复用场景题的 `scenario_vector_documents`，避免 `question_id` / `source_version` 语义混用。重建接口只对 `status=published` 的 atom 调用 embedding 并写入向量文档；`draft` / `archived` 被请求重建时会删除旧向量文档并返回 skipped，不进入可检索索引。成功重建会将 `vector_status` 更新为 `indexed` 并写入 `last_indexed_at`；失败只将 `vector_status` 更新为 `failed`，不覆盖上一次成功索引时间，也不新增内容版本。导入发布链路仍不自动触发 embedding，避免发布接口受外部 provider 可用性影响。

## 面试运行时动态追问

面试运行时在旧题库兼容链路上接入正式题库原子，但保持“开场题选择”和“追问增强”两个阶段分离：

- 创建面试会话仍以启动轨道的 `domain`、`difficulty`、`question_type` 决定开场题；优先选择 `status=published` 且 `question_role=opening|mixed` 的题库原子，未命中时回退旧 `InterviewQuestion`。
- 会话级输入首期只包含 `difficulty_level`、`focus_areas[]`、`setup_notes`，不参与开场题选择，只进入追问检索和反馈生成。
- 长期个人档案只保存 `resume_summary` 与 `project_summary`，前端可把它们合成为本场 `setup_notes` 的默认输入，但不会把本场输入回写到个人档案。
- Agent 评分链路在五维评分之后增加追问检索步骤：检索 `followup|mixed` 且 `vector_status=indexed` 的题库原子；检索不可用、未命中或索引未就绪时回退规则追问，面试流程不中断。
- 每次真实追问检索结束后写入 `InterviewRetrievalLog`，命中和回退路径都记录；写入失败只降级记录内部错误，不中断面试提交。日志 query 使用脱敏逻辑并截断到 500 字，不保存用户身份、完整回答、完整简历或项目背景。
- 面试报告展示聚合摘要、每轮 `subject`、`fallback_used`、`follow_up_type`，并基于会话评价数据生成知识点覆盖分布与规则复训建议；不展示原子正文、内部检索 query、命中片段、管理端标题细节或 selected atom 快照。复训建议首期只作为报告展示内容，不自动写入长期学习计划或个人画像。

## 当前安全约束

- 密码哈希使用 bcrypt，兼容旧 SHA-256 演示数据登录。
- 登录成功时，如果检测到旧 SHA-256 密码 hash，会透明升级为 bcrypt。
- 持久化模式下必须显式提供安全的 `JWT_SECRET`；开发内存模式允许生成临时随机密钥。
- JWT claims 带内部 `token_version`；改密会递增版本，旧 access/refresh token 会同步失效。
- 登录限流只统计失败请求，避免成功登录被错误计数。
- `/api/v1/auth/password-reset` 不再默认开放；仅内存模式且显式设置 `ENABLE_ANON_PASSWORD_RESET=true` 时允许使用，持久化模式下始终禁用。
- `/users/me/password` 改密成功后会返回新 token，前端请求层会在 401 时自动 refresh 一次并重试。
- HTTP 服务保持 `WriteTimeout=0` 以兼容 SSE，但显式设置 `ReadTimeout`、`ReadHeaderTimeout` 和 `IdleTimeout`，避免慢请求长期占用连接。
- CORS 默认只允许本地前端开发源（`localhost/127.0.0.1/0.0.0.0:5173` 和 `:4173`）；如需其他浏览器源访问，必须显式设置 `CORS_ALLOWED_ORIGINS`。
- `/api/v1/system/ai` 继续允许匿名读取，但只暴露登录页和侧栏需要的非敏感路由状态；完整系统状态仍留在管理员受保护接口后面。

## 当前稳定边界

- `server.go` 负责“路由与横切能力”，不再承载完整业务实现。
- 领域路由的新增或修改，优先放入对应 `handlers_*.go`。
- 与单一业务域强相关的辅助函数，应优先放到对应领域文件，而不是回流到 `server.go`。
