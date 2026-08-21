# 基于AI大模型的情景式教学系统 MVP

这是根据目录内需求文档与功能技术规格书落地的比赛演示版 MVP，覆盖登录、情景题、渐进式排查会话、答案评估复盘、技术面试、面试报告、个人档案和 UGC 案例预览。

## 目录结构

```text
backend/   Go API 服务，支持 PostgreSQL 持久化、Redis 限流、顺序路由 LLM 与种子数据
agent/     Python 排查工坊 Runtime（FastAPI + PydanticAI）
frontend/  React + TypeScript + Zustand 前端应用
config/    llm_routes.yaml —— LLM 站点顺序路由配置（见 docs/llm-routing.md）
scripts/   比赛演示验收与演示数据重置脚本
docs/      设计与配置文档（Runtime V2、LLM 路由、历史更新记录）
docker-compose.yml  API、Agent、PostgreSQL、Redis 本地编排
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

## 快速开始

仓库地址：https://github.com/iSakuralove/Scenario-Llm-Trainer

本节说明如何从零把本项目跑起来，以及部署时需要注意的事项。没有真实模型 Key 也能启动；配置 DeepSeek / Embedding 后可验证完整 AI 能力。

### 环境要求

- Git
- Node.js 20+
- Go 1.22+（本机直接运行后端时需要）
- Docker Desktop（推荐，用于 PostgreSQL / Redis / API 的持久化演示）
- 可选：DeepSeek、Embedding、SMTP 凭据

### 克隆项目

```powershell
git clone git@github.com:iSakuralove/Scenario-Llm-Trainer.git
cd Scenario-Llm-Trainer
git checkout main
git pull
```

### 配置环境变量

不要把真实 Key 提交进 Git。在仓库根目录创建本地 `.env`（已被忽略）：

```powershell
# 至少配置 JWT_SECRET；AI 相关变量可按需补充
@'
JWT_SECRET=please-change-me-to-a-long-random-string
DEEPSEEK_KEY=
EMBEDDING_BASE_URL=https://router.tumuer.me/
EMBEDDING_API_KEY=
EMBEDDING_MODEL=text-embedding-3-small
SMTP_HOST=smtp.qq.com
SMTP_PORT=587
SMTP_USERNAME=
SMTP_PASSWORD=
SMTP_FROM=
APP_PUBLIC_URL=http://localhost:5173
'@ | Out-File -FilePath .env -Encoding utf8
```

说明：

1. Docker 启动 API 时必须提供 `JWT_SECRET`
2. 配置 `DEEPSEEK_KEY` 后，面试反馈与题目生成会走真实模型
3. Embedding 相关变量用于语义检索；缺失时会自动回退本地规则
4. 修改 `.env` 后需要重建 API 容器才会生效：

```powershell
docker compose up -d --force-recreate --no-build api
```

更完整的变量清单见下文「环境变量」。

### 推荐启动：Docker 后端 + 本机前端

适合演示和联调，数据持久化在 PostgreSQL。

```powershell
# 终端 1
docker compose up --build api

# 终端 2
cd frontend
npm install
npm run dev
```

访问：

- 前端：http://localhost:5173
- API 健康检查：http://localhost:8080/healthz
- 管理员系统状态：http://localhost:8080/api/v1/system/status

演示账号：

- 学员：`demo` / `demo123`
- 讲师：`instructor` / `instructor123`
- 管理员：`admin` / `admin123`

登录请求字段为 `identifier` + `password`。

### 轻量启动：本机全栈

不依赖 Docker，后端默认内存存储；重启后业务数据会清空。

```powershell
npm run dev:all
```

### 面试舱使用说明

1. 登录后打开 `/interviews`
2. 在“自由选题 / 岗位专项 / 简历深挖”之间切换；每种模式会保留自己的选择和本场设置
3. 自由选题按题目、稳定题号、领域、分类或标签搜索，并可按领域、难度和题型筛选；点击题卡只会选中题目
4. 岗位专项支持按岗位名称或技术范围搜索，再从该岗位的真实题目中选择一道开始
5. 简历深挖可选择一份或多份已通过质量检测的简历；没有合格简历时开始按钮保持禁用
6. 在页头设置面试方式、最多作答轮数和追问重点；当前模式满足条件后“开始面试”才会激活
7. 会话页按时间线作答；认真的技术回答不会被固定文案“请认真回答面试问题…”误拦截
8. 多轮回答会按“题目 → 回答 → 追问”交错展示，右侧导航可快速跳转到对应轮次
9. 历史面试支持单条删除和清空全部，适合演示前重置本地记录
10. 配置有效 `DEEPSEEK_KEY` 后，反馈应来自 DeepSeek，而不是 mock 模板

### 部署注意事项

1. **密钥安全**：`.env`、系统环境变量中的 Key 不要提交仓库；`docker compose` 通过环境变量注入。
2. **API 镜像要更新**：后端改动后需要 `docker compose up --build api`。若 Docker Hub 拉基础镜像失败，可在本机交叉编译 linux 二进制后热替换进容器。
3. **AI 回退行为**：DeepSeek 调用失败时会降级 mock；可在系统状态页查看 provider 与 recent attempts。
4. **Embedding 默认地址**：`https://router.tumuer.me/`；未配置 Key 时不影响服务启动。
5. **SMTP 可选**：密码重置邮件依赖 SMTP 相关变量与 `APP_PUBLIC_URL`。
6. **中文内容编码**：接口与浏览器路径使用 UTF-8。若历史记录出现 `??`，通常是历史坏数据，删除该会话后重新作答即可。
7. **前端 API 地址**：默认请求 `http://localhost:8080/api/v1`；如需修改，复制 `frontend/.env.example` 为 `frontend/.env` 并设置 `VITE_API_URL`。

