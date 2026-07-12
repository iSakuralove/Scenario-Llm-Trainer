# 实施计划

## 顺序

1. RED：后端测试，`launchpad.open_tracks[*].tags` 存在且类型稳定。
2. GREEN：后端轨道聚合补 tags。
3. RED：前端构建或页面渲染不满足筛选与 badge 需求。
4. GREEN：增加本地筛选状态、筛选器、availability badge 和空状态。
5. 验证 fallback 模式仍能渲染。
6. 更新 feature / architecture / launchpad 合同文档。

## 验证命令

- `cd backend; go test ./internal/httpapi`
- `cd backend; go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 不做

- 不引入新的后端筛选接口。
- 不渲染未开放组合。
