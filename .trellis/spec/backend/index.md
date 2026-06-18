# Backend Spec

后端规范记录 Go API、Store、数据库 schema 和迁移的可执行契约。涉及 `backend/` 代码、`backend/migrations/` 或持久化数据结构时必须阅读相关条目。

## Pre-Development Checklist

- 读取 [store-schema-contracts.md](./store-schema-contracts.md) 中与 Store/schema/migration 相关的契约。
- 修改 `Store` 接口前，确认 MemoryStore 与 PostgresStore 都能同步实现。
- 修改数据库结构前，同时检查 `backend/internal/store/schema.go` 与 `backend/migrations/001_schema.sql`。

## Quality Check

- 运行 `go test ./...`。
- 如本地 Postgres 可用，设置 `POSTGRES_TEST_URL` 后运行相关 store 集成测试。
- 涉及前端或根脚本验收时运行根目录 `npm test`。
