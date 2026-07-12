# 面试舱学习闭环回流 PRD

## 目标

把已经进入面试报告的 `retrieval_summary.coverage` 和 `retraining_suggestions` 真正回流到现有学习闭环，让 `/users/me/dashboard` 不再只基于排查工坊和通用面试分数生成建议，而是能显式给出“面试专项复训”。

本任务只增强现有 `learningPlan()`、`reviewCalendar`、`dashboard` 聚合和仪表盘展示，不新造独立 Mentor 页面，不做 PDF 简历解析，也不做全局覆盖统计。

## 已确认事实

- 面试报告接口已经返回：
  - `retrieval_summary.coverage`
  - `retrieval_summary.retraining_suggestions`
- 当前 `learning.go` 只把面试结果当成一条 `FinalScore` 分数样本，没有消费 `coverage` 和 `retraining_suggestions`。
- 当前 `DashboardPage.tsx` 只展示通用推荐和弱项，没有明确“面试专项建议”表达。

## 需求

1. 学习计划回流
   - `learningPlan()` 必须消费已完成面试会话里的 `retrieval_summary.retraining_suggestions`。
   - 新建议仍走现有 `LearningRecommendation[]`，不新增第二套推荐接口。

2. 推荐策略
   - 至少生成一类 `kind=interview` 的推荐项。
   - 推荐理由必须来自真实报告的复训建议或知识覆盖弱项，而不是通用模板。
   - 推荐入口仍回到 `/interviews`，不新建独立路由。

3. 复习计划
   - `reviewCalendar.review_plan` 必须能包含来自面试报告复训建议的条目。
   - 这类条目需要可识别的 `source_kind`，便于前端区分。

4. 仪表盘展示
   - 仪表盘推荐区要能明确看出哪些建议来自面试专项回流。
   - 不要求新页面，但至少要有可见标识，不让它和排查题推荐混成一片。

## 验收标准

- 有低分/回退的已完成面试会话时，`/users/me/dashboard` 返回的 `learning_plan.recommendations` 中包含 `kind=interview` 建议。
- `review_calendar.review_plan` 中能看到来自面试复训的条目。
- 仪表盘页面能显式区分“面试专项建议”与普通排查推荐。

## 不做

- 不做独立 AI Mentor 页面。
- 不做 PDF 简历画像。
- 不做全局知识覆盖统计大盘。
