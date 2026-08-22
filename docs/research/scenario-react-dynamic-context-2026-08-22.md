# Scenario Teaching-ReAct 动态上下文阶段记录（2026-08-22）

> 本阶段解决的是“工具结果在同一轮能回注，但跨用户轮次消失”的状态断层。验收以本地真实浏览器为准；没有在浏览器中确认的前端详情能力仍标为待完成。

## 1. 暴露出来的真实问题

第一版只在 Python `AgentLoop` 的同一轮内维护 `phase`、动作历史和工具状态。用户在同一会话再次发送 `再看看 CPU` 时，旧 API/Agent 链路的 Thought 明确出现：

```text
tool_states show all available, action_history empty
```

这说明“工具已消费”只存在于当前请求的内存上下文，下一次 HTTP 请求没有从 Go 会话状态或历史公开事件回注。继续依赖这份实现会让 Agent 误以为工具消失、重复调用或把历史 Observation 当成新 Observation。

## 2. Python 安全上下文改动

### AgentContext 新增安全字段

- `phase`: `new_user_turn` 或 `after_tool_call`。
- `turn_id`: 当前请求的幂等/回合标识。
- `original_user_message`: 同一轮继续决策时保留用户原话。
- `action_history`: 只记录 `action`、`tool_name`、安全 `decision_summary` 和终态 `status`；不记录参数、授权 ID、证据 ID 或原始 Thought。
- `tool_states`: 工具的粗粒度状态和原因：`available`、`consumed`、`attempted`、`blocked`、`unavailable`。

### Runtime 循环行为

- 首次模型调用使用 `phase=new_user_turn`。
- 工具结果（包括拒绝、超时、前置条件失败）回注后，下一次模型调用使用 `phase=after_tool_call`。
- `after_tool_call` 明确告诉模型这是同一轮继续，不是新的用户消息；`original_user_message` 保持不变。
- 同一轮及回复 Guard 重试都复用同一份动作历史和工具状态，避免重生成时状态丢失。
- 已消费工具会从后续授权候选中移除，并由 `BatchScheduler`、`VirtualObservationExecutor` 双层拒绝重复执行。

## 3. Go → Python 跨轮传递

`agentclient.TurnRequest` 与 Python `AgentTurnRequest` 现在携带：

```text
phase
turn_id
original_user_message
action_history
tool_states
```

Go 入口从已持久化的公开 `task_upserted` / `tool_result` V2 事件，以及 Go 权威 `LearnerState.ActionsTaken` 重建最近动作摘要和工具状态：

- 成功工具：`consumed / 本会话已使用，不可重复调用`。
- 失败或超时：`attempted / 本次动作未形成公开观察`。
- 拒绝：`blocked / 本轮动作未获 Runtime 批准，不重复尝试`。
- 不支持：`unavailable / 题目当前没有声明该工具`。

只筛选题目声明的公开观察工具；`compare_answer`、参数、证据 ID、答案比较和内部授权信息不会进入 AgentContext。

## 4. 真实浏览器证据

### 修复前暴露缺口

旧 Agent 镜像在会话 `7cc731f15cd7f6138534b47b6caf24db` 的第二轮 Thought 中看到：

```text
tool_states show all available, action_history empty
```

该证据确认跨请求状态未回注，不能把当时的重复调用倾向归因于模型本身。

### 修复后闭环

新建会话：

```text
session_id=81067e0019bb864280c9c1e9a3dde2df
题目=支付回调在网关切换后间歇性超时
```

第一轮发送：`先看看 CPU`。

- 轮次从 `0/50` 推进到 `1/50`。
- CPU 工具成功返回，形成 1 条公开证据。
- 刷新后 Observation 和导师回复保留，原始 Thought 不回放。

第二轮发送：`再看看 CPU`。

- Agent Thought 明确识别：`CPU metrics already consumed, tool state consumed`。
- 没有再次出现 CPU 工具卡，也没有重复读取 Observation。
- 最终回复为：`CPU 指标本轮不可重复查询，转向时间线视角。`
- 轮次推进到 `2/50`，刷新后第二轮安全摘要仍保留。

这证明跨轮工具状态已经从 Go 会话历史进入 Python Agent，并且 Runtime 授权/执行层也拒绝重复消费。

## 5. 本阶段未完成项

- 前端工具卡点击后展示标准安全 JSON 的详情面板仍未完成；当前真实浏览器只能确认卡片进入展开状态，未看到可见 JSON 代码块。
- 动态 `tools_list` 的专门公开目录视图、QuickAction 消费后的前端隐藏规则仍需与现有 `action_catalog`/Runtime 授权统一。
- 用户瞬时 Affect、长期掌握度权重、方向偏离状态的进一步 UI/提示词投影待下一阶段实现。
- 提交结论、断流续接、失败回收和 `repair_status` 在线提交仍按既有 Phase 6 清单后置验收。
