# 面试题库运营动作手工队列与候选预览

## 目标

让管理员把真实检索运营、健康诊断、索引状态或人工判断中发现的问题，沉淀为可跟进的题库运营动作，并先从健康诊断和索引状态生成只读候选预览。

## 修改范围

- 后端新增 `InterviewBankOpsAction` 领域类型、过滤条件和枚举校验。
- 后端新增 `InterviewBankOpsActionCandidate` 候选类型、生成请求与响应契约。
- Store 接口新增运营动作创建与列表读取，MemoryStore 与 PostgresStore 同步实现。
- 数据库 schema 与初始化 migration 新增 `interview_bank_ops_actions` 主表和必要索引。
- 管理员 API 新增手工创建动作与动作列表接口。
- 管理员 API 新增健康诊断/索引状态候选生成接口。
- 前端面试题库管理页新增“运营动作”面板，支持 open 队列展示、手工创建、套用组合筛选和打开关联原子。
- 前端 API client/type 新增候选生成契约，完整候选保存 UI 留到后续切片。

## 核心实现

- `POST /api/v1/admin/interview-bank/ops-actions` 仅允许管理员调用，创建时固定 `source=manual`，默认 `status=open`，并记录 `created_by`。
- 创建请求校验标题、原因、动作类型、优先级、来源、状态和目标范围，后端保证 `dedupe_key` 非空。
- `GET /api/v1/admin/interview-bank/ops-actions` 支持按状态、类型、优先级、来源、组合、难度和 atom id 过滤，并按更新时间倒序返回。
- `POST /api/v1/admin/interview-bank/ops-actions/candidates` 只允许管理员调用，从健康诊断和索引状态生成候选预览，不写入动作表。
- 健康诊断 `blocked` 组合生成 `fill_gap/P0` 候选；`warning` 组合生成组合级 `rebuild_index` 候选，索引失败优先级 `P1`，仅 pending 优先级 `P2`。
- `published + failed` atom 生成 `rebuild_index/P1` 候选，`published + pending` atom 生成 `rebuild_index/P2` 候选；draft/archived atom 不生成普通索引候选。
- 候选生成按 active 动作状态的 `dedupe_key` 去重，`resolved/dismissed` 不阻止后续重新生成。
- PostgresStore 将 `evidence` 作为 JSON 保存；MemoryStore 返回 clone，避免调用方修改内部状态。
- 前端面板复用现有题库筛选和题目详情能力，不新增自动修题、自动归档或自动重建索引。

## 影响范围

- 新增能力只在管理员面试题库页面可见，普通用户、讲师和学生侧流程不受影响。
- 本切片不改变真实面试运行时、报告接口、题库编辑接口或索引重建行为。
- 候选生成是 admin-only 只读能力，不调用 LLM、embedding 或向量检索，也不修改 atom、版本、索引状态或检索日志。
- 新增表为空时不会影响既有题库导入、健康诊断、检索预览和真实检索运营面板。

## 验证方式

- `cd backend; go test ./internal/httpapi ./internal/store`
- `cd backend; go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- Chrome 真实页面验证：管理员进入 `http://localhost:5173/interview-bank`，创建手工运营动作后 open 队列显示 `1 个 open` 并回显标题、类型、优先级、组合和原因。

## 已知限制

- 当前支持手工创建、列表读取、健康诊断候选和索引状态候选；仍不做候选保存、动作详情、状态流转、历史记录或真实检索运营候选。
- 动作不会自动修改题库内容、归档资源或触发索引重建。
- 本地 Docker Hub 不可达时，需要使用本机 Go 构建后的本地 API 镜像启动容器；源码级 Dockerfile 仍依赖远端基础镜像。
