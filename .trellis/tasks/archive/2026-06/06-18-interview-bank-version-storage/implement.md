# 实施计划：面试题库版本治理数据层

## 顺序

1. 读取 `trellis-before-dev` 与适用规范。
2. 新增领域类型和 snapshot 构造规则。
3. 扩展 `backend/internal/store/schema.go` 与 `backend/migrations/001_schema.sql`。
4. 扩展 `Store` 接口。
5. 实现 MemoryStore 版本写入和查询。
6. 实现 PostgresStore 版本写入和查询。
7. 添加针对 MemoryStore 的行为测试；必要时补 schema 文本测试。
8. 运行验证：
   - `go test ./...`
   - `npm test`
   - 文档/提交范围检查

## 风险点

- Store 接口扩展会要求 MemoryStore 和 PostgresStore 同步实现。
- Postgres JSONB / TEXT[] 扫描需要复用现有 marshal/unmarshal helper，避免手写字符串拼接。
- `.gitignore` 当前有历史未提交改动，本任务提交时只能暂存本任务相关文件。

## 参考资料

- `docs/ai-interview-integration-prd.md`
- `docs/ai-interview-integration-tech-design.md`
- `tmp/AI-Interview-ref/backend/src/main/resources/db/migration/V6__add_question_bank.sql`
- `tmp/AI-Interview-ref/backend/src/main/java/com/interview/entity/KnowledgeAtom.java`
- `tmp/AI-Interview-ref/backend/src/main/java/com/interview/entity/KnowledgeAtomVersion.java`
