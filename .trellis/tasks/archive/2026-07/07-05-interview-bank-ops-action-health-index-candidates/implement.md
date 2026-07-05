# 实施计划

## 顺序

1. 用 CodeGraph 定位现有健康诊断、动作列表、admin handler 和测试结构。
2. 按 TDD 写第一个 HTTP 行为测试：blocked 健康组合生成候选且不落库。
3. 实现最小 domain 类型、候选生成函数和 `POST /ops-actions/candidates` handler。
4. 继续 TDD 覆盖 failed/pending atom、draft/archived 跳过、active dedupe、权限和 limit/sources。
5. 视编译需要补前端类型/API client；本切片不做完整候选保存 UI。
6. 更新 feature 文档。
7. 运行验证命令。

## 验证命令

- `cd backend; go test ./internal/httpapi ./internal/store`
- `cd backend; go test ./...`
- 如修改前端类型/client：`npm --prefix frontend run lint`
- 如修改前端类型/client：`npm --prefix frontend run build`

## 风险文件

- `backend/internal/domain/interview_bank.go`
- `backend/internal/httpapi/handlers_interview_bank.go`
- `backend/internal/httpapi/handlers_interview_bank_test.go`
- `backend/internal/store/interview_bank.go`
- `frontend/src/api/client.ts`
- `frontend/src/types/index.ts`
- `features/interview-bank-ops-actions.md`

## 不做

- 不修改数据库 schema。
- 不实现候选保存。
- 不接入真实检索运营候选。
- 不实现动作详情和状态流转。
- 不触发索引重建或编辑题目。
