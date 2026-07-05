# 面试题库运营动作手工队列

## 目标

让管理员把真实检索运营、健康诊断或人工判断中发现的问题，先沉淀为可跟进的题库运营动作，补齐“看到问题”到“进入处理队列”的首个闭环切片。

## 修改范围

- 后端新增 `InterviewBankOpsAction` 领域类型、过滤条件和枚举校验。
- Store 接口新增运营动作创建与列表读取，MemoryStore 与 PostgresStore 同步实现。
- 数据库 schema 与初始化 migration 新增 `interview_bank_ops_actions` 主表和必要索引。
- 管理员 API 新增手工创建动作与动作列表接口。
- 前端面试题库管理页新增“运营动作”面板，支持 open 队列展示、手工创建、套用组合筛选和打开关联原子。

## 核心实现

- `POST /api/v1/admin/interview-bank/ops-actions` 仅允许管理员调用，创建时固定 `source=manual`，默认 `status=open`，并记录 `created_by`。
- 创建请求校验标题、原因、动作类型、优先级、来源、状态和目标范围，后端保证 `dedupe_key` 非空。
- `GET /api/v1/admin/interview-bank/ops-actions` 支持按状态、类型、优先级、来源、组合、难度和 atom id 过滤，并按更新时间倒序返回。
- PostgresStore 将 `evidence` 作为 JSON 保存；MemoryStore 返回 clone，避免调用方修改内部状态。
- 前端面板复用现有题库筛选和题目详情能力，不新增自动修题、自动归档或自动重建索引。

## 影响范围

- 新增能力只在管理员面试题库页面可见，普通用户、讲师和学生侧流程不受影响。
- 本切片不改变真实面试运行时、报告接口、题库编辑接口或索引重建行为。
- 新增表为空时不会影响既有题库导入、健康诊断、检索预览和真实检索运营面板。

## 验证方式

- `cd backend; go test ./internal/httpapi ./internal/store`
- `cd backend; go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- Chrome 真实页面验证：管理员进入 `http://localhost:5173/interview-bank`，创建手工运营动作后 open 队列显示 `1 个 open` 并回显标题、类型、优先级、组合和原因。

## 已知限制

- 首期只支持手工创建和列表读取，不做候选生成、动作详情、状态流转、历史记录或 active dedupe 拦截。
- 动作不会自动修改题库内容、归档资源或触发索引重建。
- 本地 Docker Hub 不可达时，需要使用本机 Go 构建后的本地 API 镜像启动容器；源码级 Dockerfile 仍依赖远端基础镜像。