### 本地验证

```powershell
# 后端定向测试
cd backend
go test ./internal/httpapi -count=1 -run "Launchpad|Irrelevant|EvaluateIrrelevant"

# 前端
cd ..\frontend
npm run lint
npm run build
```

页面验收建议：

1. 使用 `demo/demo123` 登录
2. 打开面试舱，确认自由选题展示真实题目，稳定题号可搜索，未选题时开始按钮禁用
3. 切换岗位专项，确认岗位搜索和题目选择可用；切回自由选题后原选择仍保留
4. 切换简历深挖，确认没有合格简历时不能开始；有多份合格简历时可切换预览并选择本场使用项
5. 选题 → 调整本场设置 → 开始面试，连续认真回答两轮，确认正常评分与追问
6. 使用管理员账号查看系统状态中的 AI provider

## 最近更新（2026-08）

### 排查工坊 Runtime V2

单 Agent + 工具调用 Runtime 重构落地，三层（Python/Go/前端）契约同步迁移：

- **V2 事件协议**（`hiddenworld.v2`）：判别联合 payload、`sequence`/`state_revision`/`schema_version` 职责分离，Go 成为 public sequence 唯一生成者，断线重连与幂等重放不重编号
- **用户动作授权**：观察类工具只能执行学生明确提出或 QuickAction 点击授权的检查，Agent 自主观察被确定性拒绝
- **无参数答案比较**：`compare_answer` 由 Runtime 绑定当前轮答案尝试，模型无法探测标准答案；CanonicalAnswer 独立持久化并有生成/加载强校验
- **前端新交互**：事件驱动思考态、内嵌 Task List、QuickActions 快捷动作、工具结果/线索统一渲染 `markdown_ready`；界面不再出现「排查导师」等身份文字
- **旧会话兼容**：v1 事件经 LegacyEventAdapter 进入同一渲染管线，历史数据只读兼容不回填

详见 [docs/scenario-runtime-v2.md](docs/scenario-runtime-v2.md)。

### LLM 顺序路由（替代 LiteLLM）

全部 LLM 调用改由 `config/llm_routes.yaml` 声明的顺序路由接管，Python 与 Go 共用一份配置：官方 GLM / DeepSeek / MiniMax 预置默认地址与模型，key 引用 `.env` 环境变量不落盘，key 留空自动跳过该站，站点失败自动切下一站。

详见 [docs/llm-routing.md](docs/llm-routing.md)。

> 更早的更新记录见 [docs/changelog-2026-07.md](docs/changelog-2026-07.md)。

## 环境变量

后端支持以下变量：

```powershell
PORT=8080
JWT_SECRET=replace-with-a-long-random-secret
STORE_MODE=postgres
DATABASE_URL=postgres://teaching:teaching@localhost:5432/teaching_mvp?sslmode=disable
REDIS_URL=redis://localhost:6379/0
# LLM 路由已由 config/llm_routes.yaml 接管（见 docs/llm-routing.md）
# 站点 key 经 GLM_API_KEY / DEEPSEEK_KEY / MINIMAX_API_KEY 注入
ZETA_KEY=
JIANYI_API_KEY=
DEEPSEEK_KEY=
EMBEDDING_BASE_URL=https://router.tumuer.me/
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
- LLM 调用统一由 `config/llm_routes.yaml` 顺序路由接管：按声明顺序尝试站点，key 留空自动跳过，站点失败自动切换下一站；`GLM_API_KEY` / `DEEPSEEK_KEY` / `MINIMAX_API_KEY` 等站点 key 在 `.env` 注入。详见 [docs/llm-routing.md](docs/llm-routing.md)。
- DeepSeek 固定默认地址为 `https://api.deepseek.com`，默认模型为 `deepseek-v4-flash`。
- 情景题生成任务 `scenario_generate` 会优先固定使用 `deepseek-v4-flash`；即使管理员把全局 AI 配置改成其他 DeepSeek 模型，生成题仍回到该模型执行，失败后再按 Provider 链路降级。
- `scenario_generate` 是非流式结构化 JSON 调用；验证时系统状态的 recent attempts 应显示 DeepSeek 成功，页面来源应为 `DeepSeek deepseek-v4-flash`，不应因全局流式开关误显示 `Mock LLM 兜底`。
- 第三方中转站：在 `config/llm_routes.yaml` 追加条目（base_url + api_key + model）即可，支持 `extra_headers`。
- 排查会话语义网关会直接调用 `https://router.tumuer.me/v1/embeddings`；Key 读取顺序为 `EMBEDDING_API_KEY`、`jeniya_embedding_key`、`JIANYI_API_KEY`，推荐用 `jeniya_embedding_key` 放置 embedding 专用 key。默认模型为 `text-embedding-3-small`；如果需要切换模型，请确认该模型仍在 embeddings 端点可用。未配置 key 或调用失败时，后端保留本地相似度与关键词规则，不影响 Go 后端单独运行。
- 语音转写优先使用 `STT_API_KEY`，其次 `ZETA_KEY`，最后兼容 `JIANYI_API_KEY`；存在 `ZETA_KEY` 时默认 `STT_BASE_URL=https://api.zetatechs.com`，默认 `STT_MODEL=gpt-4o-mini-transcribe-2025-12-15`。
- Zeta 可选路线包括 `https://api.zetatechs.com`、`https://api.zetatechs.online`、`https://ent.zetatechs.com`、`https://ent.zetatechs.online`，用 `STT_BASE_URL` 切换。
- 不要把真实模型 Key 写入仓库；用系统环境变量或本机 `.env` 注入。

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
