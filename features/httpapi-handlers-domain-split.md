# httpapi 领域 handler 拆分

## 目标

完成 `backend/internal/httpapi/server.go` 的领域级拆分，把剩余的主要 handler 从入口文件迁出，保留统一路由和横切能力，结束 `server.go` 巨石收尾阶段。

## 修改范围

- `backend/internal/httpapi/server.go`
- `backend/internal/httpapi/handlers_auth.go`
- `backend/internal/httpapi/handlers_ai.go`
- `backend/internal/httpapi/handlers_assets.go`
- `backend/internal/httpapi/handlers_scenarios.go`
- `backend/internal/httpapi/handlers_interviews.go`
- `backend/internal/httpapi/handlers_community.go`
- `backend/internal/httpapi/handlers_admin.go`
- `docs/architecture.md`
- `review/PROGRESS.md`

## 核心实现

- 将 `/auth/*`、`/users/me/*` 迁移到 `handlers_auth.go`，并带上 `history()` 等只被该领域使用的辅助。
- 将 `/ai/*`、`/assets/*` 分别迁移到 `handlers_ai.go`、`handlers_assets.go`。
- 将 `/scenarios/*`、场景会话和问答处理迁移到 `handlers_scenarios.go`。
- 将 `/interviews/*`、语音提交和面试报告相关逻辑迁移到 `handlers_interviews.go`。
- 将 `/community/*`、UGC 草稿、讲师初审、管理员终审迁移到 `handlers_community.go`。
- 将 `/admin/*`、`/system/status` 相关的系统状态组装和 prompt/AI config 管理迁移到 `handlers_admin.go`。
- `server.go` 仅保留 `Server`、根路由 `Handler()`、鉴权/限流/审计、通用 HTTP 工具和少量跨领域基础函数。

## 影响范围

- 所有 `httpapi` 路由入口的源码位置发生变化，但对外路径、请求结构和响应结构不变。
- `server.go` 从 2790 行收敛到 555 行，后续定位路由处理逻辑的成本显著下降。

## 验证方式

已运行：

```powershell
cd backend
go test ./internal/httpapi
go test ./...
```

结果：全部通过。

## 已知限制

- `server.go` 仍保留横切能力与少量跨领域基础函数，没有继续拆审计、限流和敏感检测。
- `password-reset` 当前仍是 MVP 兼容接口，这次拆分不改变其安全边界。
