# 面试舱空状态与降级态收口 PRD

## 目标

让面试舱启动台能区分并正确表达“兼容轨道”“追问增强降级中”“当前筛选无结果”“题库已发布但暂无可启动组合”等状态，不再把所有非理想情况都折叠成一条 notice。

## 已确认事实

- 当前前端只用 `launchpadSource` 和 `launchpadNotice` 粗略区分 API 模式。
- 当前 `tracks.length === 0` 时，前端会直接回退本地兼容轨道，这会吞掉未来真正的“无开放组合”状态。
- 当前文档明确要求：
  - 用户能区分接口异常、索引降级、筛选无结果和真正无开放组合。

## 需求

1. `launchpad.summary` 增加 `state`，首期至少支持：
   - `ready`
   - `retrieval_partial`
   - `retrieval_degraded`
   - `compatibility_fallback`

2. 前端启动台：
   - 根据 `summary.state` 渲染状态说明。
   - `tracks.length === 0` 且 `fallback_mode=false` 时，不再强制回退兼容轨道，而是显示空状态。
   - 当前筛选无结果时显示“没有符合当前筛选条件的训练组合”。

3. 文案边界
   - `compatibility_fallback`：强调“当前仍可训练，但使用兼容轨道”。
   - `retrieval_degraded`：强调“开场题可用，追问增强降级”。
   - `retrieval_partial`：强调“部分组合具备增强能力”。

## 验收标准

- atom-backed 且 `indexed_atom_count=0` 时，后端 `summary.state=retrieval_degraded`。
- fallback 模式时，后端 `summary.state=compatibility_fallback`。
- 前端不会再把“空开放组合”一律当成 fallback。
- 筛选无结果和降级态文案能在页面中明确区分。
