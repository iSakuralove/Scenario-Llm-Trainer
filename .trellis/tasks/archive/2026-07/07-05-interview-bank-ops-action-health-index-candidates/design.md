# 技术设计

## 架构边界

本切片新增 **题库运营动作候选生成** 的后端能力，候选来源限定为健康诊断和索引状态。

涉及边界：

- `domain`
  新增候选动作、候选请求/响应和生成策略字段。
- `store`
  优先复用 `ListInterviewKnowledgeAtoms`、`ListInterviewBankOpsActions` 和现有健康诊断聚合函数。
- `httpapi`
  新增 admin-only 候选生成接口。
- `frontend`
  本切片可只补 API 类型/客户端；候选保存与完整页面交互放后续切片。

不涉及 schema 变更，因为候选生成不持久化。

## 公共接口

新增：

- `POST /api/v1/admin/interview-bank/ops-actions/candidates`

建议请求：

```json
{
  "sources": ["health_diagnostic", "index_status"],
  "domain": "backend",
  "category": "cache",
  "difficulty": "L3",
  "limit": 50
}
```

建议响应：

```json
{
  "list": [],
  "total": 0,
  "skipped_existing": 0,
  "policy": {
    "sources": ["health_diagnostic", "index_status"],
    "limit": 50
  }
}
```

## 候选模型

候选应复用动作字段的业务语义，但与持久化动作分开建模：

- `candidate_key`
- `action_type`
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

候选不应包含 `id`、`status`、`created_by`、`created_at`、`updated_at` 这类持久化字段。

## 生成规则

### 健康诊断

- 对 `blocked` 组合生成 `fill_gap/P0`。
- 对 `warning` 组合，如 failed/pending 资源导致风险，生成 `rebuild_index/P2`。
- dedupe key 使用 `action_type + domain + category + difficulty`。

### 索引状态

- 对 published + failed atom 生成 `rebuild_index/P1`。
- 对 published + pending atom 生成 `rebuild_index/P2`。
- dedupe key 使用 `rebuild_index + atom_id`。
- draft/archived atom 跳过。

### active 去重

active 状态：

- `open`
- `in_progress`
- `watching`
- `reopened`

候选生成前读取动作列表，构建 active dedupe key 集合；命中 active key 的候选跳过，并计入 `skipped_existing`。

## TDD 切片顺序

1. RED：健康诊断 blocked 组合通过 candidate API 返回 `fill_gap/P0`，且不写动作列表。
2. GREEN：实现最小 domain 类型、候选生成函数和 handler。
3. RED：published failed atom 返回 `rebuild_index/P1`，draft/archived 不返回。
4. GREEN：补索引状态规则。
5. RED：active 动作 dedupe 会跳过候选，resolved/dismissed 不阻止生成。
6. GREEN：复用动作列表读取做 active key 过滤。
7. RED：非管理员访问被拒绝，limit/sources 解析正确。
8. GREEN：补 handler 校验与响应策略。

## 兼容与迁移

- 不新增表、不改 migration。
- 不改变手工创建动作接口。
- 不改变健康诊断、检索预览、真实检索运营面板已有响应。
- 候选生成不依赖 Postgres 特有能力；MemoryStore 和 PostgresStore 只需保证既有列表接口语义。

## 回滚点

候选 API 是新增 admin-only 只读能力。若出现问题，可以移除路由或隐藏前端入口，不影响已保存运营动作和题库运行时。
