# 实施计划

## 顺序

1. 加载 `trellis-before-dev`，读取当前任务 PRD/design/implement 和 backend/frontend 规范。
2. RED：后端 HTTP 测试，管理员保存一个候选后 open 队列读回，source/dedupe/evidence 保留。
3. GREEN：新增 domain 保存请求/响应类型、save handler、候选到动作转换。
4. RED：active 同 key 候选保存被跳过，resolved 同 key 允许新建。
5. GREEN：复用 `activeInterviewBankOpsActionKeys()` 实现保存前去重。
6. RED：非管理员和非法候选请求被拒绝。
7. GREEN：补权限、source、dedupe、目标范围和数量校验。
8. 前端类型/API client 增加 `saveInterviewBankOpsActionCandidates`。
9. 前端 `OpsActionPanel` 加候选生成、勾选、保存、保存结果反馈。
10. 更新 `.trellis/spec/backend/store-schema-contracts.md`、`docs/architecture.md`、`features/interview-bank-ops-actions.md`。
11. 运行验证并提交。

## 验证命令

- `cd backend; go test ./internal/httpapi ./internal/store`
- `cd backend; go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 风险文件

- `backend/internal/domain/interview_bank.go`
- `backend/internal/httpapi/handlers_interview_bank.go`
- `backend/internal/httpapi/handlers_interview_bank_test.go`
- `frontend/src/api/client.ts`
- `frontend/src/types/index.ts`
- `frontend/src/features/interviewBank/InterviewBankAdminPage.tsx`
- `frontend/src/features/interviewBank/InterviewBankAdminPage.css`
- `.trellis/spec/backend/store-schema-contracts.md`
- `docs/architecture.md`
- `features/interview-bank-ops-actions.md`

## 不做

- 不改数据库 schema。
- 不新增 Store 方法。
- 不实现动作详情。
- 不实现状态流转和历史。
- 不实现候选忽略/dismiss。
- 不自动修改题库或触发索引重建。
