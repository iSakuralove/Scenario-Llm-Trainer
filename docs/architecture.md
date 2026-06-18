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
  定义 `InterviewKnowledgeAtom`、`InterviewKnowledgeAtomVersion`、`InterviewKnowledgeBatch`、`InterviewRetrievalLog` 等题库治理领域对象。
- `backend/internal/store`
  通过 `SaveInterviewKnowledgeAtomVersioned` 统一写入导入、重复导入、在线编辑和恢复归档产生的版本事件；MemoryStore 与 PostgresStore 必须保持同一版本推进口径。
- `interview_knowledge_atoms`
  保存题目当前版本内容、发布状态、`current_version` 和运行时索引状态。
- `interview_knowledge_atom_versions`
  保存完整标准化内容快照、版本类型、操作者、变更备注、`diff_summary` 和 `no_content_change`。
- `interview_sessions.question_snapshot`
  保存创建会话时的开场题快照，避免后续改题污染历史面试。
- `interview_sessions.selected_atom_snapshots`
  保存追问命中的轻量知识原子快照，只保留元数据，不保存大段正文。

版本快照只包含稳定内容字段：`id`、`title`、`subject`、`domain`、`difficulty`、`category`、`question_role`、`sourceRef`、`tags`、`principles`、`pitfalls`、`followUpPaths`、`status`。`vector_status` 和 `last_indexed_at` 属于运行时索引状态，不进入版本快照。

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
