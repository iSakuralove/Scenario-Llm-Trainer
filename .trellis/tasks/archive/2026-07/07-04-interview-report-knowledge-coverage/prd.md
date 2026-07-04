# 面试报告知识点分布与复训建议增强

## 目标

在现有面试报告中补齐“本场到底考了哪些知识点、哪些薄弱、下一步怎么复训”的闭环，让普通用户在完成面试后能直接看到可执行的复盘方向。

## 用户价值

- 用户不只看到总分和五维评分，还能看到本场覆盖的知识点分布。
- 用户能识别低分轮次对应的薄弱维度和知识点。
- 用户能拿到不依赖管理员后台的复训建议，回到面试训练入口继续练习。

## 已确认事实

- `GET /api/v1/interviews/sessions/{id}/report` 当前返回 `retrieval_summary`，包含 `summary_text`、命中轮次、回退轮次、去重 subjects 和每轮 `subject/fallback_used/follow_up_type`。
- `domain.InterviewSession` 已保存 `QuestionSnapshot`、`SelectedAtomSnapshots`、每轮 `InterviewEvaluation.RetrievedSubjects`、`FollowUpSubject`、`FallbackUsed` 和 `DimensionScores`。
- 前端 `InterviewReportPage` 已有“追问检索摘要”面板和 `InterviewReportRetrievalSummary` 类型。
- 现有架构文档明确：面试报告当前不展示原子正文、内部检索 query、命中片段或 selected atom 快照。

## 需求

1. 后端增强 `retrieval_summary`：
   - 增加知识点覆盖分布列表，按出现次数和名称稳定排序。
   - 每个知识点至少包含：`subject`、`round_count`、`hit_count`、`fallback_count`、`average_score`、`lowest_score`、`weak_dimensions`。
   - 增加复训建议列表，基于低分轮次、fallback 轮次和覆盖不足生成。
   - 建议项至少包含：`id`、`subject`、`priority`、`reason`、`actions`、`target_score`、`source_rounds`。
   - 保持旧字段兼容，旧前端字段不改名、不删除。

2. 前端增强报告页：
   - 在现有“追问检索摘要”下展示知识点覆盖分布。
   - 展示复训建议，内容可扫描、可换行，移动端不溢出。
   - 没有覆盖数据时保持当前空态文案，不显示空壳列表。
   - 不展示题库原子正文、内部检索 query、embedding 命中分或管理员审计信息。

3. 测试与文档：
   - 增加后端单元测试覆盖覆盖分布、低分建议、fallback 建议和空数据。
   - 更新前端类型。
   - 更新 `docs/architecture.md` 中面试报告展示边界。
   - 新增 `features/interview-report-knowledge-coverage.md`。

## 验收标准

- 完成一场包含多轮评价的面试报告时，接口返回 `retrieval_summary.coverage[]` 和 `retrieval_summary.retraining_suggestions[]`。
- 同一 subject 多轮出现时被聚合，统计字段正确。
- 低分维度能转换为中文弱项标签。
- fallback 轮次会产生“回到基础知识点补齐”的建议。
- 没有 evaluation 的历史会话仍返回兼容空摘要。
- 前端报告页可展示覆盖分布和建议，长 subject/actions 不撑破布局。

## 不做

- 不把复训建议自动写入用户学习计划或个人画像。
- 不新增数据库表、迁移或异步任务。
- 不展示题库原子正文、检索 query、命中片段或向量分数。
- 不做“采纳建议生成新训练任务”的交互闭环。
- 不改管理员题库治理页面。

## 风险与约束

- 复训建议首期是规则生成，不调用 LLM，避免报告接口变慢或依赖外部 provider。
- 旧历史会话可能缺少 `RetrievedSubjects`，需要回退到 `FollowUpSubject` 或开场题 `QuestionSnapshot.Subject`。
- 前端只消费新增可选字段，避免旧接口数据导致渲染错误。

## 开放问题

无阻塞问题。首期按“只增强报告展示，不改长期学习档案”的边界实现。
