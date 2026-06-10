# HTTP 安全边界收口

## 目标

收紧 HTTP 服务的默认边界：在不破坏当前 SSE、Docker 后端和前端默认开发链路的前提下，补足读取超时、CORS 约束以及匿名系统状态接口的公开范围。

## 修改范围

- `backend/cmd/server/main.go`
- `backend/cmd/server/main_test.go`
- `backend/internal/httpapi/server.go`
- `backend/internal/httpapi/http_security_test.go`
- `docs/architecture.md`

## 核心实现

- HTTP Server 新增统一 `ReadTimeout=60s`，并保留现有 `ReadHeaderTimeout=10s`、`IdleTimeout=120s`。
- 继续不设置 `WriteTimeout`，避免 SSE 被写超时中断。
- CORS 默认不再对所有浏览器源开放。
  - 默认仅允许本地前端开发源：
    - `http://localhost:5173`
    - `http://127.0.0.1:5173`
    - `http://0.0.0.0:5173`
    - `http://localhost:4173`
    - `http://127.0.0.1:4173`
    - `http://0.0.0.0:4173`
  - 需要其他浏览器源访问时，通过 `CORS_ALLOWED_ORIGINS` 显式配置。
  - 浏览器携带未知 `Origin` 时直接返回 `403 origin not allowed`。
- `/api/v1/system/ai` 继续保留匿名访问，但返回值改为公开裁剪版：
  - 保留 `provider`、`model`、`fallback`、`configured_provider`、`configured_model`、`router_version`、`health` 等非敏感字段。
  - 不再暴露 `base_url`、`init_error`、`last_error`、`last_trace_id`、`telemetry`、`provider_pool` 等敏感或内部运维字段。
- 管理员完整系统状态仍通过受保护的 `/system/status` 与 `/admin/*` 获取。

## 影响范围

- 当前 `http://localhost:5173` 前端开发环境不会回归。
- 非白名单浏览器源将不能跨域读取 API。
- 登录页和侧栏仍能显示“当前 AI 模式”，但不会再泄露后端 provider base URL 和内部错误细节。
- 慢请求和慢上传不再能无限期占住连接。

## 验证方式

- `go test ./cmd/server`
- `go test ./internal/httpapi`
- `go test ./...`
- `pnpm test`

## 已知限制

- 这轮没有引入动态多环境来源发现机制，生产部署如果不是本地前端源，必须显式设置 `CORS_ALLOWED_ORIGINS`。
- 这轮没有处理前端大包告警；构建体积优化仍属于后续阶段。
