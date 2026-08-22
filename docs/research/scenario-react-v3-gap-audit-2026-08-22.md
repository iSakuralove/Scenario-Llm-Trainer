# Teaching-ReAct V3 当前差异审计与本阶段边界（2026-08-22）

## 目标锚点

本记录只服务于：

`docs/Teaching-ReAct 动态工具编排与支付回调题库 V3 重建实施计划.md`

当前不把任务收缩为单独的工具动画、失败注入或某一条 Phase 6 行为快照。支付回调固定题仍是第一份 V3 样板，现有用户未提交的题目 JSON 改动保持不动。

## 只读审计结论

### 已具备的能力

- Python AgentContext 已有 `phase`、`turn_id`、`original_user_message`、`action_history`、`tool_states`、`available_tools` 和安全 `guidance_state`。
- 同一 Turn 内已经存在模型 → Runtime Scheduler → 虚拟 Observation → 上下文回注的多轮循环。
- Go 负责 CAS、事件序号、提交屏障、持久化和公开事件投影。
- 前端已经能显示工具任务生命周期、公开 Observation 和安全详情入口。

### 与 V3 计划的结构性差异

1. AgentContext 仍以多个平铺字段承载 Turn 语义，没有单一的 `TurnEnvelope`；同一轮的继续关系、上一动作归属和当前轮次只能从多个字段拼接推断。
2. `phase` 只有 `new_user_turn` 与 `after_tool_call`，工具失败、拒绝、前置条件不足和最终收束没有明确阶段。
3. `ActionHistoryEntry` 缺少 `round` 与 `call_id`，工具结果能说明“哪个工具”，但不能稳定说明“哪一次调用”。
4. `ToolStateView` 只有粗粒度 `state/reason`，没有 `call_count`、`can_call`、`repeat_policy`、`last_call_id` 等 Runtime 可解释字段。
5. 题库 JSON 没有显式 `tool_dependency_graph` 字段；固定支付回调题的 evidence prerequisites 已经包含部分依赖事实，但当前 `turn_runtime.py` 仍使用空 `BatchScheduler()`，没有把依赖约束接入调度。
6. `AffectState` 与 `IncidentState` 尚未形成独立安全投影；当前安全状态主要通过 `TurnAssessment`、`GuidanceState` 和 `LearnerStateView` 分散表达。
7. 当前题库和数据库仍是 `hiddenworld.v1` / 旧 `scenario_questions` 等路径，尚未开始 V3 停机切换；本阶段不提前做数据库迁移。

## 本阶段选择

本阶段只推进计划中的“阶段 3：Teaching-ReAct Loop”契约骨架：

- 增加 Runtime 生成的 `TurnEnvelope` 安全投影；
- 保留旧字段作为兼容入口，但让 Prompt 以 Envelope 为单一当前 Turn 语义来源；
- 增加 `round` 与 `call_id` 的动作归属；
- 将 `phase` 扩展为 `new_user_turn`、`after_tool_call`、`after_tool_error`、`finalizing`；
- 失败、拒绝、超时和前置条件不足统一回注为 `after_tool_error`，不把失败当成 Observation；
- 把当前轮 Observation 与跨轮动作历史分开表达；
- 让预算和上一动作状态由 Runtime 写入，不由模型声称。

## 明确留到后续阶段的内容

- 从 evidence prerequisites 或 V3 Contract 导入正式 `tool_dependency_graph`，并完成有限自动补查；
- `available / authorized / in_flight / consumed / blocked / failed_retryable / failed_terminal` 全生命周期迁移；
- `LearnerState / IncidentState / AffectState / MentorPersona` 四层完整建模；
- `scenario.v3` 题库 Contract、版本表和支付回调题停机切换；
- 长会话、断流续接、CAS、提交失败等 Phase 6 行为快照逐项真实浏览器归档。

## 验收口径

代码改动完成后，先重建当前 API/Agent 镜像，再使用真实浏览器观察：

1. 普通消息首轮显示 `new_user_turn` 对应的公开阶段；
2. 同一 Turn 的工具回注不会把原话当成新请求；
3. 工具成功与工具失败分别进入 `after_tool_call` / `after_tool_error`；
4. 工具卡与后续 Agent 决策可通过 `call_id` 对应；
5. 刷新后只恢复安全事件，不恢复内部 Thought 或未提交状态。

没有浏览器证据的内容只标记为“已实现，待浏览器”，不宣称 V3 阶段完成。
