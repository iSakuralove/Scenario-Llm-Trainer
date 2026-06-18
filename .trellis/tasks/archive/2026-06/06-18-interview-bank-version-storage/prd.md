# 实现面试题库版本治理数据层

## 目标

把已定稿的 AI-Interview 接入文档从方案推进到首个可验证实现切片：新增面试题库知识原子、版本历史、导入批次和检索日志的数据层基础，为后续管理端 API、导入发布、在线编辑、归档恢复和追问检索提供稳定存储契约。

## 背景与证据

- 当前项目后端是 Go + Postgres + 内存 Store，已有 `interview_questions` 与 `interview_sessions`，但还没有新题库治理表。
- `docs/ai-interview-integration-prd.md` 与 `docs/ai-interview-integration-tech-design.md` 已确认首期采用能力迁移，不整仓并入 `AI-Interview`。
- 参考项目位于 `tmp/AI-Interview-ref`，其中 `backend/src/main/resources/db/migration/V6__add_question_bank.sql` 提供了 `knowledge_atom`、`knowledge_atom_version`、`knowledge_atom_import_batch` 等参考模型。
- 本项目已确认版本治理口径比参考项目更严格：需要 `version_type`、`admin_id`、`diff_summary`、`no_content_change`、`current_version` 和冲突用 `base_version`。

## 本轮范围

- 新增领域类型，覆盖：
  - 面试知识原子
  - 知识原子版本
  - 导入批次
  - 检索日志
- 扩展 Postgres schema 与迁移 SQL：
  - 新增 `interview_knowledge_atoms`
  - 新增 `interview_knowledge_atom_versions`
  - 新增 `interview_knowledge_batches`
  - 新增 `interview_retrieval_logs`
  - 扩展 `interview_sessions`，支持开场题快照与追问命中快照
- 扩展 Store 接口、MemoryStore 和 PostgresStore 的最小读写能力。
- 添加后端测试，证明：
  - 新建或更新知识原子会维护 `current_version`
  - 重复导入即使内容不变也生成版本记录
  - `snapshot` 包含标准化内容字段，不包含 `vector_status / last_indexed_at`
  - 版本历史按 `created_at DESC` / version 倒序可读

## 不在本轮范围

- 不做管理端页面。
- 不做完整导入 UI。
- 不做在线编辑 API。
- 不做追问向量检索接入。
- 不迁移旧 `interview_questions` 数据。
- 不整合参考项目的 Spring Boot / MyBatis 实现。

## 需求

- `interview_knowledge_atoms` 只保存当前生效版本，必须包含 `current_version`。
- `interview_knowledge_atom_versions` 保存历史版本，版本号从 1 开始，按事件单调递增。
- 版本类型首期至少支持 `content_update / duplicate_import / manual_edit / restore_archived`。
- `snapshot` 保存完整标准化字段：`id / title / subject / domain / difficulty / category / question_role / sourceRef / tags / principles / pitfalls / followUpPaths / status`。
- `snapshot` 不保存 `vector_status / last_indexed_at` 这类运行时索引状态。
- `diff_summary` 使用 JSONB，仅用于摘要展示，不承担恢复能力。
- 版本表需要支持按 `(atom_id, version DESC)`、`version_type`、`admin_id`、`created_at` 查询。
- MemoryStore 与 PostgresStore 的行为口径保持一致。
- `.gitignore` 需要保持干净提交：继续忽略 `tmp/` 参考项目和本地工具状态，但不得重新忽略正式 `docs/` 或 `*.md`。

## 验收标准

- `go test ./...` 通过。
- `npm test` 通过，或能证明前端未受影响且后端完整验证通过。
- 新增/修改的 SQL schema 与 `backend/migrations/001_schema.sql` 保持一致。
- Store 最小能力有单元测试覆盖。
- 提交只包含本任务相关文件，不混入 `.agents/`、`.codex/`、参考项目或其它历史未跟踪文件。

## 已知限制

- 本轮只建立数据层契约，后续还需要 API、导入发布流程、管理端 UI、运行时追问检索和报告展示任务。
