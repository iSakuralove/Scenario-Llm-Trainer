# 技术设计

## 架构边界

本切片扩展已有运营动作候选生成能力，不新增独立 API。

涉及边界：

- `domain`
  扩展候选请求中允许的 source 契约，沿用 `InterviewBankOpsActionCandidate`。
- `store`
  复用 `InterviewRetrievalAnalytics(filter)`，不新增 Store 方法。
- `httpapi`
  扩展 `generateInterviewBankOpsActionCandidates` 的 source 分支。
- `frontend`
  扩展候选请求 `sources` union。

不涉及 schema 变更。

## 公共接口

沿用：

- `POST /api/v1/admin/interview-bank/ops-actions/candidates`

新增允许 source：

```json
{
  "sources": ["retrieval_analytics"],
  "domain": "backend",
  "category": "cache",
  "difficulty": "L3",
  "limit": 50
}
```

响应仍是：

```json
{
  "list": [],
  "total": 0,
  "skipped_existing": 0,
  "policy": {
    "sources": ["retrieval_analytics"],
    "limit": 50
  }
}
```

## 生成规则

### 回退组合

输入：`s.store.InterviewRetrievalAnalytics(filter).FallbackCombinations`

- `count >= 3` -> `fill_gap/P0`
- `count < 3` -> `fill_gap/P1`
- source: `retrieval_analytics`
- dedupe key: `fill_gap|combo|domain|category|difficulty`
- evidence:
  - `fallback_count`
  - `recent_reason`
  - `last_seen_at`
  - `analytics_window_total_logs`
  - `fallback_rate`

### 未命中原子

输入：`s.store.InterviewRetrievalAnalytics(filter).LowHitAtoms`

- `hit_count == 0` -> `observe/P3`
- `hit_count > 0` -> 本切片不生成候选
- source: `retrieval_analytics`
- dedupe key: `observe|atom|atom_id`
- evidence:
  - `atom_id`
  - `title`
  - `subject`
  - `domain`
  - `category`
  - `difficulty`
  - `question_role`
  - `hit_count`
  - `last_hit_at`
  - `analytics_window_total_logs`

## 去重与排序

沿用上一切片的 `add(candidate)`：

- 单次生成内相同 `dedupe_key` 只返回一个候选。
- active 动作状态 `open/in_progress/watching/reopened` 会跳过候选。
- `resolved/dismissed` 不跳过。
- 生成后按已有顺序和请求 limit 截断。

## TDD 顺序

1. RED：真实回退组合生成 `fill_gap/P0/P1` 候选，且不落库。
2. GREEN：允许 `retrieval_analytics` source，补 fallback combination 分支。
3. RED：`hit_count=0` 的 published followup/mixed atom 生成 `observe/P3`，已命中 atom 不生成。
4. GREEN：补 low-hit 分支。
5. RED：active dedupe 对 retrieval 候选生效，invalid source 测试从“retrieval invalid”调整为真正非法值。
6. GREEN：复用现有 active key 过滤和 policy 归一化。
7. 补前端类型 union。

## 兼容与迁移

- 不新增表、不改 migration。
- 不改变真实检索日志保存与聚合规则。
- 不改变健康诊断/索引状态候选语义。
- 不改变用户侧报告或面试运行时。

## 回滚点

该能力是 admin-only 只读候选扩展。若出现问题，可临时从 `sources` 允许列表移除 `retrieval_analytics`，不会影响已保存动作或题库运行时。
