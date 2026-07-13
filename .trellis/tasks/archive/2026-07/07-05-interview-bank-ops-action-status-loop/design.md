# 技术设计

## 架构边界

本切片在既有运营动作主表上增加“状态流转历史”，保持动作当前状态和历史记录分离。

涉及边界：

- `domain`
  新增状态更新请求、历史记录和详情响应扩展。
- `store`
  新增：
  - 更新动作状态
  - 读取动作历史
  - 在详情查询中拼接历史
- `schema`
  新增 `interview_bank_ops_action_history` 表与索引。
- `httpapi`
  新增 `PATCH /api/v1/admin/interview-bank/ops-actions/{id}`。
- `frontend`
  详情面板新增状态操作区和历史区。

## 设计选择

- 主表继续只表示“当前状态”。
- 历史表单独存 `from_status -> to_status + note + actor + created_at`。
- 不把历史塞进 `evidence`，避免 evidence 语义从“创建证据快照”污染成“可变审计日志”。
- 详情接口直接返回 `history[]`，避免前端额外发第二个 history 请求。

## 数据模型

新增表：

```sql
interview_bank_ops_action_history (
  id TEXT PRIMARY KEY,
  action_id TEXT NOT NULL REFERENCES interview_bank_ops_actions(id) ON DELETE CASCADE,
  from_status VARCHAR(32) NOT NULL,
  to_status VARCHAR(32) NOT NULL,
  note TEXT,
  created_by TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
)
```

索引：

- `(action_id, created_at DESC)`
- `(to_status, created_at DESC)`

## API 契约

### PATCH `/api/v1/admin/interview-bank/ops-actions/{id}`

请求：

```json
{
  "status": "resolved",
  "note": "已完成索引重建并复查检索恢复"
}
```

响应：

```json
{
  "action": { "...": "当前动作" },
  "history_entry": { "...": "本次状态变更记录" }
}
```

### GET `/api/v1/admin/interview-bank/ops-actions/{id}`

在现有 detail 基础上追加：

```json
{
  "history": [
    {
      "id": "ops_hist_1",
      "action_id": "ops_1",
      "from_status": "open",
      "to_status": "resolved",
      "note": "已完成索引重建并复查检索恢复",
      "created_by": "user-admin",
      "created_at": "2026-07-05T12:00:00Z"
    }
  ]
}
```

## 状态规则

- `resolved` / `dismissed`：`note` 必填。
- `reopened`：仅允许当前状态为 `resolved` 或 `dismissed`。
- `open -> open` 这类无变化请求直接拒绝，避免制造空历史。
- 其他合法状态只做最小约束，不引入复杂工作流图。

## TDD 切法

1. RED：管理员把 open 动作标记为 `resolved`，主表状态更新且详情能看到历史。
2. GREEN：新增 history 表、Store 更新接口、PATCH handler、detail history 返回。
3. RED：`resolved/dismissed` 缺少 note 返回 400。
4. GREEN：补 note 校验。
5. RED：`reopened` 只能从 `resolved/dismissed` 进入。
6. GREEN：补状态前置校验。
7. 前端详情面板新增状态按钮、备注输入和历史列表。

## 可借鉴点

来自 `AI-interview`：

- 业务状态流转和审核记录应独立持久化，不混入向量/证据层。
- 展示层只读取轻量状态摘要，完整治理数据仍以主业务库为准。

## 回滚点

- 若状态流转出现问题，可暂时隐藏前端状态操作区；既有列表、候选和详情只读能力不受影响。
