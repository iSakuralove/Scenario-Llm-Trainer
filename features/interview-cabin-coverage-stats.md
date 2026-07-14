# 面试舱训练覆盖率统计

## 目标

补齐面试舱剩余能力“面试覆盖率统计”，让用户在现有启动台直接看到自己已经覆盖了多少开放轨道、主要练过哪些知识点、还缺哪些方向。

## 修改范围

- 后端扩展 `GET /api/v1/interviews/launchpad`，新增 `coverage_stats` 聚合字段。
- 前端 API 类型增加覆盖率统计结构。
- 面试舱页面新增“训练覆盖率”信息块。
- smoke 脚本增加新区域断言。
- 更新 Launchpad 契约与架构说明。

## 核心实现

- 以当前 `open_tracks` 作为覆盖率分母，不新造第二套开放组合来源。
- 只统计 `final_evaluated` 的历史面试会话，避免未完成会话把覆盖率虚高。
- 用 `domain + difficulty` 匹配“已覆盖开放轨道”，输出：
  - `total_open_tracks`
  - `practiced_open_tracks`
  - `coverage_percent`
  - `completed_sessions`
- 复用 `buildInterviewReportRetrievalSummary()` 聚合历史报告安全字段，生成：
  - `subject_count`
  - `top_subjects`
- 返回 `uncovered_track_ids`，前端再映射为当前可见轨道文案，避免后端重复返回同一份轨道详情。

## 影响范围

- 用户侧 `/interviews` 会多渲染一个训练覆盖率面板。
- Launchpad 接口响应契约扩展，但仍保持单接口聚合，不增加额外请求数。
- 不修改会话表结构、不引入新 Store 接口，也不新增独立覆盖率接口。

## 验证方式

- `go test ./internal/httpapi -run TestInterviewLaunchpadCoverageStatsSummarizeCompletedTrackCoverage -count=1`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- `npm --prefix frontend run smoke`

## 已知限制

- 当前覆盖率按“开放轨道”统计，不是按全部知识原子或全部知识点统计。
- 高频知识点来自历史报告聚合，若用户还没有完成面试，则只显示空状态，不做本地推测。
- 当前待补方向只展示未覆盖的开放轨道，不自动生成额外推荐理由。
