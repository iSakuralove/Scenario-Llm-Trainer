# 基于AI大模型的情景式教学系统 MVP

这是根据目录内需求文档与功能技术规格书落地的比赛演示版 MVP，覆盖登录、情景题、渐进式排查会话、答案评估复盘、技术面试、面试报告、个人档案和 UGC 案例预览。

## 目录结构

```text
backend/   Go 1.22 API 服务，支持 PostgreSQL 持久化、Redis 限流、DeepSeek/OpenAI-compatible LLM 与种子数据
frontend/  React + TypeScript + Zustand 前端应用
scripts/   比赛演示验收与演示数据重置脚本
流程/      每次代码任务的实现记录
docker-compose.yml  API、PostgreSQL、Redis 本地编排
```

## 演示账号

- 学员：`demo` / `demo123`
- 讲师：`instructor` / `instructor123`
- 管理员：`admin` / `admin123`

角色权限：

- 学员：训练排查题、参加面试、发布 UGC 案例，只能查看自己的案例发布记录。
- 讲师：审核 UGC 结构化预览，决定是否提交管理员终审。
- 管理员：终审发布 UGC 转化题，并可在个人档案页维护用户角色。

管理员查看情景题时可看到完整根因和标准步骤；学员接口会自动脱敏。UGC 转化为正式情景题时，题目创建人记录为终审管理员，避免原作者以 owner 身份看到完整答案。

## 环境变量

后端支持以下变量：

```powershell
PORT=8080
JWT_SECRET=replace-with-a-long-random-secret
STORE_MODE=postgres
DATABASE_URL=postgres://teaching:teaching@localhost:5432/teaching_mvp?sslmode=disable
REDIS_URL=redis://localhost:6379/0
LLM_BASE_URL=
LLM_API_KEY=
LLM_MODEL=
ZETA_KEY=
JIANYI_API_KEY=
DEEPSEEK_KEY=
EMBEDDING_BASE_URL=https://jeniya.top
EMBEDDING_API_KEY=
jeniya_embedding_key=
EMBEDDING_MODEL=text-embedding-3-small
EMBEDDING_FALLBACK_MODEL=
EMBEDDING_TIMEOUT_SECONDS=8
STT_BASE_URL=https://api.zetatechs.com
STT_API_KEY=
STT_MODEL=gpt-4o-mini-transcribe-2025-12-15
STT_TIMEOUT_SECONDS=60
```

说明：

- `STORE_MODE=postgres` 时使用 PostgreSQL。
- `STORE_MODE=memory` 或未配置 `DATABASE_URL` 时降级为内存存储。
- `REDIS_URL` 存在且连接成功时启用接口限流；连接失败时自动降级为不限流。
- AI Provider 自动选择：存在 `DEEPSEEK_KEY` 时默认使用 DeepSeek；否则存在 `JIANYI_API_KEY` 时使用第三方中转站；都不存在时使用 mock。
- DeepSeek 固定默认地址为 `https://api.deepseek.com`，默认模型为 `deepseek-v4-flash`。
- 情景题生成任务 `scenario_generate` 会优先固定使用 `deepseek-v4-flash`；即使管理员把全局 AI 配置改成其他 DeepSeek 模型，生成题仍回到该模型执行，失败后再按 Provider 链路降级。
- `scenario_generate` 是非流式结构化 JSON 调用；验证时系统状态的 recent attempts 应显示 DeepSeek 成功，页面来源应为 `DeepSeek deepseek-v4-flash`，不应因全局流式开关误显示 `Mock LLM 兜底`。
- 第三方中转站可使用 `LLM_BASE_URL=https://jeniya.top`、`JIANYI_API_KEY=<your-key>`、`LLM_MODEL=gpt-5.5`。
- 排查会话语义网关会直接调用 `https://jeniya.top/v1/embeddings`；Key 读取顺序为 `EMBEDDING_API_KEY`、`jeniya_embedding_key`、`JIANYI_API_KEY`，推荐用 `jeniya_embedding_key` 放置 embedding 专用 key。默认模型为 `text-embedding-3-small`；如果需要切换模型，请确认该模型仍在 embeddings 端点可用。未配置 key 或调用失败时，后端保留本地相似度与关键词规则，不影响 Go 后端单独运行。
- 语音转写优先使用 `STT_API_KEY`，其次 `ZETA_KEY`，最后兼容 `JIANYI_API_KEY`；存在 `ZETA_KEY` 时默认 `STT_BASE_URL=https://api.zetatechs.com`，默认 `STT_MODEL=gpt-4o-mini-transcribe-2025-12-15`。
- Zeta 可选路线包括 `https://api.zetatechs.com`、`https://api.zetatechs.online`、`https://ent.zetatechs.com`、`https://ent.zetatechs.online`，用 `STT_BASE_URL` 切换。
- 不要把真实模型 Key 写入仓库；用系统环境变量或本机 `.env` 注入。

