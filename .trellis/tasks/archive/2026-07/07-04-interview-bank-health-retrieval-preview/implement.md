# 实施计划

## 步骤

1. 阅读题库管理、运行时追问、向量索引相关代码与测试。
2. 明确现有开放组合计算和题库摘要结构，避免重复实现冲突口径。
3. 后端新增健康诊断领域结构和 admin-only 接口。
4. 后端新增检索预览请求、响应和 handler，复用现有题库检索能力。
5. 为 MemoryStore / PostgresStore 补齐必要的最小查询或聚合辅助函数。
6. 增加后端测试：权限、聚合状态、告警原因、预览命中、无索引 fallback。
7. 前端 API 类型和请求函数补齐。
8. 前端题库管理页增加健康诊断视图和检索预览视图。
9. 前端处理加载、空状态、错误状态、文本溢出和窄屏布局。
10. 更新 `docs/architecture.md` 和 `features/` 功能说明。

## 验证命令

- `cd backend; go test ./internal/httpapi ./internal/store`
- 如检索逻辑改动较多：`cd backend; go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 注意事项

- 不提交 `docs/ai-interview-integration-prd.md` 中既有无关改动，除非后续明确把它纳入本任务。
- 检索预览不能写正式面试日志，也不能修改题目或版本状态。
- 管理端可展示更多题库治理信息，但普通用户报告仍不能暴露题库版本号或管理端标题细节。
