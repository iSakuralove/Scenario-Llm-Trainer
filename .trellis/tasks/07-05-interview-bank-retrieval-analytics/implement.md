# 实施计划

## 顺序

1. 领域和 Store
   - 增加检索日志过滤与分析类型。
   - Store 接口增加保存、查询和分析方法。
   - MemoryStore 实现日志保存、查询、聚合和 clone。
   - PostgresStore 实现日志插入、查询和聚合。
   - 补 `interview_retrieval_logs` 查询索引到 `schema.go` 和 `001_schema.sql`。

2. 运行时写日志
   - 在追问检索链路中生成日志。
   - query 文本脱敏并截断到 500 字。
   - 命中、回退、错误路径都记录。
   - 写日志失败不阻断面试。

3. 管理端 API
   - 增加 `/admin/interview-bank/retrieval-logs`。
   - 增加 `/admin/interview-bank/retrieval-analytics`。
   - 增加 admin-only、过滤参数、limit 上限和响应类型。

4. 前端
   - 增加 API client 和类型。
   - `InterviewBankAdminPage` 加载 analytics/logs。
   - 新增真实检索运营面板，展示指标、排行、最近日志。
   - 支持打开命中原子详情和套用回退组合筛选。

5. 文档和规范
   - 更新架构文档。
   - 新增 feature 文档。
   - 更新 Store/schema contract。

6. 验证
   - `cd backend; go test ./internal/httpapi ./internal/store`
   - `cd backend; go test ./...`
   - `npm --prefix frontend run lint`
   - `npm --prefix frontend run build`

## 风险文件

- `backend/internal/store/interface.go`
- `backend/internal/store/memory.go`
- `backend/internal/store/postgres.go`
- `backend/internal/store/schema.go`
- `backend/migrations/001_schema.sql`
- `backend/internal/httpapi/interview_runtime.go`
- `backend/internal/httpapi/handlers_interview_bank.go`
- `frontend/src/features/interviewBank/InterviewBankAdminPage.tsx`

## 注意事项

- 不提交 `docs/ai-interview-integration-prd.md`。
- 不让日志写入失败影响用户面试。
- 不暴露完整用户回答或用户身份。
- 不新增异步队列或 LLM 调用。
