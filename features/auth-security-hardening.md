# 认证安全收口

## 目标

检查 bcrypt 迁移、`JWT_SECRET` 校验、密码重置边界、token 失效机制与前端会话恢复，确保安全加固不会破坏现有登录流程。

## 修改范围

- `backend/internal/auth/jwt.go`
- `backend/internal/auth/jwt_test.go`
- `backend/internal/domain/types.go`
- `backend/cmd/server/main.go`
- `backend/cmd/server/main_test.go`
- `backend/internal/httpapi/server.go`
- `backend/internal/httpapi/handlers_auth.go`
- `backend/internal/httpapi/auth_security_test.go`
- `backend/internal/httpapi/password_reset_test.go`
- `backend/internal/store/interface.go`
- `backend/internal/store/memory.go`
- `backend/internal/store/postgres.go`
- `backend/internal/store/schema.go`
- `backend/internal/store/schema_test.go`
- `backend/migrations/001_schema.sql`
- `docker-compose.yml`
- `frontend/src/api/client.ts`
- `frontend/src/stores/authStore.ts`
- `frontend/src/features/learning/DashboardPage.tsx`
- `frontend/src/features/profile/ProfilePage.tsx`
- `frontend/e2e/auth-refresh.spec.ts`
- `scripts/backend-local-runtime.mjs`
- `scripts/backend-local.test.mjs`
- `package.json`
- `scripts/dev-all.test.mjs`

## 核心实现

- bcrypt 迁移保持兼容：新密码继续使用 bcrypt，旧 SHA-256 hash 仍可通过 `CheckPassword` 校验。
- 登录成功后自动把 legacy SHA-256 hash 升级为 bcrypt，并记录升级或失败审计，避免旧 hash 长期滞留。
- 注册和改密统一复用密码规则，拒绝空白密码和长度不足的密码。
- `User` 新增内部 `token_version`，JWT claims 带上版本号；改密后会递增版本，使旧 access/refresh token 同时失效。
- `/users/me/password` 改密成功后立即返回新的 `access_token` 和 `refresh_token`，避免当前用户被迫手动重新登录。
- 匿名 `/api/v1/auth/password-reset` 默认禁用，仅在内存模式且显式设置 `ENABLE_ANON_PASSWORD_RESET=true` 时才允许使用；持久化 Postgres 模式下始终禁用。
- `JWT_SECRET` persistent 模式要求显式强 secret，并额外拒绝 `local-dev-secret-please-change` 这类仓库内固定默认值。
- `docker-compose.yml` 不再提供任何仓库内固定 JWT 默认值；未设置 `JWT_SECRET` 时直接启动失败。
- 本地后端脚本不再偷偷注入固定 JWT secret；持久化本地后端必须从 shell 或根目录 `.env` 显式提供 `JWT_SECRET`。
- 前端请求层新增 `401 -> refresh -> retry once`，并在 refresh 失败时统一清理登录态回到登录页。
- 档案保存与打卡在自动 refresh 后会复用最新 token，不会把旧 token 再覆盖回 store。
- 登录限流保持失败后计数，避免成功登录消耗账号级配额导致演示账号被 429 锁住。

## 影响范围

- 旧数据库中 SHA-256 格式的用户密码不会因为 bcrypt 迁移失效。
- 改密后此前签发的 access token 和 refresh token 会立刻失效，受保护请求必须使用新的 token。
- 仍在使用旧 `JWT_SECRET=local-dev-secret` 或 `local-dev-secret-please-change` 的持久化部署会启动失败，需要改成更长的自定义随机值。
- 本地 Docker Compose 现在必须显式设置 `JWT_SECRET`，不再依赖仓库内固定默认值。
- `pnpm dev` / `pnpm dev:backend` 会读取根目录 `.env`；如果同名环境变量已在当前 shell 中设置，则以 shell 环境变量为准。
- 默认开发方式是 Docker 后端常驻时运行 `pnpm run dev` 只启动前端；需要前后端一起本地启动时改用 `pnpm run dev:all`。
- 匿名重置口不再是默认可用能力，现有脚本如果依赖它，需要先显式开启 `ENABLE_ANON_PASSWORD_RESET=true` 且仅限内存模式。

## 验证方式

- `go test ./internal/auth`
- `go test ./cmd/server`
- `go test ./internal/store`
- `go test ./internal/httpapi`
- `go test ./...`
- `pnpm test`
- `pnpm --dir frontend exec playwright test e2e/auth-refresh.spec.ts e2e/login.spec.ts --project chromium`

## 已知限制

- `HashPassword` 仍保持无错误返回签名，极端情况下 bcrypt 失败会回退到旧 hash；后续若要彻底收口，应把注册和重置链路改为显式返回 hash 错误。
- 匿名 `password-reset` 仍保留为受控兼容能力，而不是正式的生产密码找回方案；真实生产环境仍应替换为邮件、短信或一次性 token 重置流程。
- 前端仍存在较大的构建 chunk warning；这轮只处理认证与会话链路，没有进入路由级懒加载优化。
