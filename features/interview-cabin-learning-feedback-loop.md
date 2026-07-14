# 面试舱学习闭环回流

## 目标

让已经进入面试报告的 `retrieval_summary.coverage` 和 `retraining_suggestions` 真正回流到当前学习闭环，使仪表盘不仅能展示排查题推荐，也能明确给出“面试专项建议”与对应复训动作。

## 修改范围

- 后端 `learningPlan()` 开始消费已完成面试会话的 `buildInterviewReportRetrievalSummary(session)` 结果。
- 后端学习计划推荐新增 `kind=interview` 的面试专项建议。
- 后端 `review_calendar.review_plan` 新增 `source_kind=interview_retraining` 条目。
- 前端仪表盘把 `kind=interview` 的推荐从通用推荐里显式区分出来。

## 核心实现

- `learning.go` 在遍历已完成面试会话时，除了记录 `FinalScore` 外，还读取报告级 `retraining_suggestions`。
- 每条复训建议被映射成 `LearningRecommendation`：
  - `kind=interview`
  - `title=面试复训：{subject}`
  - `action_path=/interviews`
  - `reason` 直接复用报告建议原因
- 同一批复训建议被映射成 `ReviewPlanItem`：
  - `source_kind=interview_retraining`
  - `focus={subject} 面试复训`
  - `actions` 直接复用报告建议动作
- 如果已经存在面试专项复训条目，则 `review_plan` 会优先展示它们，并避免再重复塞入泛化的 `interview_wrong` 条目。
- 仪表盘推荐区新增“面试专项建议”分组，让用户明确知道这些建议来自最近面试结果，而不是普通排查题推荐。

## 影响范围

- `/api/v1/users/me/dashboard` 返回的 `learning_plan.recommendations` 中现在可能包含 `kind=interview`。
- `/api/v1/users/me/dashboard` 返回的 `review_calendar.review_plan` 中现在可能包含 `source_kind=interview_retraining`。
- 面试报告接口结构未改动，仍只通过已有 `retrieval_summary` 参与回流。
- 不引入新路由，不新增独立 Mentor 页面。

## 验证方式

- `go test ./internal/httpapi -run "TestLearningPlanDashboardCalendarAndCheckin|TestDashboardLearningPlanIncludesInterviewRetrainingLoop" -count=1`
- `go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 已知限制

- 当前回流只使用面试报告中的复训建议，不直接读取全局知识覆盖趋势。
- 面试专项建议目前仍统一回到 `/interviews`，没有细分到具体推荐轨道或独立 Mentor 页面。
- 学习仪表盘还没有做全局“面试覆盖率统计”，只是在推荐和复习计划层面消费面试结果。
