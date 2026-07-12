# 面试舱新发布题库驱动推荐 PRD

## 目标

让面试舱启动台的推荐训练不只依赖未完成会话、历史低分维度和个人偏好，还能显式提示“某个方向题库刚刚更新，适合趁热训练”。

## 已确认事实

- 当前推荐来源已包含：
  - `continue_session`
  - `weak_dimension`
  - `preferred_domain`
  - `default_open_track`
- atom-backed `launchpad` 轨道聚合内部已经能拿到 `latestUpdate`，但还没有把它转成推荐信号。
- 前端推荐卡已经会直接显示后端 `reason`，因此这刀可以后端先行。

## 需求

1. 新增推荐来源
   - `source_kind=fresh_content`
   - 触发条件：正式题库轨道中有最近更新的轨道，且未被 `continue_session` / `weak_dimension` 占满。

2. 推荐原因
   - 必须明确表达“题库最近更新/刚发布”。
   - 不能使用泛文案。

3. 排序优先级
   - 推荐优先级更新为：
     1. `continue_session`
     2. `weak_dimension`
     3. `fresh_content`
     4. `preferred_domain`
     5. `default_open_track`

## 验收标准

- atom-backed轨道最近更新时，`recommended_tracks` 中会出现 `source_kind=fresh_content` 项。
- 推荐原因会明确点出“最近更新”或“新发布题库”语义。
- 该推荐项仍必须是当前真实可启动的开放轨道。

## 不做

- 不新增前端独立“最近更新轨道”模块。
- 不做复杂时间窗口配置。
