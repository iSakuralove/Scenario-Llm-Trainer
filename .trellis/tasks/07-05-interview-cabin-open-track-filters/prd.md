# 面试舱开放组合筛选与可用性模型 PRD

## 目标

让面试舱用户侧启动台不再只是“开放轨道列表”，而是能按 `category / difficulty / question_role / tags` 筛选开放组合，并清楚看到当前组合是否为正常可训练、兼容兜底或追问增强降级状态。

本任务只做用户侧开放组合筛选与可用性展示，不做管理端题库治理操作，也不把未开放组合渲染成可点击灰态入口。

## 已确认事实

- 当前 `launchpad` 聚合已经返回 `open_tracks`、`recommended_tracks`、`recent_sessions` 与覆盖摘要。
- 当前 `open_tracks` 已含：
  - `category`
  - `difficulty`
  - `question_type`
  - `question_role`
  - `availability_state`
  - `unavailable_reason`
  - `vector_status_summary`
- 当前前端面试舱还没有用户侧筛选器，也没有可用性 badge。
- 文档要求：
  - 用户侧只展示后端认为可见的组合；
  - 用户可做轻量筛选；
  - 必须能理解为什么当前可以练、或者当前处于何种降级状态。

## 需求

1. `launchpad` 轨道数据补充
   - `open_tracks` 增加 `tags: string[]`。
   - `details` 继续保留，但不能代替结构化筛选字段。

2. 用户侧筛选器
   - 支持按以下条件筛选当前 `open_tracks`：
     - `category`
     - `difficulty`
     - `question_role`
     - `tags`
   - 所有筛选都只作用于当前后端返回的 `open_tracks`，不自行发明未开放组合。
   - 提供“清空筛选”入口。

3. 可用性模型展示
   - 每个开放组合卡片展示轻量 availability badge。
   - 最少支持：
     - `available`
     - `indexing`
     - `fallback`
   - `availability_state` 与 `vector_status_summary` 不一致时，以后端 `availability_state` 为准。
   - 用户侧不展示“draft / archived / 批次 / source_ref”。

4. 空状态
   - 筛选后没有结果时，展示“没有符合当前筛选条件的训练组合”。
   - `launchpad` fallback 模式时，组合卡片仍可渲染，但要明确是兼容轨道。

## 验收标准

- 后端 `open_tracks` 返回 `tags`。
- 前端可在面试舱中按分类、难度、角色和标签筛选组合。
- 卡片能显示当前组合是“可训练 / 追问增强准备中 / 兼容轨道”。
- 清空筛选后恢复全部开放组合。
- 筛选为空时有明确空状态，不报错。

## 不做

- 不展示未开放组合灰态卡片。
- 不做管理端题库筛选器复用。
- 不做组合级解释页。