## 向量数据库与排查评分

排查会话的证据检索和评分优先走 PostgreSQL + pgvector 主路线；Docker Compose 提供可直接启动的 PostgreSQL/pgvector 镜像环境，用于持久化索引和比赛演示。Go 后端在没有 Docker、PostgreSQL 或 pgvector 扩展时会自动降级为内存向量索引，保留本地验证、开发和基础排查评分能力。

详细验证步骤见 `docs/vector-scoring-verification.md`。文档中的命令使用占位配置，不写入任何真实 API key。

## Docker 启动

推荐用 Docker Compose 启动后端依赖和 API：

```powershell
docker compose up --build api
```

服务地址：

- API：`http://localhost:8080`
- 健康检查：`http://localhost:8080/healthz`
- AI 状态：`http://localhost:8080/api/v1/system/ai`
- PostgreSQL：`localhost:5432`
- Redis：`localhost:6379`

数据会保存在 Docker volume `teaching-mvp_postgres-data` 中。需要清空数据库时执行：

```powershell
docker compose down -v
```

也可以使用仓库内脚本重置演示数据并重新启动 API：

```powershell
.\scripts\reset-demo-data.ps1 -StartApi
```

## 本机启动

默认开发启动不依赖 Docker。`npm run dev:all` 会启动本地 Go 后端和 Vite 前端，后端使用内存存储并禁用 Redis，重启后数据会重置：

```powershell
npm run dev:all
```

启动后访问：

- 前端：`http://localhost:5173`
- 后端健康检查：`http://localhost:8080/healthz`
- AI 状态：`http://localhost:8080/api/v1/system/ai`

如果只想单独运行后端：

```powershell
cd backend
go run ./cmd/server
```

或使用同等的仓库脚本：

```powershell
npm run dev:backend
```

前端：

```powershell
cd frontend
npm install
npm run dev
```

默认前端请求 `http://localhost:8080/api/v1`。如需调整，复制 `frontend/.env.example` 为 `frontend/.env` 并修改 `VITE_API_URL`。

如果需要 PostgreSQL/Redis 持久化演示环境，再显式使用 Docker 后端：

```powershell
npm run dev:all -- --docker
```

端到端测试使用 Playwright，默认复用或启动 `http://localhost:5173` 前端，并依赖本地 `http://localhost:8080` API 已启动：

```powershell
cd frontend
npm run e2e
```

首次运行如缺少浏览器驱动，先执行：

```powershell
cd frontend
npx playwright install chromium
```

## MVP 覆盖范围

- 用户体系：注册、登录、刷新 Token、当前用户、个人档案设置。
- 情景题：种子题、真实 LLM 生成题、列表筛选、详情脱敏、创建会话。
- 排查工坊：Reveal Strategy 分层线索、深层线索前置条件、防猜拦截、提示升级、答案评估、复盘。
- 面试舱：按域创建面试、五维评分、后端规则追问、最终报告与雷达图。
- 学习闭环：仪表盘展示能力画像、短板解释、今日推荐、三天复习计划和每日打卡。
- 案例工坊：发布真实案例并生成 AI 结构化预览，讲师可编辑结构化内容并初审，管理员终审发布为正式情景题，审核历史全程保留。
- 数据支撑：PostgreSQL 持久化业务数据，Redis 支撑基础限流。
- AI 接入：默认优先使用 DeepSeek 真实模型；也支持 OpenAI-compatible 第三方中转站，只有缺少模型 Key 或真实调用失败时才走本地兜底。
- 异步与流式：生成题目支持异步任务进度查询；排查消息支持 `text/event-stream` 流式展示。
- Prompt 编排：真实 Provider 的题目生成、UGC 结构化、排查回复和面试评语使用 `backend/internal/ai/prompts/*.tmpl` 模板管理。
- 上下文压缩：排查会话超过 10 轮后，后端会保留摘要和最近 5 轮原始对话再发往 LLM，控制上下文长度。
- 系统状态：管理员可打开系统状态页，检查 API、DB、Redis、AI、种子数据、演示账号和脚本入口。
- 导出能力：排查复盘页和面试报告页支持浏览器打印，可在打印窗口中保存为 PDF。

