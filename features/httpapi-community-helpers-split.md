# httpapi 社区辅助逻辑拆分

## 目标

继续降低 `backend/internal/httpapi/server.go` 的复杂度，将社区帖子可见性、审核流转辅助、场景转换和内容清洗逻辑拆分到独立文件，便于后续继续拆社区 handler。

## 修改范围

- `backend/internal/httpapi/server.go`
- `backend/internal/httpapi/community_helpers.go`
- `review/PROGRESS.md`

## 核心实现

- 新增 `community_helpers.go`，承载社区帖子列表过滤、历史视图匹配、帖子到场景转换、场景 fork 草稿、审核摘要刷新、帖子视图补全、内容归一化和清洗、权限判断、审核历史项生成、社区历史辅助。
- 保留 `handleCommunity`、`handleInstructorReview`、`handleFinalReview` 等 handler 在 `server.go` 中，避免本次拆分同时改变路由入口。
- 未移动通用安全检测、AI 配置、审计汇总等辅助函数，避免把非社区职责混入社区文件。
- 保持同 package `httpapi` 内拆分，接口行为和响应结构不变。

## 影响范围

- 社区帖子列表与筛选。
- 讲师审核、管理员终审和审核历史。
- UGC 场景发布转换。
- 用户历史中的社区帖子摘要。

本次是机械拆分，不改变业务语义。

## 验证方式

已运行：

```powershell
cd backend
go test ./internal/httpapi
go test ./...
```

结果：全部通过。

## 已知限制

- CodeGraph 显示社区辅助函数缺少专门覆盖测试，当前主要由 `httpapi` 包现有测试和全量后端测试兜底。
- 社区 handler 仍保留在 `server.go`，后续如继续拆分，可单独抽 `handlers_community.go`。
