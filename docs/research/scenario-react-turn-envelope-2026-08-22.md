# Teaching-ReAct V3 TurnEnvelope 阶段记录

日期：2026-08-22

Trellis 任务：`.trellis/tasks/08-22-v3-turn-envelope-loop`

## 1. 阶段目标

本阶段把同一用户 Turn 的首次请求、工具成功回注、工具失败回注和最终收束统一为 Runtime 生成的 `TurnEnvelope`。同时收口工具调用的 `round + call_id` 归属，避免模型把工具回注误判为新用户请求，也避免前端把同一工具调用渲染成两条任务。

## 2. 跨层实现

### Python Runtime

- `TurnEnvelope` 统一承载 `turn_id`、`state_revision`、`round`、`phase`、`input_source`、用户原话、continuation、上一动作和当前轮 Observation。
- `TurnPhase` 固定为 `new_user_turn`、`after_tool_call`、`after_tool_error`、`finalizing`。
- `ActionHistoryEntry` 记录 `round` 与 `call_id`；`ToolStateView` 记录 `call_count`、`can_call`、`blocked_reason`、`last_call_id` 和重试策略。
- `AgentLoop` 在每轮更新 envelope、action history、tool states 和 current-turn observations。成功工具进入 `after_tool_call`；拒绝、失败、超时或前置不足进入 `after_tool_error`；最终评估前记录 `finalizing`。
- Prompt 明确同一 Turn 的工具回注不是新的用户请求，失败结果不属于 Observation，`can_call=false` 的工具不能重复调用。

### Go 入口与事件投影

- `TurnRequest.TurnContext` 与 Python transport 同步，普通消息和 QuickAction 都标记 `input_source`。
- Go 入口生成初始 `TurnEnvelope`，公开 trace、任务事件和工具结果统一贯穿 `round + call_id`。
- 同一次调用的任务身份使用稳定的 `task_id = obs:<call_id>`；`round` 描述轮次，不参与替代调用身份。

### 前端任务归并

- 新事件优先按 `task_id` 归并，其次使用 `call_id`。
- 仅为历史错误回放保留窄兼容：当终态事件使用旧的 `obs:<tool_ref>` fallback、且同工具同轮只有一个 running/pending 候选时，终态事件合并到该候选。
- 不按工具名全局去重，因此同一 Turn 内合法的两次同工具调用仍可由不同 `call_id` 保持独立。
- 工具结果详情使用同样的 legacy fallback 关联规则，避免 TaskList 已合并后再出现孤立的重复详情行。

## 3. 重复任务根因与修复

在真实 SSE 事件中观察到同一业务调用的身份不一致：

```text
agent_tool_started:
  sequence=2, task_id=obs:call-2, call_id=call-2, round=1

agent_tool_result:
  sequence=3, task_id=obs:inspect:change.gateway_release,
  call_id=inspect:change.gateway_release, round 缺失
```

根因是 `agent/src/hiddenworld/scenario_runtime/turn_runtime.py` 的成功 Observation 分支在发出 `agent_tool_result` 时漏掉了 `round` 和 `call_id`。同时，旧回放数据可能把工具名当成 fallback call id。修复包括：

1. Runtime 成功 Observation 事件显式写入 `round=self._round` 和 `call_id=result.call_id`。
2. Loop 事件处理保留完整 ToolCall 对象，避免只传工具名导致调用身份丢失。
3. 前端对旧终态事件增加受限归并，仅处理可证明属于同一 pending 调用的历史数据。
4. Python 回归测试断言 started/result 两个事件都携带相同的 `call_id=call-1` 和 `round=1`。

## 4. 真实浏览器验收

### 成功与刷新恢复

会话：`976d5493e53d17997da97d27be41cfdc`

依次调用回调访问日志、网关 VIP 发布记录、Nginx 回调访问日志和网关切换前后配置差异后，页面显示：

- `4/50` 轮；
- 已形成证据 `4`；
- 重要线索 `3`；
- 四次工具调用各只有一条“工具调用完成”任务和一条对应详情，没有同名 running/completed 双行。

刷新页面后上述计数和四条完成状态保持不变，证明正式历史回放没有再次插入重复任务。

### 错误回注

在同一会话再次请求已经消费的 Nginx 回调访问日志后，页面从 `4/50` 进入 `5/50`，证据仍为 `4`，并显示“本轮没有新增公开观察”。页面没有伪装成成功 Observation，也没有新增成功勾选；可用工具列表恢复为可调用状态。调试 Thought 只在当前运行区展示，未进入正式历史内容。

浏览器控制台错误和警告：`[]`。

## 5. 安全与协议边界

- 正式公开事件不包含原始 Thought；调试思维链只属于当前运行区。
- 失败结果进入 `after_tool_error` 和 action history，不进入成功 Observation 或证据集合。
- `can_call` 由 Runtime 写入，模型不能自行修改。
- `finalizing` 只表示 Runtime 的停止意图，不允许重新发起工具动作。

## 6. 检查边界与未覆盖项

- 本阶段以真实浏览器、SSE 归属、刷新恢复和页面错误日志为主要验收依据；没有运行全量自动化套件。
- 正式 `tool_dependency_graph`、Learner/Affect/Incident 三层状态模型、V3 题库停机迁移和全量 Phase 6 浏览器矩阵属于后续子任务。
- 旧历史事件的 fallback 只用于只读兼容；新事件必须由后端保持稳定 `call_id + round`，不能依赖前端猜测。
