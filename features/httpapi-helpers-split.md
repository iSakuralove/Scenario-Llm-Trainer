# httpapi 通用 helpers 拆分

## 目标

把 `backend/internal/httpapi/server.go` 末尾的无状态通用辅助函数拆到独立文件，减少巨石文件尾部杂项堆积，让后续 handler 拆分更容易审查。

## 修改范围

- `backend/internal/httpapi/server.go`
- `backend/internal/httpapi/helpers.go`
- `review/PROGRESS.md`

## 核心实现

- 新增 `helpers.go`，承载 `firstNonEmpty`、`scoreIf`、`firstSentence`、`clamp`、`min`、`max`。
- 保留 `decode`、`writeOK`、`writeError`、`setCORS` 等 HTTP 响应相关函数在 `server.go`，避免把协议层辅助和普通计算工具混在一起。
- 保持同 package `httpapi` 内拆分，调用方无需修改。

## 影响范围

- 学习计划、面试评分、场景生成归一化等内部调用这些 helper 的流程。
- 对外 API 行为不变。

## 验证方式

已运行：

```powershell
cd backend
go test ./internal/httpapi
go test ./...
```

结果：全部通过。

## 已知限制

- `helpers.go` 仅做低风险工具函数拆分；HTTP handler 仍主要集中在 `server.go`，后续需要单独按领域拆分。
