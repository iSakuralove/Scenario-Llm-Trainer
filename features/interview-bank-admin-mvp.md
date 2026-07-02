# 面试题库治理管理端 MVP

## 目标

补齐 AI-Interview 能力迁移中的首期题库治理闭环，让管理员可以校验、预览、发布结构化面试题库资源，并在管理端查看题库、批次和索引状态摘要。

## 修改范围

- 扩展 Store 接口、MemoryStore 和 PostgresStore，增加题库列表、批次记录和治理摘要。
- 新增 `/api/v1/admin/interview-bank/*` 管理接口。
- 新增前端“面试题库”管理员页面和侧栏入口。
- 扩展系统状态页，展示题库治理摘要。
- 补充 Store 和 HTTP API 回归测试。
- 更新 `docs/architecture.md` 的管理端边界说明。

## 核心实现

- 导入校验支持 `items` / `atoms` 和 snake_case / camelCase 字段兼容。
- `validate` 接口只返回错误、警告、逐条结果和新增/更新/重复统计，不写库。
- `publish` 接口复用校验逻辑，通过后调用 `SaveInterviewKnowledgeAtomVersioned` 写入版本，重复导入同一内容会生成 `duplicate_import` 版本。
- 题库列表支持 `status`、`domain`、`difficulty`、`category`、`question_role`、`vector_status` 筛选。
- `vector_status=failed` 仅作为筛选和状态展示，不触发索引重建。

## 影响范围

- 管理员可以通过前端 `/interview-bank` 使用题库治理页面。
- 非管理员前端路由和后端 API 都不能访问题库治理能力。
- 系统状态页新增题库资源数、开放组合数、批次数、待索引、已索引和索引失败统计。
- 现有面试舱启动、提交和报告链路未切换到新题库选择逻辑。

## 验证方式

- `go test ./...`（在 `backend/` 目录）
- `go test ./internal/httpapi -run "TestAdminInterviewBank|TestSystemStatusRequiresAdmin"`（在 `backend/` 目录）
- `go test ./internal/store -run "TestMemory.*InterviewKnowledge|TestInterviewKnowledge"`（在 `backend/` 目录）
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 已知限制

- 不包含真实向量索引重建、索引任务队列、失败重试或异步状态。
- 不包含在线编辑、归档恢复、版本详情页和 `base_version` 冲突处理。
- 不切换用户侧 Launchpad 的题库选择主链路，不实现动态 RAG 追问。
