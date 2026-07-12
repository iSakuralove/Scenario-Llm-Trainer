# 实施计划

1. RED：后端测试 `source_kind=fresh_content`。
2. GREEN：补 launchpad 推荐逻辑。
3. 跑 `go test ./internal/httpapi` 与 `go test ./...`。
4. 跑 `npm --prefix frontend run lint/build`，确认前端继续消费。
