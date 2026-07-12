# 面试题库运营动作关闭忽略观察重开闭环 PRD

## 目标

在已有运营动作 open 队列、候选保存和动作详情基础上，补齐状态闭环：管理员可以把动作标记为处理中、观察中、已解决、已忽略和已重开；关闭/忽略必须填写备注；详情可看到状态历史；状态更新失败不影响既有只读治理面板。

本切片只做“动作状态流转 + 备注校验 + 历史记录 + 前端闭环”，不做自动 resolve/dismiss，不做批量状态操作，不做候选 dismiss。

## 用户价值

- 管理员做完编辑、归档、恢复或重建索引后，可以把动作显式关闭，而不是只能留在 open 队列里。
- 弱信号可以进入“观察中”，避免误导成必须立即修题。
- 错误关闭的动作可以重开，保留原历史。
- 后续复盘时能看到谁在什么时间把动作改成了什么状态，以及备注是什么。

## 已确认事实

- `interview_bank_ops_actions` 目前只有主表，没有动作状态历史表。
- 当前后端已经支持：
  - 动作创建；
  - open 队列读取；
  - 候选生成/保存；
  - 动作详情读取与当前 atom 上下文。
- 当前前端详情面板已经能展示动作证据、atom 当前状态，并复用现有原子详情/索引重建入口。
- `AI-interview` 参考项目没有同名功能，但它把“状态流转、审核记录、版本快照”当作独立业务持久化对象，而不是塞进向量索引或轻量 evidence；本切片沿用这个思路。

## 需求

1. 新增动作状态更新接口：
   - `PATCH /api/v1/admin/interview-bank/ops-actions/{id}`
   - 只允许管理员调用。
   - 请求至少包含 `status`，可包含 `note`。

2. 支持状态：
   - `open`
   - `in_progress`
   - `watching`
   - `resolved`
   - `dismissed`
   - `reopened`

3. 状态更新规则：
   - `resolved` 和 `dismissed` 必须填写非空 `note`。
   - `watching`、`in_progress`、`reopened` 允许 note 为空，但如果提供则需要落历史。
   - `reopened` 只允许从 `resolved` 或 `dismissed` 进入。
   - `resolved`、`dismissed`、`watching`、`in_progress` 可以从 active 状态进入，不做复杂工作流约束。
   - 状态更新后主表 `status` 和 `updated_at` 更新。
   - 每次有效状态变更都追加一条历史记录，不覆盖旧记录。

4. 历史记录要求：
   - 详情接口返回历史列表。
   - 每条历史至少包含：
     - `id`
     - `action_id`
     - `from_status`
     - `to_status`
     - `note`
     - `created_by`
     - `created_at`
   - 列表按 `created_at DESC` 展示。

5. 数据持久化：
   - 新增独立历史表，不把历史塞进 `evidence`。
   - MemoryStore 与 PostgresStore 行为一致。
   - schema、legacy compatibility SQL、`001_schema.sql` 同步更新。

6. 前端交互：
   - 动作详情中增加状态操作区。
   - `resolved` / `dismissed` 时强制输入备注。
   - `watching` / `in_progress` / `reopened` 提供显式按钮。
   - 更新成功后刷新动作详情、open 队列和历史列表。
   - API 失败时保留现有健康诊断、检索预览、真实检索运营面板可用。

## 验收标准

- 管理员可把 open 动作改成 `in_progress`、`watching`、`resolved`、`dismissed`。
- `resolved` 和 `dismissed` 缺少备注时返回 `400`。
- 管理员可把 `resolved` 或 `dismissed` 动作改成 `reopened`。
- 每次状态变更后，详情历史新增一条记录，包含操作者和备注。
- open 队列默认不再显示已 `resolved` / `dismissed` 的动作；`reopened` 会重新回到 active 集合。
- 非管理员访问状态更新接口返回现有权限错误。

## 风险与约束

- 本切片需要新增 schema；必须同步更新 `SchemaSQL`、`LegacyCompatibilitySQL`、`backend/migrations/001_schema.sql` 和 schema text tests。
- 历史表是业务审计数据，不应混入 `evidence`。
- 本切片不能把状态流转和真实题库修改绑定在一起，避免“点解决就自动改题/自动重建”。

## 不做

- 不做自动关闭 stale 动作。
- 不做候选 dismiss。
- 不做批量状态流转。
- 不做通知系统或通用工单。
