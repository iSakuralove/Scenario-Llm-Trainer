# 技术设计

## 架构边界

本任务只增强现有学习聚合链路：

- `backend/internal/httpapi/learning.go`
- `backend/internal/httpapi/handlers_auth.go` 的 dashboard 聚合输出
- `frontend/src/features/learning/DashboardPage.tsx`

不改：

- 面试报告接口结构
- 面试运行时提交流程
- 独立学习服务或新路由

## 数据流

1. 用户完成一场带检索摘要的面试。
2. `learningPlan()` 遍历用户面试会话。
3. 对每个已完成会话调用现有 `buildInterviewReportRetrievalSummary(session)`。
4. 从 `retraining_suggestions` 中提取“面试专项复训”建议，映射到 `LearningRecommendation`。
5. 从同一批建议中提取 `ReviewPlanItem`。
6. `/users/me/dashboard` 返回增强后的 `learning_plan` 与 `review_calendar`。
7. 仪表盘前端将 `kind=interview` 的推荐显式标记出来。

## 设计选择

- 不新增新类型接口，优先复用 `LearningRecommendation` 和 `ReviewPlanItem`。
- `kind=interview` 作为区分来源的主标识。
- `source_kind=interview_retraining` 作为 review plan 条目的来源标识。
- 仪表盘不额外请求报告详情，所有回流都在后端聚合完成。

## 推荐映射

对 `interviewReportRetrainingSuggestion`：

- `title` -> `面试复训：{subject}`
- `description` -> 第一条 action 或默认说明
- `reason` -> 报告里的 `reason`
- `difficulty` -> 当前会话难度或 `targetInterviewDifficulty`
- `action_label` -> `进入面试舱`
- `action_path` -> `/interviews`

优先级：

- 用报告 `priority` 反向映射到较高的 `LearningRecommendation.Priority`，保证它不会总被 scenario 推荐挤掉。

## ReviewPlan 映射

每条面试复训建议映射成：

- `domain`：会话 domain
- `focus`：`{subject} 面试复训`
- `actions`：报告建议 actions
- `target_score`：报告 target_score
- `source_kind`：`interview_retraining`
- `source_id`：会话 id
- `reason`：报告建议 reason

## TDD 切法

1. RED：dashboard 返回 `kind=interview` 推荐。
2. GREEN：在 `learningPlan()` 中接入报告复训建议。
3. RED：review calendar 包含 `source_kind=interview_retraining`。
4. GREEN：补 review plan 映射。
5. 前端把 `kind=interview` 标识渲染出来。
