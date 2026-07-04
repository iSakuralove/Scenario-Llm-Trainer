# 实施计划：面试题库在线编辑与版本审计

## 实施顺序

1. 读取 backend/frontend 相关 Trellis 规范。
2. 后端扩展 `handlers_interview_bank.go`：
   - 增加单题详情、版本历史、编辑请求结构。
   - 抽出共享题库 atom 校验函数。
   - 实现 `PATCH` 保存和版本冲突处理。
3. 补后端测试：
   - admin 权限、详情、版本历史。
   - 编辑成功、版本推进、`manual_edit`、`pending` 索引状态。
   - 缺少备注、缺少/冲突 `base_version`、非法字段校验。
   - 无内容变化保存。
4. 前端扩展类型与 API client。
5. 前端扩展题库管理页：
   - 行操作入口。
   - 详情/编辑表单。
   - 版本历史面板。
   - 保存确认、成功刷新和错误展示。
6. 更新架构文档与 features 文档。
7. 运行验证命令并修复问题。

## 验证命令

- `cd backend; go test ./internal/httpapi ./internal/store`
- `cd backend; go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 风险点

- 在线编辑校验不能复制出一套与导入不一致的规则。
- `vector_status` 更新为 `pending` 时不能绕过版本写入，否则版本历史与当前主表会分叉。
- 前端数组字段必须避免空行、重复标签导致误报校验失败。
- 当前工作树已有无关变更，提交时必须只纳入本任务文件。

