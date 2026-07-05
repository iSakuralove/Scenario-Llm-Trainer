# 实施计划

## 顺序

1. 加载 `trellis-before-dev`，读取当前任务 PRD/design/implement 和 backend/frontend 规范。
2. 用 TDD 写 retrieval fallback HTTP 行为测试：回退组合生成 `fill_gap` 候选且不落库。
3. 实现 `retrieval_analytics` source 允许逻辑和 fallback combination 生成规则。
4. 写 low-hit 行为测试：零命中 followup/mixed atom 生成 `observe/P3`，已命中 atom 不生成。
5. 实现 low-hit 规则和 compact evidence。
6. 调整现有 invalid source 测试，确保真正非法 source 仍被拒绝。
7. 补前端类型 union。
8. 更新 features、architecture 和 backend spec。
9. 运行验证并提交。

## 验证命令

- `cd backend; go test ./internal/httpapi ./internal/store`
- `cd backend; go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 风险文件

- `backend/internal/httpapi/handlers_interview_bank.go`
- `backend/internal/httpapi/handlers_interview_bank_test.go`
- `frontend/src/types/index.ts`
- `.trellis/spec/backend/store-schema-contracts.md`
- `docs/architecture.md`
- `features/interview-bank-ops-actions.md`

## 不做

- 不改数据库 schema。
- 不新增 Store 方法。
- 不保存候选。
- 不实现候选选择保存。
- 不新增 UI 面板。
