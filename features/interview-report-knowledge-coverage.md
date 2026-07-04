# 面试报告知识点分布与复训建议增强

## 目标

让普通用户在完成面试后，除了总分、五维评分和追问检索摘要，还能看到本场覆盖的知识点分布、薄弱维度和下一步复训建议。

## 修改范围

- `backend/internal/httpapi/interview_runtime.go`
  增强报告 `retrieval_summary` 聚合结构。
- `backend/internal/httpapi/interview_session_test.go`
  增加报告知识点覆盖与复训建议测试。
- `frontend/src/types/index.ts`
  增加报告覆盖分布与复训建议类型。
- `frontend/src/features/interviews/InterviewReportPage.tsx`
  在面试报告页展示知识点覆盖和复训建议。
- `frontend/src/features/interviews/InterviewReportPage.css`
  增加响应式布局和长文本换行样式。
- `docs/architecture.md`
  更新面试报告展示边界。

## 核心实现

- 后端基于 `InterviewSession.Evaluations` 单次遍历聚合 subject 维度的轮次、题库命中、规则回退、平均分、最低分和低分维度。
- 低于 70 分的维度会映射为中文弱项标签。
- 复训建议由规则生成，不调用 LLM，不新增数据库表或异步任务。
- 前端在现有追问检索摘要面板内追加展示覆盖卡片和建议卡片，新增字段使用可选链兜底。

## 影响范围

- 报告接口新增 `retrieval_summary.coverage` 和 `retrieval_summary.retraining_suggestions` 字段，旧字段保持兼容。
- 历史空报告仍返回空覆盖与空建议，不影响旧会话查看。
- 不修改面试题库、索引、用户学习计划或个人画像持久化逻辑。

## 验证方式

- `cd backend; go test ./internal/httpapi`
- `cd backend; go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 已知限制

- 复训建议首期是规则生成，只用于报告展示，不会自动创建复训任务。
- 知识点分布基于会话已有评价和追问检索摘要，无法还原未记录的内部检索 query 或命中片段。
