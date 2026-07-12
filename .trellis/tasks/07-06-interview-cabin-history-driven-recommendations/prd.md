# 面试舱历史表现驱动推荐 PRD

## 目标

让面试舱启动台的推荐训练真正由“未完成会话 + 最近低分维度”驱动，而不是只看用户偏好领域和默认开放轨道。用户进入面试舱时，应能直接看到“你上次哪里答得最差、建议继续练什么”。

## 已确认事实

- 当前 `recommended_tracks` 已支持：
  - `continue_session`
  - `preferred_domain`
  - `default_open_track`
- 当前推荐还没有消费最近已完成面试的最低维度。
- 当前 `recent_sessions` 只返回 `final_score`，不返回弱项维度摘要。

## 需求

1. 后端推荐逻辑
   - 在没有未完成会话占据全部推荐位时，应把最近已完成面试里的最低维度信号映射成推荐。
   - 推荐原因必须明确点出最低维度，例如 `技术准确性偏低`。
   - 推荐项仍必须对应现有可启动轨道。

2. `recent_sessions`
   - 追加：
     - `weak_dimension?`
     - `weak_score?`
   - 只在已完成且能算出最低维度时返回。

3. 前端展示
   - 启动台“最近训练”卡片在有 `weak_dimension` 时显示最低维度和对应分数。
   - 推荐区对 `source_kind=weak_dimension` 的条目继续展示可解释原因。

## 验收标准

- 用户最近完成一场低分数据库面试后，`recommended_tracks` 中会出现数据库专项推荐，`reason` 含最低维度标签。
- `recent_sessions[0]` 在有低分维度时包含 `weak_dimension / weak_score`。
- 前端最近训练卡片能展示最低维度信息。

## 不做

- 不做“新发布题库”驱动推荐。
- 不做独立历史表现页面。
