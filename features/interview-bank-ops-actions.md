# 面试题库运营动作队列、候选预览、详情联动与状态闭环

## 目标

让管理员把真实检索运营、健康诊断、索引状态或人工判断中发现的问题，沉淀为可跟进的题库运营动作；系统可从健康诊断、索引状态和真实检索运营聚合生成候选预览，管理员显式勾选后保存为正式 open 动作，可打开动作详情查看证据与当前 atom 状态，并能在详情内完成处理中、观察中、已解决、已忽略和已重开闭环。

## 修改范围

- 后端新增 `InterviewBankOpsAction` 领域类型、过滤条件和枚举校验。
- 后端新增 `InterviewBankOpsActionCandidate` 候选类型、生成请求与响应契约。
- Store 接口新增运营动作创建与列表读取，MemoryStore 与 PostgresStore 同步实现。
- Store 接口新增运营动作按 id 读取，供动作详情复用。
- Store 接口新增运营动作状态更新与历史读取，MemoryStore 与 PostgresStore 同步实现。
- 数据库 schema 与初始化 migration 新增 `interview_bank_ops_actions` 主表和必要索引。
- 数据库 schema 与初始化 migration 新增 `interview_bank_ops_action_history` 历史表和索引。
- 管理员 API 新增手工创建动作与动作列表接口。
- 管理员 API 新增动作详情接口，返回 compact evidence 与当前关联 atom 轻量上下文。
- 管理员 API 新增动作状态更新接口，要求关闭/忽略备注并回写历史。
- 管理员 API 新增健康诊断/索引状态/真实检索运营候选生成接口。
- 管理员 API 新增候选保存接口，将选中 generated 候选落库为 open 运营动作。
- 前端面试题库管理页新增“运营动作”面板与“动作详情”面板，支持 open 队列展示、手工创建、套用组合筛选、打开关联原子和打开动作详情。
- 前端 API client/type 新增候选生成、候选保存与动作详情契约；运营动作面板支持生成候选、默认全选、取消/全选和保存选中候选，详情面板支持查看证据和从 `rebuild_index + atom_id` 动作直接走现有单 atom 重建路径。

## 核心实现

- `POST /api/v1/admin/interview-bank/ops-actions` 仅允许管理员调用，创建时固定 `source=manual`，默认 `status=open`，并记录 `created_by`。
- 创建请求校验标题、原因、动作类型、优先级、来源、状态和目标范围，后端保证 `dedupe_key` 非空。
- `GET /api/v1/admin/interview-bank/ops-actions` 支持按状态、类型、优先级、来源、组合、难度和 atom id 过滤，并按更新时间倒序返回。
- `GET /api/v1/admin/interview-bank/ops-actions/{id}` 仅允许管理员调用，返回动作本身、compact evidence、当前关联 atom 的 `status/vector_status/current_version/updated_at`，并在 atom 缺失或已归档时标记 `stale=true`。
- `PATCH /api/v1/admin/interview-bank/ops-actions/{id}` 仅允许管理员调用，支持 `in_progress`、`watching`、`resolved`、`dismissed`、`reopened` 状态流转；`resolved/dismissed` 必须填写备注；每次状态变更都会追加一条独立历史记录。
- `POST /api/v1/admin/interview-bank/ops-actions/candidates` 只允许管理员调用，从健康诊断、索引状态和真实检索运营聚合生成候选预览，不写入动作表。
- 健康诊断 `blocked` 组合生成 `fill_gap/P0` 候选；`warning` 组合生成组合级 `rebuild_index` 候选，索引失败优先级 `P1`，仅 pending 优先级 `P2`。
- `published + failed` atom 生成 `rebuild_index/P1` 候选，`published + pending` atom 生成 `rebuild_index/P2` 候选；draft/archived atom 不生成普通索引候选。
- 真实检索回退组合生成 `retrieval_analytics + fill_gap` 候选，回退次数 `>=3` 为 `P0`，低于 3 次为 `P1`，证据只保留回退次数、最近原因摘要、窗口总量和回退率。
- 真实检索低命中聚合仅对 `hit_count=0` 的 published followup/mixed atom 生成 `observe/P3` 候选，已命中 atom 不生成候选，也不自动生成归档建议。
- 候选生成按 active 动作状态的 `dedupe_key` 去重，`resolved/dismissed` 不阻止后续重新生成。
- `POST /api/v1/admin/interview-bank/ops-actions/candidates/save` 只允许管理员调用，候选数量必须在 1 到 50 之间。
- 候选保存只接受 `health_diagnostic`、`index_status`、`retrieval_analytics` 三类 generated source；拒绝 `manual`、`retrieval_log` 和非法枚举。
- 保存时固定 `status=open`、`created_by=adminID`，保留候选 `dedupe_key`、目标范围与 compact evidence。
- 保存前读取 active 动作 key，已存在 active key 或同请求重复 `dedupe_key` 的候选会跳过并计入 `skipped_existing`；`resolved/dismissed` 不阻止未来保存同 key 候选。
- `reopened` 会重新回到 active 集合；如果当前已有别的 active 动作占用同一个 `dedupe_key`，后端拒绝重开，避免 active 队列重复。
- PostgresStore 将 `evidence` 作为 JSON 保存；MemoryStore 返回 clone，避免调用方修改内部状态。
- 动作详情面板只聚合信息和按钮，不新增第二套治理接口：打开原子详情复用现有 atom detail，套用复用现有筛选/检索预览，`rebuild_index + atom_id` 复用现有索引重建 API。
- 前端面板新增动作状态过滤，默认展示 open 队列；详情面板新增备注输入、状态按钮和历史列表，候选保存成功后刷新 open 队列并清空本次候选；动作详情中的关联 atom 在编辑、归档、恢复或重建后会刷新显示最新状态，不新增自动修题、自动归档或自动重建索引。

