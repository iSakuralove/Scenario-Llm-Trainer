# 面试题库在线编辑与版本审计

## 目标

让管理员可以在现有面试题库治理后台中查看单条面试知识原子详情、在线编辑已发布题目，并查看版本历史，补齐题库真实运营中的“发布后修正”和“审计追踪”闭环。

## 用户价值

- 管理员发现已发布题目内容、分类、标签或追问路径有问题时，可以直接修正，不必重新导入整批数据。
- 每次编辑都有版本历史、编辑备注和操作者记录，便于追溯。
- 编辑后后续新面试立即使用新内容，历史会话继续依赖已保存快照，不被污染。

## 已确认事实

- 当前已经有题库导入、发布、列表、批次、索引重建和运行时动态追问能力。
- Store 已有 `SaveInterviewKnowledgeAtomVersioned`、`GetInterviewKnowledgeAtom`、`ListInterviewKnowledgeAtomVersions`。
- 领域层已有版本类型 `manual_edit`，适合作为在线编辑版本事件。
- 前端管理页已有题库列表、筛选、导入校验/发布和索引重建入口。
- 本任务不处理归档/恢复，因为当前没有 `archive` 版本类型，强行纳入会扩大模型决策。

## 需求

1. 后端新增 admin-only 单题详情接口：`GET /api/v1/admin/interview-bank/atoms/{id}`。
2. 后端新增 admin-only 版本历史接口：`GET /api/v1/admin/interview-bank/atoms/{id}/versions`，默认按 `created_at DESC` 返回。
3. 后端新增 admin-only 在线编辑接口：`PATCH /api/v1/admin/interview-bank/atoms/{id}`。
4. 在线编辑请求必须携带 `base_version` 和非空 `change_note`。
5. 当 `base_version` 缺失或不等于当前 `current_version` 时，拒绝保存；版本冲突提示为 `版本已更新，请刷新后重试`。
6. 在线编辑不允许修改稳定 `id`，也不允许通过普通编辑修改 `status`。
7. 在线编辑支持修改 `title`、`subject`、`domain`、`difficulty`、`category`、`question_role`、`source_ref`、`tags`、`principles`、`pitfalls`、`follow_up_paths`。
8. 在线编辑必须复用导入链路同等硬校验：必填字段、枚举、`source_ref` 非空、`principles/pitfalls/follow_up_paths` 数量下限。
9. 保存成功必须生成 `manual_edit` 版本，并推进 `current_version`。
10. 即使内容无变化，也生成版本并标记 `no_content_change=true`。
11. 保存成功后将 `vector_status` 置为 `pending`，等待管理员手动重建索引。
12. 前端题库列表增加查看/编辑入口。
13. 前端单题详情展示结构化内容、当前版本、索引状态、更新时间和版本历史。
14. 前端编辑保存前弹出二次确认：“保存后将立即影响后续新面试，历史会话不受影响”。

## 不做

- 归档/恢复归档。
- 回滚到历史版本。
- 批量编辑。
- 自动异步索引任务队列。
- 复杂审批流或多人复核。

## 验收标准

- 管理员可以打开题库列表中的单题详情，看到完整结构化内容。
- 管理员可以查看该题版本历史，历史按最新优先排序。
- 管理员编辑已发布题目并填写备注后，保存成功，`current_version` 增加，版本类型为 `manual_edit`。
- 管理员基于旧版本保存时，接口拒绝并返回轻量冲突提示。
- 缺少备注、非法枚举、缺少核心内容或 `source_ref` 为空时，接口拒绝保存。
- 无内容变化保存仍生成版本，并在版本历史中可见 `no_content_change`。
- 保存后该题索引状态变为 `pending`。
- 非管理员不能访问新增接口。

