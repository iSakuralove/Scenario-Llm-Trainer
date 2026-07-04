# 实施清单

## 后端

1. 查找 `InterviewKnowledgeVersionType` 与版本常量，补齐 `archive` / `restore_archived`。
2. 查找向量文档删除/替换函数，确定归档时的最小清理路径。
3. 在题库 admin handler 增加归档/恢复请求结构和处理函数。
4. 注册路由并补充权限测试。
5. 增加归档成功、缺少原因、重复归档、恢复成功、恢复非归档题、恢复硬校验失败、向量状态/文档处理测试。

## 前端

1. 增加 API 类型和 client 方法。
2. 在详情面板增加归档/恢复操作区。
3. 成功后刷新详情、版本历史、列表和摘要。
4. 增加 loading / error 状态，避免重复点击。

## 文档

1. 更新 `docs/architecture.md` 的题库治理接口说明。
2. 新增 `features/interview-bank-archive-restore.md`。
3. 如实现中发现新的跨层合同或坑点，更新 `.trellis/spec/`。

## 验证

- `cd backend; go test ./internal/httpapi ./internal/store`
- `cd backend; go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- 使用本机 Chrome 验证详情页归档、恢复和版本历史刷新。

