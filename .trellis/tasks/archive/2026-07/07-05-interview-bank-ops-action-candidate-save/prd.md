# 面试题库运营动作候选选择保存 PRD

## 目标

在已有运营动作手工创建、动作列表、健康/索引/真实检索候选预览基础上，补齐第四个 TDD 切片：管理员可以把候选预览中选中的候选保存为正式 open 运营动作。

本切片只做“候选选择保存 + active dedupe + 前端最小闭环”，不做动作详情、状态流转、关闭备注、重开或自动修题。

## 用户价值

- 管理员不再需要把候选内容手工复制到“创建动作”表单。
- 候选预览可以保持低风险，只在管理员显式选择后进入 open 队列。
- 已存在 active `dedupe_key` 的动作不会因为重复保存污染队列。
- 保存后的动作保留候选的来源、类型、优先级、目标范围和轻量 evidence，便于后续处理。

## 已确认事实

- `POST /api/v1/admin/interview-bank/ops-actions` 已支持手工创建动作，但后端固定 `source=manual`，不适合保存 generated source。
- `POST /api/v1/admin/interview-bank/ops-actions/candidates` 已支持 `health_diagnostic`、`index_status`、`retrieval_analytics` 三类候选，且只读不落库。
- `CreateInterviewBankOpsAction` 已能保存任意合法 source，但 Store 层当前不主动检查 active dedupe。
- `generateInterviewBankOpsActionCandidates` 已经通过 `activeInterviewBankOpsActionKeys()` 跳过 active 候选。
- 前端面试题库管理页已有 open 动作队列和手工创建表单，但没有候选生成、选择和保存 UI。

## 需求

1. 新增候选保存管理员接口：
   - `POST /api/v1/admin/interview-bank/ops-actions/candidates/save`
   - 请求体包含 `candidates: InterviewBankOpsActionCandidate[]`。
   - 只允许管理员调用，非管理员返回现有权限错误。

2. 保存规则：
   - 请求必须包含至少 1 个候选，最多保存 50 个。
   - 只接受 generated source：`health_diagnostic`、`index_status`、`retrieval_analytics`。
   - 不接受 `manual` 或 `retrieval_log` 作为候选保存 source。
   - 后端重新校验 `action_type`、`priority`、`source`、`title`、`reason`、`dedupe_key` 和目标范围。
   - 保存时固定 `status=open`，`created_by=adminID`。
   - 保存成功保留候选的 `dedupe_key`，不重新生成不同 key。
   - evidence 只保存候选传入的 compact evidence，不补 full query、完整回答、完整 atom 正文或 provider payload。

3. 去重规则：
   - 保存前读取 active 动作 key：`open/in_progress/watching/reopened`。
   - 已有 active key 的候选跳过并计入 `skipped_existing`。
   - 同一请求内重复 `dedupe_key` 只保存一次。
   - `resolved/dismissed` 不阻止未来保存同 key 候选。

4. 响应：
   - 返回成功保存的动作列表、`saved`、`skipped_existing`、`total`。
   - 候选全部被 active dedupe 跳过时仍返回 200，`saved=0`。
   - 非法候选返回 400，不写入该非法候选。

5. 前端管理页增强：
   - 在“运营动作”面板增加“生成候选”按钮。
   - 候选列表展示标题、类型、优先级、source、目标和原因。
   - 每个候选支持勾选，默认全选本次生成的候选。
   - “保存选中候选”调用新增保存接口。
   - 保存成功后刷新 open 队列，并清空已保存候选或保留未保存/被跳过提示。
   - 空候选、保存中、保存失败和 skipped existing 状态有明确反馈。

## 验收标准

- 管理员保存一个 `health_diagnostic` 候选后，open 动作列表能读回该动作，source 仍为 `health_diagnostic`。
- 保存 `index_status` 或 `retrieval_analytics` 候选时，动作保留 atom/组合目标和 compact evidence。
- 已存在 open 同 `dedupe_key` 动作时，保存候选返回 `skipped_existing=1` 且不新增动作。
- resolved 同 `dedupe_key` 动作不阻止保存新的 open 动作。
- 非管理员不能保存候选。
- 缺少候选、非法 source、缺少 `dedupe_key` 或缺少目标范围时返回 400。
- 前端可生成候选、勾选保存、刷新后看到 open 队列新增动作。

## 不做

- 不做动作详情页或详情抽屉。
- 不做 `PATCH /ops-actions/{id}` 状态流转。
- 不做关闭/忽略备注或历史表。
- 不做候选 dismiss。
- 不自动编辑、归档、恢复或重建索引。
- 不引入 LLM、embedding 或后台任务。
- 不改变普通用户报告接口。

## 风险与约束

- Store 层当前没有 active dedupe 唯一约束，本切片先在 admin 保存接口做显式 dedupe；并发双击仍需前端 disabled 和后端同请求去重降低概率，跨请求竞态可在状态流转/Store 约束切片再下沉。
- 前端保存的是候选快照，可能与再次生成的候选不同；这是可接受的，因为管理员保存的是当时看到的证据。
- 保存接口必须继续保持动作创建的治理边界，不允许 generated candidate 保存触发任何实际题库修改。

## 开放问题

无阻塞问题。按“独立候选保存接口 + 前端最小选择保存 + 不做状态流转”的边界实现。