## SRS 功能代号速查

- `UR`：用户与权限模块，覆盖登录、个人资料、角色权限。
- `SC`：情景题生成与管理模块，覆盖题目生成、脱敏、分页筛选、Fork 派生自定义。
- `DG`：渐进式排查会话模块，覆盖多轮排查、线索释放、提示、提交、评分、复盘。
- `IV`：技术面试模拟与评估模块，覆盖面试会话、追问、评分、报告、语音答案。
- `CM`：UGC 社区与案例工坊模块，覆盖案例提交、结构化预览、讲师初审、管理员终审。
- `PF`：个人学习档案模块，覆盖能力画像、薄弱点、错题复习日历、推荐题。
- `AI`：AI 交互与编排模块，覆盖 Prompt、模型配置、结构化输出校验、安全过滤、上下文管理。
- `SR`：安全需求，覆盖答案泄露防护、敏感信息检测、统一鉴权、限流与审计。

## 比赛演示流程

推荐按以下顺序演示：

1. 使用 `admin/admin123` 登录，打开系统状态页，确认 API、DB、Redis、AI、种子数据和脚本入口正常。
2. 使用 `demo/demo123` 登录，在仪表盘查看今日推荐、短板解释、三天复习计划，并点击今日打卡。
3. 进入排查工坊，生成或选择情景题，开始排查并提交最终答案。
   - 点击“生成题目”后，顶部蓝色状态条应显示“模型：deepseek-v4-flash”；完成后应显示“来源：DeepSeek deepseek-v4-flash”。若 DeepSeek 不可用，再观察是否切到 fallback。
4. 在排查复盘页点击“打印/导出 PDF”，展示标准答案、标准步骤、关键证据和对话记录。
5. 进入面试舱，创建一次面试，提交回答并完成追问，打开面试报告页查看五维雷达图和综合评语。
6. 进入案例工坊，用学员账号发布 UGC 案例，状态进入“待讲师初审”。
7. 使用 `instructor/instructor123` 登录案例工坊，对 UGC 结构化预览做必要编辑并初审通过。
8. 使用 `admin/admin123` 登录案例工坊，对初审通过的 UGC 终审发布为正式情景题。
9. 回到学员账号，在排查工坊查看新发布题目，确认学员看到的是脱敏版。

阶段 18-22 增强验收建议：

1. `IV-02（面试语音答案）`：在面试会话页上传音频，确认后端真实保存音频文件并生成转写草稿，可回填到答案框并提交；面试报告页能看到语音提交记录、资产摘要和可回放语音证据。
2. `AI-01（管理员 Prompt/模型配置）` 与 `AI-02（显式结构化 Schema）`：管理员进入系统状态页，编辑 Prompt、保存模型参数，确认结构化校验器状态可见。
3. `AI-03/SR-02（UGC 敏感信息检测）`：学员发布带 IP、密码或密钥特征的案例，案例工坊应显示风险提示和脱敏建议，审核端也能看到检测结果。
4. `SR-03（审计与限流可视化）`：管理员在系统状态页查看最近审计事件、限流状态和 AI 错误摘要，确认不展示 token、密码或完整密钥。
5. `SC-06（Fork 作者自定义编辑）`：在排查工坊点击“派生题目”，确认进入案例工坊草稿，作者可编辑标题、描述、标签和结构化内容后提交初审。
6. `PF-04（真实错题复习日历）` 与 `PF-05（AI 动态推荐题）`：完成低分排查或面试后回到仪表盘，确认复习计划显示来源和推荐理由；模型不可用时显示规则回退推荐。

赛前可运行一键验收脚本检查主流程：

```powershell
.\scripts\demo-acceptance.ps1
```

如果想减少真实模型调用次数，可以跳过题目生成，仍验证登录、排查、面试、UGC 审核等主流程：

```powershell
.\scripts\demo-acceptance.ps1 -SkipScenarioGenerate
```

赛前回归建议顺序：

```powershell
git diff --check
cd backend
go test ./...
cd ..\frontend
npm run lint
npm run build
npm run e2e
cd ..
.\scripts\demo-acceptance.ps1 -SkipScenarioGenerate
```

## API 约定

