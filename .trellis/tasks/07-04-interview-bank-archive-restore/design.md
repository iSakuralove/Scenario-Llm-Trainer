# 技术设计

## 后端边界

- 在 `backend/internal/httpapi/handlers_interview_bank.go` 增加两个 admin-only handler：
  - `POST /api/v1/admin/interview-bank/atoms/{id}/archive`
  - `POST /api/v1/admin/interview-bank/atoms/{id}/restore`
- 路由注册继续放在现有题库管理路由分支内，不新增独立服务。
- 复用现有 `GetInterviewKnowledgeAtom`、`SaveInterviewKnowledgeAtomVersioned` 和题库字段硬校验。
- 如当前版本类型缺少 `archive` / `restore_archived`，在领域常量中补齐，并保持 MemoryStore 与 PostgresStore 同一版本写入口径。

## 数据与版本合同

归档：

1. 读取当前 atom。
2. 校验当前状态不是 `archived`，请求 `reason` 非空。
3. 将状态设置为 `archived`。
4. 调用 `SaveInterviewKnowledgeAtomVersioned` 写入 `archive` 版本，操作者为 admin id，备注使用归档原因。
5. 清理向量文档或确保该 atom 不再可被检索命中。

恢复：

1. 读取当前 atom。
2. 校验当前状态为 `archived`。
3. 将状态设置为 `published`。
4. 复用题库硬校验，确保恢复后的内容满足发布要求。
5. 设置 `vector_status=pending`，`last_indexed_at` 按现有保存策略处理。
6. 调用 `SaveInterviewKnowledgeAtomVersioned` 写入 `restore_archived` 版本。

## 前端边界

- 在 `frontend/src/api/client.ts` 新增归档/恢复 API。
- 在 `frontend/src/types/index.ts` 新增请求类型。
- 在 `InterviewBankAdminPage.tsx` 的单题详情区增加状态动作。
- 归档使用原因输入，恢复只需要二次确认。
- 动作成功后刷新单题详情、版本历史和列表统计。

## 兼容性

- 不改变在线编辑接口。
- 不改变历史版本快照字段。
- 不改变运行时检索主链路；状态和向量文档清理保证归档题自然退出。
- CORS 已支持 `POST`，本任务无需新增 HTTP method。

## 风险

- 如果只改状态但不清理向量文档，运行时检索若未过滤状态可能继续命中旧内容；实现时必须确认检索查询或归档动作至少一侧阻断。
- 恢复时如果绕过硬校验，会把不完整题目重新发布；必须复用现有校验。

