# 面试题库在线编辑与版本审计

## 目标

补齐面试题库治理后台的发布后修正能力，让管理员可以查看单题详情、在线编辑题库资源，并查看版本历史。

## 修改范围

- 后端新增题库单题详情、版本历史和在线编辑管理接口。
- 前端题库治理页增加“查看/编辑”操作、结构化编辑表单和版本历史面板。
- 架构文档补充题库在线编辑接口边界。

## 核心实现

- `PATCH /api/v1/admin/interview-bank/atoms/{id}` 要求 `base_version` 与 `change_note`，基于当前版本做并发校验。
- 在线编辑复用导入链路的题库字段硬校验，不允许通过普通编辑修改稳定 `id` 或题目状态。
- 保存成功调用 `SaveInterviewKnowledgeAtomVersioned` 写入 `manual_edit` 版本，并将当前题目的 `vector_status` 置为 `pending`。
- 无内容变化的保存仍会生成版本，版本记录中标记 `no_content_change=true`。
- 前端数组字段使用多行文本编辑，标签支持逗号或换行分隔并在提交前去空、去重。

## 影响范围

- 管理员可以直接修正已发布题库内容，后续新面试会读取修正后的当前版本。
- 历史面试仍依赖会话快照，不会被在线编辑反向污染。
- 编辑后需要管理员手动重建索引，追问检索增强才会使用新向量内容。

## 验证方式

- `go test ./internal/httpapi -run "TestAdminInterviewBank"`
- `go test ./internal/httpapi ./internal/store`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- 使用本机 Chrome 打开管理端，验证题库列表、查看详情、编辑保存和版本历史展示。

## 已知限制

- 不包含归档、恢复归档或回滚到历史版本。
- 不包含批量在线编辑。
- 不包含自动异步索引任务队列；编辑后仍需管理员手动重建索引。
