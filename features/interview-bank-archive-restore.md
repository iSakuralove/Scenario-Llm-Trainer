# 面试题库归档与恢复归档

## 目标

补齐管理员对面试题库资源的下架与恢复能力，让错误、过期或重复题目可以退出后续新面试和追问检索，并保留可审计版本历史。

## 修改范围

- 后端新增题库单题归档和恢复管理接口。
- 前端题库详情面板增加归档原因输入、归档按钮和恢复按钮。
- 领域版本类型补充 `archive`。
- 架构文档补充归档/恢复接口边界。

## 核心实现

- `POST /api/v1/admin/interview-bank/atoms/{id}/archive` 要求 `reason`，保存 `archive` 版本，将状态改为 `archived`，并清理题库向量文档。
- `POST /api/v1/admin/interview-bank/atoms/{id}/restore` 只允许恢复 `archived` 题目，恢复前复用题库硬校验，保存 `restore_archived` 版本，并将 `vector_status` 置为 `pending`。
- 运行时追问检索已有 `status=published` 与 `vector_status=indexed` 双重过滤；归档动作额外删除向量文档，避免旧索引残留。
- 前端动作成功后刷新单题详情、版本历史、列表和摘要。

## 影响范围

- 已归档题目不会进入后续新面试和追问增强。
- 恢复后的题目可重新进入开场题筛选，但追问增强需要管理员重建索引后才可使用。
- 历史会话继续依赖既有快照，不会被归档或恢复反向污染。

## 验证方式

- `go test ./internal/httpapi ./internal/store`
- `go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- 使用本机 Chrome 验证详情页归档、恢复和版本历史刷新。

## 已知限制

- 不包含回滚到历史版本。
- 不包含批量归档或批量恢复。
- 不包含自动异步索引任务队列。
- 不包含物理删除题目。

