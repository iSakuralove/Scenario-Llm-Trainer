# 设计：面试题库版本治理数据层

## 架构边界

本轮在现有后端内实现，不引入独立服务：

- `domain`：新增题库治理领域类型。
- `store`：扩展 Store 接口，并在 MemoryStore / PostgresStore 中实现最小读写。
- `schema` 与 `migrations`：新增表结构和幂等迁移。
- `httpapi`：本轮不新增接口，避免 API/UI 与数据层混在同一提交。

## 数据表

### `interview_knowledge_atoms`

保存当前版本内容与治理状态。

关键字段：

- `id TEXT PRIMARY KEY`
- `title / subject / domain / difficulty / category / question_role`
- `source_ref`
- `tags TEXT[]`
- `principles JSONB`
- `pitfalls JSONB`
- `follow_up_paths JSONB`
- `status TEXT DEFAULT 'draft'`
- `current_version INT DEFAULT 1`
- `vector_status TEXT DEFAULT 'pending'`
- `last_indexed_at TIMESTAMPTZ`
- `created_at / updated_at`

### `interview_knowledge_atom_versions`

保存审计和版本历史。

关键字段：

- `id TEXT PRIMARY KEY`
- `atom_id TEXT NOT NULL`
- `version INT NOT NULL`
- `version_type TEXT NOT NULL`
- `admin_id TEXT`
- `change_note TEXT`
- `snapshot JSONB NOT NULL`
- `diff_summary JSONB DEFAULT '{}'`
- `no_content_change BOOLEAN DEFAULT FALSE`
- `created_at TIMESTAMPTZ`

约束：

- `UNIQUE(atom_id, version)`
- `version_type` 限定在首期枚举内

### `interview_knowledge_batches`

记录导入批次，不承载单题版本历史。

关键字段：

- `id TEXT PRIMARY KEY`
- `source_ref`
- `status`
- `mode`
- `atom_count`
- `validation_report JSONB`
- `publish_note`
- `admin_id`
- `created_at / updated_at`

### `interview_retrieval_logs`

记录运行时追问检索命中和降级信息。

关键字段：

- `id TEXT PRIMARY KEY`
- `session_id`
- `round`
- `query_text`
- `matched_atoms JSONB`
- `fallback_used BOOLEAN`
- `error_message`
- `created_at`

## Store 合约

本轮新增最小合约：

- `SaveInterviewKnowledgeAtomVersioned(atom, versionType, adminID, changeNote) (atom, version, error)`
- `GetInterviewKnowledgeAtom(id) (atom, bool)`
- `ListInterviewKnowledgeAtomVersions(atomID) []version`

保存函数负责：

- 新题从版本 1 开始。
- 已有题每次事件推进 `current_version`。
- 内容无变化但 `versionType=duplicate_import` 仍推进版本。
- 生成标准化快照，排除运行时索引字段。

## 兼容与迁移

- 旧 `interview_questions` 保留，不在本轮迁移。
- `interview_sessions` 只新增 JSONB 快照字段，不改变现有创建会话逻辑。
- 迁移使用 `CREATE TABLE IF NOT EXISTS` 与 `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`，允许重复执行。

## 回滚

- 代码层回滚可移除 Store 新方法和领域类型。
- 数据库回滚不自动删除表，避免误删已导入题库数据；后续若需要 destructive rollback，单独写运维脚本。
