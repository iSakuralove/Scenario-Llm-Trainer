# 实施计划

## 顺序

1. 后端类型与聚合逻辑
   - 在 `interview_runtime.go` 扩展 `interviewReportRetrievalSummary`。
   - 新增覆盖项、建议项结构体和内部聚合 helper。
   - 保留旧字段生成逻辑。

2. 后端测试
   - 覆盖多 subject 聚合。
   - 覆盖低分弱项中文标签。
   - 覆盖 fallback 建议。
   - 覆盖空 session/evaluations。

3. 前端类型与展示
   - 更新 `InterviewReportRetrievalSummary` 类型。
   - 在报告页追问摘要面板中增加覆盖分布和复训建议区块。
   - 更新 CSS，保证长文本换行、响应式布局稳定。

4. 文档
   - 更新 `docs/architecture.md` 的面试报告展示边界。
   - 新增 `features/interview-report-knowledge-coverage.md`。

5. 验证
   - `cd backend; go test ./internal/httpapi`
   - `cd backend; go test ./...`
   - `npm --prefix frontend run lint`
   - `npm --prefix frontend run build`

## 风险文件

- `backend/internal/httpapi/interview_runtime.go`
- `frontend/src/features/interviews/InterviewReportPage.tsx`
- `frontend/src/features/interviews/InterviewReportPage.css`
- `frontend/src/types/index.ts`

## 注意事项

- 不修改 `Store` 接口和数据库结构。
- 不展示题库原子正文、检索 query、命中片段或向量分。
- 前端访问新增字段时使用可选链和空数组兜底。
- 中文文案保持短句，避免卡片内溢出。
