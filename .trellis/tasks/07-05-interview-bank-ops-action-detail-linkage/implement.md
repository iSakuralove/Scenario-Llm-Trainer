# 实施计划

## 顺序

1. 读取 backend/frontend 规范与当前任务 PRD/design。
2. RED：后端 HTTP 测试，管理员读取一个动作详情，返回动作本身与当前 atom 上下文。
3. GREEN：新增 domain 详情类型、Store `GetInterviewBankOpsAction`、detail handler。
4. RED：动作关联 atom 不存在或已归档时，详情返回 `stale=true` 和原因。
5. GREEN：补 stale 计算与 404/权限校验。
6. 前端类型/API client 新增动作详情契约。
7. 前端 open 队列增加“详情”按钮和详情面板，展示 evidence、当前 atom 状态、stale 提示。
8. 前端在详情中复用现有“套用组合”“查看原子”行为。
9. 前端在 `rebuild_index + atom_id` 场景复用现有重建 API，实现单 atom 重建入口。
10. 更新 `docs/architecture.md` 与 `features/interview-bank-ops-actions.md`。
11. 运行后端测试、前端 lint/build。

## 验证命令

- `cd backend; go test ./internal/httpapi ./internal/store`
- `cd backend; go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 风险文件

- `backend/internal/domain/interview_bank.go`
- `backend/internal/store/interface.go`
- `backend/internal/store/memory.go`
- `backend/internal/store/postgres.go`
- `backend/internal/httpapi/handlers_interview_bank.go`
- `backend/internal/httpapi/handlers_interview_bank_test.go`
- `frontend/src/types/index.ts`
- `frontend/src/api/client.ts`
- `frontend/src/features/interviewBank/InterviewBankAdminPage.tsx`
- `frontend/src/features/interviewBank/InterviewBankAdminPage.css`
- `docs/architecture.md`
- `features/interview-bank-ops-actions.md`

## 不做

- 不实现动作状态流转和动作历史。
- 不新增 schema/migration。
- 不做组合级一键重建。
- 不改候选生成/候选保存策略。
