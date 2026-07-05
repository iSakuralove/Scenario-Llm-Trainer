# 实施计划

## 总体顺序

1. 首个 TDD tracer bullet：手工创建运营动作并列表读回。
2. 健康诊断与索引状态生成动作候选。
3. 真实检索运营数据生成动作候选。
4. 候选动作选择保存与 active dedupe。
5. 动作详情联动现有题库治理入口。
6. 动作关闭、忽略、观察与重开闭环。
7. Chrome 管理员真实流程验收。

## 首个切片 TDD 循环

### RED 1

写 admin HTTP API 行为测试：

- 管理员 `POST /admin/interview-bank/ops-actions` 创建手工动作。
- 管理员 `GET /admin/interview-bank/ops-actions` 能看到刚创建的动作。

预期先失败，因为类型、Store 和 handler 尚不存在。

### GREEN 1

最小实现：

- 增加 domain 类型和 Store 接口。
- MemoryStore 保存/list。
- Admin handler 创建/list。

### RED 2

补权限测试：

- 非管理员创建动作被拒绝。
- 非管理员读取列表被拒绝。

### GREEN 2

复用现有 admin-only 路由边界修正 handler。

### RED 3

补校验测试：

- 缺少标题或原因拒绝。
- 非法 action_type / priority 拒绝。
- 创建后默认 `status=open`。

### GREEN 3

补最小校验和默认值。

### RED 4

补 Store 测试：

- MemoryStore clone-safe。
- 列表按 `updated_at DESC` 或 `created_at DESC` 稳定排序。
- status/type/combination 过滤可用。

### GREEN 4

实现 Store helper 并让 MemoryStore 通过。

### RED 5

补 schema 文本测试或现有 schema 断言：

- runtime schema 和 `001_schema.sql` 都包含运营动作主表和必要索引。

### GREEN 5

实现 Postgres schema/migration 和 PostgresStore。

### RED 6

前端最小契约：

- API client/type 编译失败点先暴露。
- 页面接入动作面板后 lint/build 通过。

### GREEN 6

补前端类型、API client、最小面板、空状态和创建表单。

## 后续切片原则

- 每个切片先写一个端到端行为测试，不批量先写全部测试。
- 候选生成可有纯函数 deep module 测试，但必须以业务行为命名。
- 前端每次只接入一个用户可见行为，避免一次性塞完整面板。

## 验证命令

首个切片至少运行：

- `cd backend; go test ./internal/httpapi ./internal/store`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

涉及 schema、PostgresStore 或共享接口后补跑：

- `cd backend; go test ./...`

## 风险文件

- `backend/internal/domain/interview_bank.go`
- `backend/internal/store/interface.go`
- `backend/internal/store/memory.go`
- `backend/internal/store/postgres.go`
- `backend/internal/store/schema.go`
- `backend/migrations/001_schema.sql`
- `backend/internal/httpapi/handlers_interview_bank.go`
- `backend/internal/httpapi/handlers_interview_bank_test.go`
- `frontend/src/api/client.ts`
- `frontend/src/types/index.ts`
- `frontend/src/features/interviewBank/InterviewBankAdminPage.tsx`
- `frontend/src/features/interviewBank/InterviewBankAdminPage.css`

## 不做

- 不在首个切片实现候选生成。
- 不自动修改题库内容。
- 不自动重建索引。
- 不改普通用户报告接口。
- 不引入 LLM 调用。
