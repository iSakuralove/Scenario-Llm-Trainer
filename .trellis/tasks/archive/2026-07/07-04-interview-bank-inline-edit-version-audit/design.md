# 技术设计：面试题库在线编辑与版本审计

## 架构边界

- 后端入口继续放在 `backend/internal/httpapi/handlers_interview_bank.go`，挂载在现有 `/admin/interview-bank/*` admin-only 边界下。
- 数据读写优先复用 Store 现有接口，不新增审计表，不改变题库主表/版本表 schema。
- 前端继续在现有 `InterviewBankAdminPage` 内扩展，避免新增独立后台或路由。

## 后端接口

- `GET /api/v1/admin/interview-bank/atoms/{id}`
  - 返回 `{ atom }`。
  - 不存在返回 404。

- `GET /api/v1/admin/interview-bank/atoms/{id}/versions`
  - 返回 `{ list }`。
  - Store 返回值如不是最新优先，HTTP 层或 Store 层保证按 `created_at DESC`。

- `PATCH /api/v1/admin/interview-bank/atoms/{id}`
  - 请求字段：`base_version`、`change_note`、`title`、`subject`、`domain`、`difficulty`、`category`、`question_role`、`source_ref`、`tags`、`principles`、`pitfalls`、`follow_up_paths`。
  - 路径 `id` 是稳定 ID，body 不接收或不信任 `id`。
  - 当前仅允许编辑已存在题目；普通编辑不改变 `status`。
  - 保存前复制当前 atom，替换可编辑字段，`status` 保持当前值，`vector_status` 设置为 `pending`，`last_indexed_at` 清空。
  - 保存时调用 `SaveInterviewKnowledgeAtomVersioned(atom, manual_edit, user.ID, change_note)`。
  - 返回 `{ atom, version }`。

## 校验策略

- 抽出或复用现有导入校验中的共享校验函数，避免导入和在线编辑规则分叉。
- 在线编辑必须校验：
  - `base_version > 0`。
  - `change_note` trim 后非空。
  - 当前 atom 存在。
  - `base_version == current_version`。
  - `difficulty` 为 `L1-L5`。
  - `category`、`question_role`、`status`、`vector_status` 命中现有受控枚举。
  - `principles`、`pitfalls`、`follow_up_paths` 至少 2 条。
  - `source_ref` 非空。

## 前端交互

- 列表每行增加“查看/编辑”按钮。
- 点击后加载单题详情和版本历史，展示在页面内详情面板或弹层中。
- 详情面板包含只读状态信息与可编辑表单。
- 数组字段用多行文本编辑，提交前按行拆分、trim、去空行。
- 标签用逗号或换行分隔，提交前 trim、去空和去重。
- 保存前使用浏览器确认弹窗；取消则不请求接口。
- 保存成功后刷新列表、详情和版本历史。
- 版本冲突或校验错误以现有错误提示区域展示。

## 兼容性

- 不改变现有导入/发布/索引重建接口。
- 不影响用户侧面试会话快照和报告展示。
- 历史版本仍由现有版本表承载。
- 归档/恢复后续单独设计，不在本任务内引入新版本类型。

