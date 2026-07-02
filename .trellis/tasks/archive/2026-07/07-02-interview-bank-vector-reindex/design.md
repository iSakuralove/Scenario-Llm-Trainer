# 设计：面试题库向量索引重建

## 架构边界

本任务在现有 Go + React + PostgreSQL 单体内补齐题库向量基础设施，不引入外部任务队列、Qdrant、Redis 队列或新的服务进程。

后端边界：

- HTTP API 继续挂在 `backend/internal/httpapi` 的 admin 路由下。
- 题库领域对象继续使用 `backend/internal/domain/interview_bank.go`。
- embedding 复用现有 `ai.EmbeddingClient` 和环境变量配置。
- 持久化继续通过 `backend/internal/store` 隔离 MemoryStore 与 PostgresStore。
- 题库向量文档与场景题向量文档分表保存，避免把 `scenario_vector_documents` 的 `question_id` / `source_version` 语义混用于题库 atom。

前端边界：

- 管理入口继续放在 `frontend/src/features/interviewBank/InterviewBankAdminPage.tsx`。
- 只在管理员题库治理页提供重建能力。
- 用户侧 `InterviewsPage`、Launchpad 和面试运行时不在本任务中切换到题库向量检索。

## 数据模型

新增题库向量文档表建议命名为 `interview_knowledge_vector_documents`：

- `id TEXT PRIMARY KEY`
- `atom_id TEXT NOT NULL REFERENCES interview_knowledge_atoms(id) ON DELETE CASCADE`
- `atom_version INT NOT NULL`
- `doc_type TEXT NOT NULL`
- `doc_key TEXT NOT NULL`
- `doc_text TEXT NOT NULL`
- `text_hash TEXT NOT NULL`
- `metadata JSONB DEFAULT '{}'`
- `embedding_model TEXT`
- `embedding_dim INT`
- `embedding vector(1536)`
- `status TEXT DEFAULT 'active'`
- `created_at TIMESTAMPTZ DEFAULT NOW()`
- `updated_at TIMESTAMPTZ DEFAULT NOW()`
- `UNIQUE(atom_id, atom_version, doc_type, doc_key)`

`interview_knowledge_atoms.vector_status` 继续使用：

- `pending`：待索引或内容更新后需要重新索引。
- `indexed`：最近一次成功写入题库向量文档。
- `failed`：最近一次索引失败。

`last_indexed_at` 只在成功索引时更新；失败不覆盖上一次成功时间。

## 文档构建

为每个 `InterviewKnowledgeAtom` 构建多个稳定文档：

- `overview`：标题、考察点、领域、难度、分类、题目角色、标签。
- `principle`：每条 `principles` 单独生成文档。
- `pitfall`：每条 `pitfalls` 单独生成文档。
- `follow_up`：每条 `follow_up_paths` 单独生成文档。

文档 ID 由 `atom_id | atom_version | doc_type | doc_key` 组成，与现有场景题向量文档的稳定 ID 风格保持一致。

只索引 `status=published` 的 atom。`draft` / `archived` 被请求重建时不调用 embedding，并删除或停用已有该 atom 的题库向量文档。

## API 草案

新增 admin-only 接口：

- `POST /api/v1/admin/interview-bank/index/rebuild`

请求体：

```json
{
  "atom_ids": ["atom-1", "atom-2"],
  "vector_status": "pending_failed",
  "limit": 50
}
```

语义：

- `atom_ids` 非空时按 ID 精确重建。
- `atom_ids` 为空时按 `vector_status` 选择候选，MVP 默认 `pending_failed`。
- `vector_status` 支持 `pending`、`failed`、`pending_failed`。
- `limit` 用于限制一次请求处理数量，防止长时间占用 API 请求。

响应体：

```json
{
  "total": 2,
  "indexed": 1,
  "failed": 1,
  "skipped": 0,
  "results": [
    {
      "atom_id": "atom-1",
      "status": "indexed",
      "doc_count": 7,
      "embedding_model": "text-embedding-3-small"
    },
    {
      "atom_id": "atom-2",
      "status": "failed",
      "error": "embedding provider returned status 429"
    }
  ]
}
```

## 执行模式

MVP 采用同步限量请求：

- 单次最多处理固定数量 atom，例如 50。
- 前端展示按钮 loading 和结果摘要。
- 后端逐条处理，单条失败不阻断其它 atom。
- 不引入持久化 job 表、后台 worker、SSE 或重启续跑。

理由：

- 当前项目虽有 AIJob 能力，但它服务于 AI 生成任务，不是通用后台任务系统。
- 本任务的核心价值是让索引状态真实闭环；同步限量能覆盖演示和管理端日常修复。
- 真正的大批量异步队列、重试策略和任务恢复可以后续独立设计。

异步任务、轮询接口、取消/恢复和持久化 job 结果不进入本任务。

## 错误处理

- embedding client 未配置：本次候选全部标记为 `failed`，返回明确错误。
- embedding 返回数量不一致、向量为空或维度异常：当前 atom 标记为 `failed`。
- 向量表写入失败：当前 atom 标记为 `failed`。
- draft / archived：跳过 embedding，并删除或停用旧向量文档，返回 `skipped`。
- 未找到 atom ID：返回单条 `failed` 或 `skipped`，不影响其它 ID。

## 兼容与迁移

- `SaveInterviewKnowledgeAtomVersioned` 不负责触发索引重建，避免导入发布接口被外部 embedding provider 阻塞。
- 内容发布后默认仍可保留 `pending`，由管理员显式触发重建。
- Store 新增运行时状态更新方法，不创建 `InterviewKnowledgeAtomVersion`。
- `docs/architecture.md` 和 `.trellis/spec/backend/store-schema-contracts.md` 需要记录题库索引状态与版本快照的边界。

## 风险

- 同步请求处理过多 atom 会触发超时或 provider 限流，因此必须限制 `limit`。
- 如果失败时覆盖 `last_indexed_at`，会丢失最后一次成功索引时间，不能这么做。
- 如果将题库向量文档写入 `scenario_vector_documents`，后续运行时检索语义会混乱，应分表。
- 如果导入发布时自动触发索引，发布接口会受 embedding provider 可用性影响，管理体验变差。
