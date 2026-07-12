# 实施计划

## 顺序

1. RED：后端 HTTP 测试，管理员把 open 动作改成 `resolved`，返回更新后的 action 和一条 history entry。
2. GREEN：新增 domain 历史类型、Store 更新/历史读取接口、schema 历史表、PATCH handler，并让 detail 返回 history。
3. RED：`resolved/dismissed` 缺少备注返回 `400`。
4. GREEN：补备注校验。
5. RED：`reopened` 只能从 `resolved/dismissed` 进入。
6. GREEN：补状态前置校验。
7. RED：非管理员不能更新状态；history 顺序为最新优先。
8. GREEN：补权限和排序。
9. 前端详情面板增加状态按钮、备注输入、历史列表和错误提示。
10. 更新 `.trellis/spec/backend/store-schema-contracts.md`、`docs/architecture.md`、`features/interview-bank-ops-actions.md`。
11. 跑后端测试、前端 lint/build。

## 验证命令

- `cd backend; go test ./internal/httpapi ./internal/store`
- `cd backend; go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 风险文件

- `backend/internal/domain/interview_bank.go`
- `backend/internal/store/interface.go`
- `backend/internal/store/interview_bank.go`
- `backend/internal/store/memory.go`
- `backend/internal/store/postgres.go`
- `backend/internal/store/schema.go`
- `backend/internal/store/schema_test.go`
- `backend/migrations/001_schema.sql`
- `backend/internal/httpapi/handlers_interview_bank.go`
- `backend/internal/httpapi/handlers_interview_bank_test.go`
- `frontend/src/types/index.ts`
- `frontend/src/api/client.ts`
- `frontend/src/features/interviewBank/InterviewBankAdminPage.tsx`
- `frontend/src/features/interviewBank/InterviewBankAdminPage.css`

## 不做

- 不实现候选 dismiss。
- 不实现批量状态更新。
- 不实现自动关闭或自动重开。
