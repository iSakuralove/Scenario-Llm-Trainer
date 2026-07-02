# 实现面试题库治理管理端 MVP

## 目标

为 AI-Interview 能力迁移补齐首期“面试题库治理”后台闭环，让管理员可以把结构化面试题库资源导入当前 Go + React + PostgreSQL 运行时，并让用户侧 Launchpad 后续能够消费真实题库状态，而不是长期依赖兼容种子。

## 用户价值

- 管理员可以维护一批可发布、可追踪、可筛选的面试题库资源。
- 面试舱的可启动组合具备后台治理来源，为后续动态追问和报告增强提供数据基础。
- 比赛演示时能清楚展示“题库治理驱动训练入口”的产品链路。

## 已确认事实

- `docs/ai-interview-integration-tech-design.md` 的实施顺序中，数据层后下一步是“补管理接口和最小管理界面”。
- `interview-cabin-restructure.md` Phase 2 明确要求“题库治理最小管理端”。
- 当前后端已有 `InterviewKnowledgeAtom`、`InterviewKnowledgeAtomVersion`、`InterviewKnowledgeBatch`、`InterviewRetrievalLog` 领域对象。
- 当前 Store 已支持 `SaveInterviewKnowledgeAtomVersioned`、`GetInterviewKnowledgeAtom`、`ListInterviewKnowledgeAtomVersions`。
- 当前后端 admin 路由已有用户、Prompt、AI 配置、审计接口，但还没有面试题库治理接口。
- 当前前端 admin 入口只有 `/system`，没有独立的 Interview Bank 管理页面。
- 当前用户侧 `GET /api/v1/interviews/launchpad` 已接入，但仍以兼容题库轨道为主。
- 8080 端口冲突问题已由用户确认解决，不纳入本任务。
- 本任务只做 `vector_status=failed` 筛选和状态展示，不真实触发索引重建。
- 真实触发索引重建是后续独立任务，已记录到项目记忆。

## 范围

### 后端

- 在现有 `/api/v1/admin` 路由下新增面试题库治理接口。
- 支持结构化导入包校验，返回 error / warning / 新增 / 更新 / 重复导入统计。
- 支持导入预览，不写正式题库。
- 支持正式发布，写入 `InterviewKnowledgeAtom` 并保留版本记录。
- 支持题库列表和基础筛选：`status`、`domain`、`difficulty`、`category`、`question_role`、`vector_status`。
- 支持批次记录的最小展示数据：批次 ID、导入时间、操作者、统计、状态。
- 支持 `vector_status=failed` 筛选，但不实现真实重建索引动作。
- 支持系统状态页读取题库治理摘要。
- 管理接口仅允许 `admin` 访问。

### 前端

- 在 admin 区域新增“面试题库”入口。
- 新增 Interview Bank 管理页面。
- 页面支持导入包粘贴或上传 JSON 文本、校验、预览、发布。
- 页面展示题库列表、批次摘要、状态筛选、错误/警告信息。
- 页面提供 `vector_status=failed` 筛选入口。
- 页面不提供会真实触发索引重建的操作；如展示“重建索引”，只能作为后续能力提示或禁用态入口。
- 系统状态页只展示题库治理摘要，不承载导入、编辑和版本历史操作。

### 文档与验证

- 新增 `features/` 文档记录本次实现。
- 必要时更新 `docs/architecture.md`，说明题库治理 API 与 Store 边界。
- 后端补充单元测试或 API 回归测试。
- 前端运行 lint/build，必要时补充轻量测试或浏览器冒烟验证。

## 不在本任务范围

- 动态 RAG 追问。
- 面试运行时从新题库选择开场题的完整切换。
- 在线编辑已发布题。
- `base_version` 冲突处理。
- 版本历史详情页。
- 归档、恢复归档和二次确认。
- 真实触发索引重建、索引任务队列、失败重试和异步运行状态。
- 复杂 Topic 覆盖热力图。
- AI Mentor、简历解析、报告页增强。
- Docker 8080 端口冲突排查。

## 验收标准

1. 管理员能打开“面试题库”管理页面。
2. 非管理员不能访问题库治理接口和页面。
3. 管理员提交结构化导入包后，可以看到校验结果、错误、警告和统计。
4. 校验/预览不会写入正式题库。
5. 发布成功后，题库列表能看到新增或更新后的题库资源。
6. 重复导入同一稳定 ID 时，不创建重复题目，并保留版本记录。
7. 列表筛选至少覆盖状态、领域、难度、题目角色和索引状态。
8. 系统状态页能展示题库治理摘要。
9. 现有面试舱启动、提交、报告链路不回归。
10. 相关测试、lint、build 通过。

## 待确认问题

无。
