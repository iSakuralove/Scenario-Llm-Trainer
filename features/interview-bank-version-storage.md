# 面试题库版本治理数据层

## 目标

落地 AI-Interview 接入方案的首个可验证数据层切片：为面试题库知识原子、版本历史、导入批次、检索日志和会话快照提供稳定存储契约。

## 修改范围

- 新增面试题库治理领域类型。
- 扩展 Store 接口、MemoryStore 和 PostgresStore。
- 新增 Postgres 表结构与 Docker 初始化 schema。
- 扩展 `interview_sessions`，支持开场题快照与追问命中轻量快照。
- 补充 Store 行为测试、Postgres 集成测试和 schema 文本测试。
- 补充后端 Store/schema Trellis code-spec 与架构说明。

## 核心实现

- `SaveInterviewKnowledgeAtomVersioned` 统一处理版本事件，版本从 `1` 开始并按事件单调递增。
- `duplicate_import` 即使内容不变也会生成版本记录，并标记 `no_content_change=true`。
- 版本 `snapshot` 只保存标准化内容字段，不保存 `vector_status`、`last_indexed_at` 等运行时索引状态。
- Postgres 写入使用事务和当前 atom 行锁，保证主表 `current_version` 与版本表记录一致。
- 版本历史按 `created_at DESC, version DESC` 返回。

## 影响范围

- 后续管理端 API、导入发布、在线编辑、归档恢复和运行时追问检索可复用同一版本写入契约。
- 旧 `interview_questions` 和现有面试 API 未切换到新题库主链路。
- 正在进行中的面试快照字段已具备存储能力，但本轮未接入运行时写入逻辑。

## 验证方式

- `go test ./...`
- `$env:POSTGRES_TEST_URL="postgres://teaching:teaching@localhost:5432/teaching_mvp?sslmode=disable"; go test ./internal/store -run TestPostgresInterviewKnowledgeAtomVersionedCRUD -count=1 -v`
- `npm test`

## 已知限制

- 本轮只实现数据层，不包含管理端 API、前端页面、导入 UI、在线编辑接口、向量检索接入或旧题迁移。
- `diff_summary` 当前是字段级摘要，仅用于展示，不支持历史版本恢复。
