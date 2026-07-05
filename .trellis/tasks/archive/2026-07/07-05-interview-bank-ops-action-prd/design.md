# 技术设计

## 架构边界

本任务新增 **题库运营动作**，作为面试题库管理端内部的治理队列。它只把已有运营信号沉淀为管理员可跟进事项，不自动修改题库。

涉及边界：

- `domain`
  新增运营动作、动作历史、候选动作、过滤条件和状态更新请求类型。
- `store`
  新增运营动作保存、列表、详情、状态更新、历史追加和 active dedupe 查询能力；MemoryStore 与 PostgresStore 语义一致。
- `schema`
  新增运营动作主表和动作历史表；运行时 schema 与初始化 migration 同步。
- `httpapi`
  在现有 `/api/v1/admin/interview-bank/*` admin-only 边界内新增运营动作接口。
- `frontend`
  在现有面试题库管理页新增“运营动作”区域，复用现有题目详情、组合筛选和索引重建入口。

不涉及普通用户面试报告、学习计划、案例工坊或自动修题。

## 首个 TDD Tracer Bullet

首个实现切片固定为：

> 管理员手工创建一个题库运营动作，并能通过 admin API 列表读回。

这个切片必须端到端覆盖：

- 动作领域类型和枚举校验。
- Store 保存和列表读取。
- Postgres schema/migration。
- Admin API 创建和列表。
- 非管理员拒绝访问。
- 前端最小队列展示和空状态。

首个切片不做候选生成、不做动作详情跳转、不做状态流转完整闭环。

## 数据模型

运营动作主记录建议字段：

- `id`
- `action_type`
- `status`
- `priority`
- `source`
- `dedupe_key`
- `title`
- `reason`
- `domain`
- `category`
- `difficulty`
- `atom_id`
- `evidence`
- `created_by`
- `created_at`
- `updated_at`

动作历史建议字段：

- `id`
- `action_id`
- `from_status`
- `to_status`
- `note`
- `operator_id`
- `created_at`

`evidence` 使用 JSONB，但首期只保存轻量证据：计数、时间、原因摘要、组合和 atom 元数据。不得保存完整用户回答、完整简历、项目背景或完整 atom 正文。

## 状态与约束

动作类型：

- `fill_gap`
- `fix_atom`
- `rebuild_index`
- `review_archive`
- `observe`

动作来源：

- `retrieval_analytics`
- `retrieval_log`
- `health_diagnostic`
- `index_status`
- `manual`

动作状态：

- `open`
- `in_progress`
- `watching`
- `resolved`
- `dismissed`
- `reopened`

首个切片只需要支持创建时默认 `open`，列表展示该状态。完整状态切换放后续切片。

`dedupe_key` 是后续候选去重的稳定边界，手工动作也应保存。首个切片可由后端基于类型和目标范围生成；如果目标不足，则退化为手工动作 ID 级别唯一 key，但不允许为空。

## API 契约

首个切片：

- `GET /api/v1/admin/interview-bank/ops-actions`
  返回动作列表，支持最小 `status/type/priority/domain/category/difficulty/atom_id/limit` 过滤。
- `POST /api/v1/admin/interview-bank/ops-actions`
  创建手工运营动作。

后续切片：

- `POST /api/v1/admin/interview-bank/ops-actions/candidates`
- `GET /api/v1/admin/interview-bank/ops-actions/{id}`
- `PATCH /api/v1/admin/interview-bank/ops-actions/{id}`

所有接口 admin-only，非管理员返回权限错误。

## 前端设计

首个切片在面试题库管理页新增最小“运营动作”面板：

- 空状态：暂无运营动作。
- 列表状态：展示标题、类型、状态、优先级、组合/原子、更新时间。
- 手工创建入口：支持标题、类型、优先级、原因、组合和可选 atom id。
- 错误状态：创建失败或读取失败时不影响已有健康诊断、检索预览和真实检索运营面板。

后续切片再加入候选预览、动作详情、联动题目详情、状态流转和真实浏览器验收。

## TDD 策略

每个切片遵循 red-green-refactor：

1. 先写一个公开接口行为测试并确认失败。
2. 写最小实现使测试通过。
3. 增加下一个行为测试。
4. 所有行为通过后再整理重复和边界。

首个切片推荐测试顺序：

1. Admin 创建动作后，列表能读回。
2. 非管理员创建/读取被拒绝。
3. 缺少必填字段或非法枚举被拒绝。
4. MemoryStore 与 PostgresStore clone/list/filter 语义一致。
5. 前端 lint/build 验证新增类型和页面状态。

## 兼容与迁移

- 新表为空时不影响现有面试题库管理页。
- 不改变已有 `interview_retrieval_logs`、题库版本、归档、索引重建和报告接口。
- 不修改普通用户侧响应。

## 回滚点

- 运营动作是新增 admin-only 能力，必要时可隐藏前端面板并保留数据表。
- 首个切片不改变运行时面试流程，回滚风险集中在 admin 路由和 schema。
