# 设计：面试题库治理管理端 MVP

## 架构边界

本任务在现有单体运行时内实现能力迁移，不引入 AI-Interview 参考仓库的 Spring Boot、Vue、MySQL 或 Qdrant 运行时。

后端边界：

- HTTP API 继续放在 `backend/internal/httpapi`。
- 领域对象继续使用 `backend/internal/domain/interview_bank.go`。
- 持久化继续通过 `backend/internal/store` 的 Store 接口隔离 MemoryStore / PostgresStore。
- 管理端权限复用现有 `handleAdmin` 的 `RoleAdmin` 校验。

前端边界：

- 管理页面放在 `frontend/src/features` 下的新题库治理模块。
- 路由挂在 admin 可见导航下，使用现有 `RoleGuard`。
- 用户侧 `InterviewsPage` 不承载导入、发布、审计、版本等治理动作。

## 数据流

1. 管理员进入“面试题库”页面。
2. 前端读取题库摘要、批次摘要和题库列表。
3. 管理员提交结构化 JSON 导入包。
4. 后端校验字段完整性、受控枚举、核心内容数量、稳定 ID 和批内重复。
5. 预览接口只返回统计和逐条结果，不写 Store。
6. 发布接口复用同一套校验，通过后写入批次记录和题库资源。
7. 每条题库资源写入必须调用版本化保存逻辑，保留版本历史。
8. 系统状态页读取治理摘要，用于展示题库资源数量、批次数、失败索引数等。

## API 草案

路径建议复用当前前端已有 `/admin/*` 风格：

- `GET /api/v1/admin/interview-bank/summary`
- `GET /api/v1/admin/interview-bank/atoms`
- `GET /api/v1/admin/interview-bank/batches`
- `POST /api/v1/admin/interview-bank/import/validate`
- `POST /api/v1/admin/interview-bank/import/publish`

导入包最小字段：

- `batch_id` 可选，缺省由后端生成。
- `source_ref` 可选，用于来源追踪。
- `items[]` 必填，每项映射到 `InterviewKnowledgeAtom`。

每项必填字段：

- `id`
- `title`
- `subject`
- `category`
- `domain`
- `difficulty`
- `question_role`
- `principles`
- `pitfalls`
- `follow_up_paths`

## 校验规则

- `id` 必须稳定、非空、批内唯一。
- `subject` 必须非空，表示考察点标题。
- `domain`、`difficulty`、`question_role` 使用受控范围。
- `principles` 至少 2 条。
- `pitfalls` 至少 2 条。
- `follow_up_paths` 至少 2 条。
- `tags` 做去空格、去重和空值过滤。
- 同 ID 已存在时进入更新路径，不创建重复题目。
- 内容完全一致的重复导入仍保留版本记录，版本类型为 duplicate import。

## 兼容与迁移

- MemoryStore 和 PostgresStore 都要实现新增列表、批次和摘要能力，避免本地开发和测试模式不一致。
- 现有 `InterviewQuestion` 不在本任务中迁移为新题库资源。
- Launchpad 仍可保持兼容轨道；本任务只提供治理后台和摘要数据。

## 取舍

- 不在本任务中实现真实向量索引重建，以免在题库治理尚未闭环时扩大复杂度。
- 本任务只提供 `vector_status=failed` 筛选和“索引未接入/待重建”状态提示。
- 后续再启用真实触发索引重建，包括索引任务、失败重试、异步运行状态和系统状态观测。
- 不做在线编辑和版本历史详情页，但后端写入必须保留版本记录，为后续页面留接口空间。

## 风险

- 如果导入校验和发布接口各写一套规则，容易出现预览通过但发布失败。应复用同一校验函数。
- 如果前端把管理状态塞入 `systemStore`，会扩大系统状态页职责。应单独建题库治理状态。
- 如果发布接口部分成功但批次统计不一致，会影响演示可信度。发布写入需要明确逐条结果和失败处理策略。