- Base URL：`/api/v1`
- 认证：`Authorization: Bearer <JWT>`
- 响应：`{ "code": 200, "message": "success", "data": ... }`
- 限流：启用 Redis 时，未登录接口按 IP 每分钟 60 次，登录接口按用户每分钟 120 次。

UGC 审核接口：

- `GET /community/posts?status=pending_review`：讲师/管理员查看审核队列；学员默认只看自己的帖子。
- `POST /community/posts/{id}/instructor-review`：讲师初审，`decision=approve|reject`，可携带 `structured_content` 保存讲师编辑版。
- `POST /community/posts/{id}/final-review`：管理员终审，`decision=publish|reject`；发布时优先使用讲师编辑版。
- `GET /admin/users`、`PUT /admin/users/{id}/role`：管理员用户角色管理。

学习闭环接口：

- `GET /users/me/dashboard`：返回统计、能力雷达、短板、推荐题、学习计划和复习日历。
- `GET /users/me/learning-plan`：返回领域洞察、推荐项和三天复习计划。
- `GET /users/me/review-calendar`：返回今日打卡状态、连续天数、复习计划和下一步行动。
- `POST /users/me/checkin`：记录今日打卡，重复调用保持幂等。

系统状态接口：

- `GET /system/ai`：返回当前 AI Provider、模型和 fallback 状态，不返回 Key。
- `GET /system/status`：管理员查看 API、DB、Redis、AI、种子数据、演示账号和脚本入口。

AI 任务与流式接口：

- `POST /scenarios/generate/jobs`：创建异步情景题生成任务，立即返回 `job`。
- `GET /ai/jobs/{id}`：查询任务状态；完成后返回脱敏后的 `question`。
- `GET /ai/jobs/{id}/events`：以 SSE 返回任务进度事件。
- `POST /scenarios/sessions/{sid}/messages`：请求头 `Accept: text/event-stream` 时返回 `delta` 与 `finish` 事件；普通请求仍返回 JSON。

## 验证步骤

1. 启动依赖服务和 API，确认 `http://localhost:8080/healthz` 返回健康状态。
2. 启动前端，打开 `http://localhost:5173`，分别使用学员、讲师、管理员演示账号登录。
3. 按“比赛演示流程”完成系统状态、学习仪表盘、排查工坊、面试舱、案例工坊双审和脱敏题查看。
4. 运行后端、前端和 E2E 自动化命令。
   - 后端可额外执行 `go test ./internal/ai ./internal/httpapi ./internal/store`，确认情景题生成模型约束、任务状态模型回填与存储迁移均通过。
5. 最后运行演示验收脚本，确认主流程仍能连续完成。
6. 面试语音链路专项验证：
   - 以 `demo/demo123` 登录，进入面试舱创建一次面试。
   - 在回答区上传 `mp3`、`wav` 或 `webm` 音频，确认页面提示“上传语音资源”后生成转写草稿。
   - 点击“确认转写文本”，提交语音回答，跳转到面试报告页。
   - 在报告页确认出现“资产链路”摘要，包含资源名、体积、SHA256 摘要，并可直接播放语音证据。
   - 另开一个终端执行 `Get-ChildItem backend\\data\\assets -Recurse` 或查看 `ASSET_STORAGE_DIR` 指向目录，确认存在对应音频文件。
   - 如需接口级验证，使用已登录 token 请求 `GET /api/v1/assets/{asset_id}` 与 `GET /api/v1/assets/{asset_id}?content=1`，确认元数据和原始音频内容均可返回。

通过标准：

- 三类账号均可登录并访问对应页面。
- 核心流程不白屏，不显示原始 JSON，不泄露管理员权限错误。
- 自动化命令和演示验收脚本通过。

## 验证命令

演示验收：

```powershell
.\scripts\demo-acceptance.ps1
```

前端：

```powershell
cd frontend
npm run lint
npm run build
npm run e2e
```

后端：

```powershell
cd backend
go test ./...
```

当前环境如果没有安装 Go，可以用 Docker 的 Go 镜像执行测试；需要保证 Docker 能访问镜像仓库：

```powershell
docker run --rm -v "${PWD}:/src" -w /src golang:1.22-alpine go test ./...
```

PostgreSQL 集成测试默认跳过；如果需要运行，设置：

```powershell
$env:POSTGRES_TEST_URL="postgres://teaching:teaching@localhost:5432/teaching_mvp?sslmode=disable"
cd backend
go test ./internal/store
```
