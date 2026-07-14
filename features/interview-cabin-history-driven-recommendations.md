# 面试舱历史表现驱动推荐

## 目标

让面试舱启动台的推荐训练不再只看“未完成会话 + 偏好领域”，而是真正利用最近已完成面试的最低维度信号，把“你上次哪项最弱、应该继续练什么”明确展示出来。

## 修改范围

- 后端 `GET /api/v1/interviews/launchpad` 推荐逻辑新增 `weak_dimension` 来源。
- 后端 `recent_sessions` 轻量摘要新增 `weak_dimension / weak_score`。
- 前端最近训练卡展示最低维度与分数。

## 核心实现

- `recommended_tracks` 新增 `source_kind=weak_dimension`。
- 推荐优先级现在为：
  1. `continue_session`
  2. `weak_dimension`
  3. `preferred_domain`
  4. `default_open_track`
- 后端从最近已完成面试会话的 `evaluations[*].dimension_scores` 中找出最低维度；只有当最低分低于 75 时，才生成“继续补强”推荐。
- `recent_sessions` 会在已完成会话上额外返回：
  - `weak_dimension`
  - `weak_score`
- 前端最近训练卡会在有该字段时显示“最低维度：技术准确性 · 55 分”这类摘要。

## 影响范围

- `/api/v1/interviews/launchpad` 的 `recommended_tracks` 现在更依赖真实面试历史。
- `/api/v1/interviews/launchpad` 的 `recent_sessions` 结构扩展为可展示评分弱项摘要。
- 不影响创建会话、报告接口和学习仪表盘接口。

## 验证方式

- `go test ./internal/httpapi -run "TestInterviewLaunchpadRecommendsTrackFromWeakDimension|TestInterviewLaunchpadRecentSessionsIncludeWeakDimensionSummary" -count=1`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 已知限制

- 当前还没有把“新发布题库”作为推荐来源。
- 最低维度仍按最近会话中的最低分直接判定，没有引入更复杂的趋势模型。