## 影响范围

- 新增能力只在管理员面试题库页面可见，普通用户、讲师和学生侧流程不受影响。
- 本切片不改变真实面试运行时、报告接口、题库编辑接口或索引重建行为。
- 候选生成是 admin-only 只读能力，不调用 LLM、embedding 或向量检索，也不修改 atom、版本、索引状态或检索日志。
- 候选保存是 admin-only 显式写路径，只创建治理动作，不修改 atom、版本、索引状态或检索日志。
- 动作详情读取是 admin-only 只读能力，只补充当前关联 atom 的轻量上下文，不返回完整 atom 正文、原理、误区、追问路径或版本快照。
- 动作状态更新是 admin-only 显式写路径，只修改动作当前状态和历史记录，不自动变更题库内容、版本、索引状态或检索日志。
- 新增表为空时不会影响既有题库导入、健康诊断、检索预览和真实检索运营面板。

## 验证方式

- `cd backend; go test ./internal/httpapi ./internal/store`
- `cd backend; go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- Chrome 真实页面验证：管理员在动作详情中可成功套用组合目标到筛选/检索预览，并能从 atom 类动作跳转到现有题目详情面板。
- Chrome 真实页面验证：管理员可在详情中把动作标记为已解决，看到历史记录新增；关闭/忽略不填备注时前端会先拦截。
- Chrome 真实页面验证：已解决动作重新生成候选后可再次保存；列表状态过滤能查到 `resolved` 与 `reopened` 动作。
- 窄屏 smoke：390px 宽度下页面无横向溢出，运营动作列表、空候选态和详情面板可同时渲染。
- Chrome 真实页面验证：管理员进入 `http://localhost:5173/interview-bank`，创建手工运营动作后 open 队列显示 `1 个 open` 并回显标题、类型、优先级、组合和原因。
- Chrome 真实页面验证：管理员点击“生成候选”，勾选候选并“保存选中”，成功提示显示 saved/skipped existing，open 队列刷新后出现保存的 generated source 动作。
- Chrome 真实页面验证：管理员点击 open 队列里的“详情”，可以看到证据快照、当前 atom 状态/索引状态/版本；`rebuild_index + atom_id` 动作可以直接触发一次现有单 atom 索引重建。

### 2026-07-14 浏览器冒烟记录

- 使用本机浏览器 1920×1080 访问 `http://localhost:5173/interviews`，管理员会话可正常进入面试舱，并可导航到面试题库。
- 面试题库列表、健康诊断、运营动作列表和题目资源面板均可渲染；在 390px 移动视口模拟下，`document.documentElement.scrollWidth === window.innerWidth`，未发现横向溢出。
- 当前 `5173` 前端仍指向 `http://localhost:8080/api/v1` 的旧 Docker API；点击运营动作“详情”返回 `not found`，因此详情证据、状态闭环、单 atom 重建等真实写操作尚未判定通过。最新源码后端已在 `18080` 监听，但当前前端实例未切换到该 API。

## 已知限制

- 当前支持手工创建、列表读取、动作详情、状态流转、动作历史、健康诊断候选、索引状态候选、真实检索运营候选和候选选择保存。
- 动作不会自动修改题库内容、归档资源或触发索引重建。
- 组合级 `rebuild_index` 动作当前只支持套用到现有筛选，不提供新的批量自动重建入口。
- active dedupe 目前覆盖候选生成、候选保存和动作重开；并发竞态仍主要靠接口时序和当前 Store 读写顺序兜底，数据库层尚未增加唯一活动约束。
- 本地 Docker Hub 不可达时，需要使用本机 Go 构建后的本地 API 镜像启动容器；源码级 Dockerfile 仍依赖远端基础镜像。
