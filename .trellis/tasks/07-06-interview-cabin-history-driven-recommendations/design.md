# 技术设计

## 边界

- 只增强 `GET /api/v1/interviews/launchpad`
- 只增强面试舱启动台展示
- 不改会话创建、报告接口或学习仪表盘

## 推荐来源优先级

1. `continue_session`
2. `weak_dimension`
3. `preferred_domain`
4. `default_open_track`

## 最低维度计算

- 从最近已完成面试会话中读取 `session.Evaluations`
- 取最后一轮或整场最低分维度的最小值
- 只在分数低于 75 时产生 `weak_dimension` 推荐

## recent_sessions 扩展

- `weak_dimension?: string`
- `weak_score?: number`

## TDD

1. RED：最近已完成低分会话会产生 `weak_dimension` 推荐。
2. GREEN：后端推荐逻辑接入最低维度。
3. RED：`recent_sessions` 缺 `weak_dimension / weak_score`。
4. GREEN：补轻量摘要字段。
5. 前端最近训练卡展示最低维度。
