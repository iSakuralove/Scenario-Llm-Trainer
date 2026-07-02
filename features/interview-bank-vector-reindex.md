# 面试题库向量索引重建

## 目标

为面试题库治理后台补齐真实向量索引重建能力，让管理员可以把 `pending` / `failed` 的已发布题库资源推进到可检索状态，并可靠更新 `vector_status` 与 `last_indexed_at`。

## 修改范围

- 新增题库向量文档构建逻辑，覆盖 overview、principle、pitfall、follow_up 文档。
- 扩展 `VectorStore`，为 Memory/Postgres 实现题库向量文档 upsert、delete 和 rebuild。
- 扩展 `Store`，增加独立的题库索引状态更新方法，不创建内容版本。
- 新增 `POST /api/v1/admin/interview-bank/index/rebuild` 管理接口。
- 前端题库治理页增加重建待处理/失败、重建选中、行选择和结果摘要。
- 补充 API、Store、向量文档构建和 schema 相关测试。

## 核心实现

- `BuildInterviewKnowledgeVectorDocuments` 只为 `status=published` 的 atom 构建稳定文档。
- `interview_knowledge_vector_documents` 与场景题向量表分表保存，避免检索语义混用。
- 重建接口支持 `atom_ids` 精确重建，也支持 `vector_status=pending|failed|pending_failed` 批量选择候选，单次默认/最大 50 条。
- 后端逐条处理 atom，单条失败不阻断其它 atom。
- 成功时写入题库向量文档，将 atom 标记为 `indexed`，并写入 `last_indexed_at`。
- embedding 或写库失败时将 atom 标记为 `failed`，不覆盖上一次成功索引时间。
- draft / archived atom 不调用 embedding，会删除旧向量文档并返回 skipped。

## 影响范围

- 管理员可以在 `/interview-bank` 页面触发真实索引重建。
- 系统状态和题库治理页中的索引统计会随着真实重建结果变化。
- 现有导入校验、发布、列表筛选、批次记录和版本快照语义保持不变。
- 用户侧 Launchpad、面试运行时和动态 RAG 追问链路未切换到新题库向量检索。

## 验证方式

- `go test ./internal/ai ./internal/store ./internal/httpapi`（已通过）
- `go test ./...`（在 `backend/` 目录）
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 已知限制

- MVP 采用同步限量请求，不包含异步任务表、后台 worker、轮询、取消或重启续跑。
- 向量维度沿用当前 pgvector schema 的 `vector(1536)`；不在本任务中引入动态维度迁移。
- 不实现在线编辑、归档恢复、版本详情页和 `base_version` 冲突处理。
