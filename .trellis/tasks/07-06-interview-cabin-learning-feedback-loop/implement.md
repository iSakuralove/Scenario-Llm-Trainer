# 实施计划

## 顺序

1. RED：后端 dashboard 测试，已完成面试会话带 `retraining_suggestions` 时返回 `kind=interview` 推荐。
2. GREEN：增强 `learningPlan()` 生成面试专项建议。
3. RED：review calendar 中出现 `source_kind=interview_retraining` 条目。
4. GREEN：补面试复训条目映射。
5. 前端仪表盘推荐区增加 `kind=interview` 的显式标识。
6. 跑测试、lint、build，并更新文档。

## 验证命令

- `cd backend; go test ./internal/httpapi`
- `cd backend; go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 不做

- 不新增独立 Mentor 页。
- 不改面试报告接口。
