# 实施计划：面试题库向量索引重建

## 实施顺序

1. 读取规范与现有实现
   - `.trellis/spec/backend/index.md`
   - `.trellis/spec/frontend/index.md`
   - `.trellis/spec/backend/store-schema-contracts.md`
   - `backend/internal/ai/embedding.go`
   - `backend/internal/ai/scenario_vector.go`
   - `backend/internal/store/vector.go`
   - `backend/internal/store/postgres_vector.go`
   - `backend/internal/httpapi/handlers_interview_bank.go`
   - `frontend/src/features/interviewBank/InterviewBankAdminPage.tsx`

2. 后端领域与向量文档
   - 新增题库向量文档类型和构建函数。
   - 文档覆盖 overview、principle、pitfall、follow_up。
   - 复用 hash 和文本规范化策略，避免重复空白导致 hash 抖动。

3. Store 能力
   - 扩展 Store 接口，增加题库索引状态更新方法。
   - 增加题库向量存储接口或扩展现有 VectorStore 的题库专用方法。
   - 实现 MemoryStore / MemoryVectorStore。
   - 实现 PostgresStore / PostgresVectorStore。
   - 更新 `schema.go` 与 `backend/migrations/001_schema.sql`。

4. 后端 Admin API
   - 新增 `POST /api/v1/admin/interview-bank/index/rebuild`。
   - 支持 `atom_ids`、`vector_status`、`limit`。
   - 复用 admin 权限校验。
   - 按 atom 逐条处理，单条失败不阻断整体。
   - 成功更新为 `indexed` 并写 `last_indexed_at`。
   - 失败更新为 `failed`，不覆盖 `last_indexed_at`。
   - draft / archived 删除或停用旧向量文档并返回 skipped。

5. 前端 API 与页面
   - 在 `frontend/src/api/client.ts` 增加重建接口。
   - 在 `frontend/src/types/index.ts` 增加请求/响应类型。
   - 在题库治理页增加行选择能力。
   - 增加“重建待索引/失败”和“重建选中”操作。
   - 展示加载、成功、失败、跳过摘要。
   - 重建后刷新 summary、atoms、batches。

6. 文档更新
   - 新增或更新 `features/interview-bank-vector-reindex.md`。
   - 更新 `docs/architecture.md` 的题库索引边界。
   - 更新 `.trellis/spec/backend/store-schema-contracts.md` 的 Store/Schema 合同。

7. 验证
   - 后端单元测试：文档构建、状态更新、MemoryVectorStore。
   - 后端 API 测试：admin 权限、成功、失败、跳过、按 ID、按状态。
   - PostgreSQL schema 测试：schema.go 与 migration 同步。
   - 前端 lint/build。

## 验证命令

- `go test ./...`（在 `backend/` 目录）
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 高风险文件

- `backend/internal/store/interface.go`
- `backend/internal/store/vector.go`
- `backend/internal/store/postgres_vector.go`
- `backend/internal/store/postgres.go`
- `backend/internal/store/memory.go`
- `backend/internal/store/schema.go`
- `backend/migrations/001_schema.sql`
- `backend/internal/httpapi/handlers_interview_bank.go`
- `frontend/src/api/client.ts`
- `frontend/src/types/index.ts`
- `frontend/src/features/interviewBank/InterviewBankAdminPage.tsx`

## 回滚点

- 如果向量文档表设计不稳定，先回退索引写入和 API，不修改导入发布链路。
- 如果前端选择交互风险过大，可先保留按状态重建，按 ID 重建只开放 API。
- 如果 embedding provider 在测试环境不可用，使用 mock embedding client 覆盖 API 行为，不让单元测试依赖外部网络。

## 已确认实施约束

- 重建执行模式采用同步限量请求。
- 不新增异步任务表、后台 worker、轮询、取消或重启续跑。
