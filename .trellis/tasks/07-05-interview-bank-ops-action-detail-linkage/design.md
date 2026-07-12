# 技术设计

## 架构边界

本切片在既有运营动作模型上新增“详情读取与 UI 联动”，不新增 schema，不新增历史表，不修改候选生成/保存。

涉及边界：

- `domain`
  新增动作详情响应中的轻量 atom 上下文类型。
- `store`
  新增按 id 读取运营动作的方法，MemoryStore 与 PostgresStore 同步实现。
- `httpapi`
  新增 `GET /api/v1/admin/interview-bank/ops-actions/{id}` handler，复用现有 atom Store 读取。
- `frontend`
  新增动作详情 API/type，在 `InterviewBankAdminPage` 中增加详情面板和“从详情触发原子重建”的按钮。

## 数据流

1. 前端点击 open 队列中的“详情”。
2. `api.adminInterviewBankOpsActionDetail(token, actionId)` 请求后端。
3. 后端读取动作主记录。
4. 若动作含 `atom_id`，后端读取当前 atom，并计算：
   - 是否存在；
   - 是否归档；
   - 当前 `status/vector_status/current_version/updated_at`。
5. 后端返回：
   - `action`
   - `atom_context?`
   - `stale`
   - `stale_reason?`
6. 前端详情面板渲染：
   - 动作基本信息；
   - evidence；
   - 目标范围；
   - atom 当前状态；
   - 复用按钮：套用、查看原子、重建索引。

## API 契约

新增：

```json
GET /api/v1/admin/interview-bank/ops-actions/{id}
{
  "action": {
    "id": "ops_123",
    "action_type": "rebuild_index",
    "status": "open",
    "priority": "P1",
    "source": "index_status",
    "dedupe_key": "rebuild_index|atom|atom_failed",
    "title": "重建题库索引：Redis 缓存雪崩追问",
    "reason": "已发布题目索引状态为 failed，可能影响后续追问检索。",
    "domain": "backend",
    "category": "cache",
    "difficulty": "L3",
    "atom_id": "atom_failed",
    "evidence": {},
    "created_by": "user-admin",
    "created_at": "2026-07-05T12:00:00Z",
    "updated_at": "2026-07-05T12:00:00Z"
  },
  "atom_context": {
    "id": "atom_failed",
    "title": "Redis 缓存雪崩追问",
    "status": "published",
    "vector_status": "failed",
    "current_version": 3,
    "updated_at": "2026-07-05T12:10:00Z"
  },
  "stale": false,
  "stale_reason": ""
}
```

当 atom 缺失或归档时：

```json
{
  "action": { "...": "..." },
  "atom_context": null,
  "stale": true,
  "stale_reason": "关联 atom 不存在"
}
```

或：

```json
{
  "action": { "...": "..." },
  "atom_context": {
    "id": "atom_archived",
    "title": "旧题",
    "status": "archived",
    "vector_status": "pending",
    "current_version": 4,
    "updated_at": "2026-07-05T12:10:00Z"
  },
  "stale": true,
  "stale_reason": "关联 atom 已归档"
}
```

## 后端实现

- `domain` 增加：
  - `InterviewBankOpsActionAtomContext`
  - `InterviewBankOpsActionDetail`
- `store.Interface` 增加：
  - `GetInterviewBankOpsAction(id string) (*domain.InterviewBankOpsAction, bool)`
- `MemoryStore`：
  - 复用现有动作 map/slice，按 id clone 返回。
- `PostgresStore`：
  - 单条查询 `interview_bank_ops_actions`，沿用现有 evidence JSON 反序列化逻辑。
- `httpapi`：
  - 详情 handler 做 admin 校验。
  - 动作不存在返回 `404`。
  - 若有 `atom_id`，读取当前 atom 并构建轻量上下文。
  - stale 判断仅依赖“缺失 / archived”，不自动判断 resolved。

## 前端实现

- `types/index.ts`
  - 新增动作详情类型。
- `api/client.ts`
  - 新增 `adminInterviewBankOpsActionDetail`。
- `InterviewBankAdminPage.tsx`
  - 新增 `activeOpsActionDetail`、`isOpsActionDetailLoading` 状态。
  - open 队列增加“详情”按钮。
  - 新增 `handleOpenOpsActionDetail(actionID)`。
  - 新增详情面板：
    - 基本信息
    - evidence
    - stale 状态
    - atom 当前状态
    - 入口按钮：套用、查看原子、重建索引
- 对 `rebuild_index + atom_id`，点击“重建索引”时直接调用现有重建 API，请求体固定为 `{ atom_ids: [atomID], limit: 1 }`。
- 重建成功后刷新动作详情、原子列表和 open 队列。

## TDD 切法

1. RED：详情 API 返回动作和当前 atom 上下文。
2. GREEN：补 domain/store/http handler 最小实现。
3. RED：atom 缺失/归档时返回 stale。
4. GREEN：补 stale 计算。
5. 前端接 API 和详情面板。
6. 复用现有重建 API，从动作详情直接触发单 atom 重建。

## 回滚点

- 如果详情联动有问题，可先下掉前端详情按钮；已存在列表、候选生成和保存能力不受影响。
- 如果 detail API 不稳定，可保留已有 open 队列，不影响其他治理面板的只读能力。
