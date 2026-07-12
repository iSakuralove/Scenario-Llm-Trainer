# 实施计划

## 顺序

1. RED：后端测试，`GET /interviews/launchpad` 返回 `recommended_tracks` 和 `recent_sessions`。
2. GREEN：扩展 `interviewLaunchpad()` 聚合逻辑。
3. RED：前端类型/页面因缺少新字段而无法渲染预期区块。
4. GREEN：补 `api.client` 类型和 `InterviewsPage` 状态区、推荐区、覆盖区。
5. 验证 fallback 模式下页面仍能启动面试。
6. 更新相关文档。

## 验证命令

- `cd backend; go test ./internal/httpapi`
- `cd backend; go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 不做

- 不改学习闭环接口。
- 不改报告接口。
- 不做 Mentor 页面。
