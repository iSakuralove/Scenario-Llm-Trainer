# 面试题库向量索引重建

## 目标

为面试题库治理后台补齐真实向量索引重建能力，让管理员可以对已发布题库资源触发 embedding 生成、向量文档写入，并可靠更新 `vector_status` 与 `last_indexed_at`，替代上一阶段仅能筛选和展示 `vector_status=failed` 的占位能力。

## 用户价值

- 管理员可以把 `pending` / `failed` 的题库资源重新推进到可检索状态。
- 系统状态页和题库治理页展示的索引状态来自真实索引流程，而不是人工导入字段。
- 后续面试运行时切换到新题库、动态追问和检索增强时，已有可靠的题库向量基础设施。

## 已确认事实

- 当前任务承接已完成任务 `07-02-interview-bank-admin-mvp`。
- 上一任务已经实现 `/api/v1/admin/interview-bank/*` 管理接口、题库列表、批次、摘要和前端 `/interview-bank` 页面。
- 上一任务明确只允许 `vector_status=failed` 筛选和状态展示，不触发索引重建。
- `InterviewKnowledgeAtom` 已包含 `VectorStatus` 和 `LastIndexedAt` 字段。
- `interview_knowledge_atoms` 表已有 `vector_status VARCHAR(20) DEFAULT 'pending'` 和 `last_indexed_at TIMESTAMPTZ`。
- `SaveInterviewKnowledgeAtomVersioned` 会保留已有运行时索引状态，不把 `vector_status` / `last_indexed_at` 写入版本快照。
- 当前已有 `ai.EmbeddingClient`，可通过环境变量创建 OpenAI-compatible embedding client。
- 当前已有 `scenario_vector_documents`、`VectorStore`、`MemoryVectorStore`、`PostgresVectorStore`，但它们服务于场景题，不是面试题库。
- 场景题索引只索引 `status=active` 的题目；对应到面试题库，本任务默认只索引 `status=published` 的题库资源，避免 draft / archived 内容进入可检索索引。
- 当前 Store 接口还没有更新题库 `vector_status` / `last_indexed_at` 的独立方法，也没有题库向量文档表。
- 当前前端题库治理页已有索引状态筛选和失败状态展示，但没有真实重建操作。
- 当前会话的 CodeGraph MCP 工具未热刷新，已排查并修正全局配置；本轮规划使用 PowerShell 精确读取代码。

## 范围

### 后端

- 新增题库向量文档构建逻辑，把 `InterviewKnowledgeAtom` 转成稳定的向量文档集合。
- 新增题库向量文档持久化能力，PostgreSQL 使用 pgvector，MemoryStore/内存实现保持测试与本地模式一致。
- 新增 Store 方法用于更新 `InterviewKnowledgeAtom` 的 `vector_status` 和 `last_indexed_at`，不得创建内容版本记录。
- 新增 admin-only 索引重建接口，触发真实 embedding、写入向量文档、更新 atom 索引状态。
- MVP 同时支持重建 `pending` 和 `failed` 题库资源，并支持按选中 atom ID 精确重建。
- 重建成功时将 atom 标记为 `indexed` 并写入 `last_indexed_at`。
- 重建失败时将 atom 标记为 `failed`，返回失败原因摘要，避免吞掉 provider、写库或向量维度错误。
- 对 draft / archived atom 不写入可检索向量文档；如已有旧向量文档，应删除或标记不可用。
- 系统状态摘要继续使用真实 `InterviewKnowledgeSummary()` 统计 pending / indexed / failed。
- 保持 `/admin/interview-bank/import/validate` 和 `/publish` 的校验/发布语义不变。

### 前端

- 在题库治理页面为管理员提供索引重建入口。
- 支持对 `pending` / `failed` 资源触发重建。
- 支持对当前选中的 atom ID 精确触发重建。
- 展示重建中的加载状态、成功数量、失败数量和失败摘要。
- 重建完成后刷新摘要、列表和批次/状态视图。
- 禁止非 admin 访问或触发重建。

### 文档与验证

- 更新 `features/` 文档记录本次实现。
- 如新增表、Store 合同或索引流程，更新 `docs/architecture.md` 和 `.trellis/spec/backend/store-schema-contracts.md`。
- 增加后端单元测试/API 回归测试，覆盖成功、失败、非 admin、draft/archived 跳过、状态更新。
- 运行后端测试、前端 lint/build。

## 不在本任务范围

- 不切换用户侧 Launchpad 的题库选择主链路。
- 不实现完整动态 RAG 追问。
- 不实现分布式任务队列、跨进程恢复、任务持久化或重启续跑。
- 不实现在线编辑、版本详情页、归档恢复和 `base_version` 冲突处理。
- 不处理 Docker 8080 端口占用问题。

## 初始验收标准

1. 管理员可以从题库治理页触发真实索引重建。
2. 非管理员不能调用索引重建接口。
3. 对可索引题库资源，后端会调用 embedding client 并写入题库向量文档。
4. 成功索引后，atom 的 `vector_status` 变为 `indexed`，`last_indexed_at` 非空。
5. embedding 或写库失败时，atom 的 `vector_status` 变为 `failed`，接口返回失败摘要。
6. draft / archived atom 不进入可检索向量索引。
7. 系统状态页和题库治理页刷新后能看到真实索引状态变化。
8. 现有导入校验、发布、列表筛选和批次能力不回归。
9. 相关测试、lint、build 通过。

## 待确认问题

- 已确认：MVP 同时覆盖 `pending` 和 `failed`，并支持按选中 ID 重建。
- 已确认：重建执行模式采用同步限量请求，单次处理固定上限内的 atom，不引入异步任务表、后台 worker、轮询或重启续跑。
