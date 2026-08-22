# Scenario ReAct：跨轮方向教学信号回注（2026-08-22）

## 阶段结论

本阶段把“学生上一轮的排查方向是否仍然贴合当前故障”归约成一个可跨轮回注的安全教学信号，并接入 Go → Python → Agent Prompt 的链路。

它解决的是承接问题，不是把内部裁判结果直接暴露给模型或学生：

- 上一轮沿证据推进时，下一轮知道应继续保持节奏；
- 上一轮仍在建立链路时，下一轮不会把“尚未证实”误写成结论；
- 上一轮停滞、随机排查或证据不支持时，下一轮会收拢范围；
- 上一轮明显偏离主题时，下一轮先把对话拉回当前故障；
- 当前用户消息提供了新的公开事实时，旧方向信号只作教学提示，不能覆盖本轮事实。

## 对外允许的四种状态

| `direction_status` | 学生侧语义 | Agent 承接原则 |
| --- | --- | --- |
| `aligned` | 沿证据推进 | 保持当前排查节奏，继续解释公开观察能证明什么 |
| `exploring` | 正在建立链路 | 区分事实、推断和待确认部分，不提前下结论 |
| `needs_refocus` | 需要收拢范围 | 缩小讨论面，避免继续堆叠无关方向 |
| `off_topic` | 先回到当前故障 | 可以承接情绪或闲聊，但不泄露内部答案，不伪造工具观察 |

这些值只描述“下一轮应该如何承接”，不携带以下内容：

- `hypothesis_id`、`evidence_id`、CanonicalAnswer 或内部答案比较；
- 具体哪个假设错误、哪个候选被排除；
- 工具授权标识、工具参数和模型原始 Thought；
- 任何可以直接替学生完成结论的隐藏证据。

## 代码改动

### Python 合同与归约

修改文件：

- `agent/src/hiddenworld/contracts/assessment.py`
- `agent/src/hiddenworld/contracts/agent_context.py`
- `agent/src/hiddenworld/contracts/transport.py`
- `agent/src/hiddenworld/scenario_runtime/context.py`
- `agent/src/hiddenworld/scenario_runtime/state_reducer.py`
- `agent/src/hiddenworld/scenario_runtime/agent_loop.py`
- `agent/src/hiddenworld/agents/scenario_agent.py`

主要变化：

1. `TurnAssessment` 通过 `direction_status_for_assessment(...)` 做确定性归约：
   - `is_off_topic` 或 `intent=off_topic` → `off_topic`；
   - 卡住、随机调查、无进展、不支持或矛盾 → `needs_refocus`；
   - `progress=progress` → `aligned`；
   - 其余情况 → `exploring`。
2. `GuidanceState` 增加 `direction_status`，默认值是 `exploring`，旧请求可以继续按旧字段运行。
3. `AgentTurnRequest` 接收可选的上一轮 `guidance_state`；`project_agent_context(...)` 只把安全教学切片投影到 `AgentContext`。
4. 如果本轮没有新的 `TurnAssessment`，StateReducer 保留上一轮方向信号，而不是静默重置为 `exploring`。这条规则避免了“模型没有给出评估”被误解成“学生方向已经恢复”。
5. Agent Prompt 说明四种状态的承接方式，并明确：当前轮公开事实优先，方向状态不能用来泄露内部假设或越权调用工具。

### Go 传输与持久化回注

修改文件：

- `backend/internal/agentclient/types.go`
- `backend/internal/httpapi/handlers_scenarios.go`
- `backend/internal/httpapi/scenario_agent.go`

主要变化：

1. `TurnRequest` 增加 `GuidanceState` 指针字段。
2. 处理下一轮消息时，从最近一条已持久化的 `TeachingProjection` 构造 `scenarioPriorGuidanceState(...)`。
3. 回注前对 `direction_status` 做白名单归一化；未知值降级为 `exploring`，不会把不受控字符串传给 Agent。
4. 非旧合同响应会校验 `GuidanceState.DirectionStatus` 是否与 Go 根据 `TurnAssessment` 派生的值一致，拒绝模型自报与评估不一致的方向信号。
5. 旧消息没有 `TeachingProjection` 时返回 `nil`，Python Runtime 从现有 `LearnerState` 构造兼容默认值，不阻断历史会话。

## 与现有安全边界的关系

方向信号不等于工具可用性，也不等于工具推荐概率：

- `available_tools` 决定当前可以公开选择的工具目录；
- `tool_states` 说明工具已消费、被阻断或等待明确请求的原因；
- `direction_status` 只影响解释节奏、承接方式和教学语气；
- 工具是否能真正执行，仍由 `authorized_actions`、Runtime 预算和 Go 审批决定。

因此，即使方向状态是 `needs_refocus`，Agent 也不能因为“想帮学生收拢范围”就凭空调用不在授权集合中的工具；它只能回复安全解释，或等待下一轮明确动作。

## 兼容性与失败策略

- 缺少 `guidance_state`：按旧请求处理，使用 `LearnerState` 的焦点和停滞次数构造最小导航。
- 缺少 `direction_status`：Pydantic 使用 `exploring` 默认值；Go 对旧响应不强制比较该字段。
- 存在未知方向值：Go 归一化为 `exploring`；不会把未知字符串直接送入 Prompt。
- 会话已经 `terminal`：继续沿用上一轮权威教学状态，迟到请求不能重写方向导航。
- 本轮没有形成公开观察：前端仍不应生成“已查到新证据”的文案；方向信号只影响承接，不制造事实。

## 前端状态动画关联

工具状态动画继续遵循 Runtime 事件，而不是浏览器端计时器：

- `pending`：同一图标槽位中的虚线环慢速旋转，表示调用已排队但尚未开始；
- `running`：切换到高亮 Loader，工具行出现青绿色强调背景，表示执行中；
- `completed`：交叉淡入勾选图标，表示收到成功结果；
- `failed`、`timeout`、`rejected` 等终态：显示错误/跳过状态，不能误显示为成功。

该动画的实现与跨轮方向回注相互独立：动画只消费公开的任务生命周期事件，方向信号只消费安全教学投影。

## 本阶段验证边界

- 已完成源码差异审查，并修正 StateReducer 在缺少新评估时覆盖旧方向信号的问题。
- `git diff --check` 只报告工作区既有的换行格式警告，没有新增空白错误。
- 按当前验收约束，本阶段未运行测试套件，也未进行额外编译校验；Docker 重建仅用于让真实浏览器加载最新 Go/Python 服务。
- Docker 重建后，真实浏览器已完成一次 `off_topic` → 回到故障的跨轮验证：偏题回合显示粗粒度“先回到当前故障”，下一轮恢复到“正在建立链路”，当前焦点回到“资源”。
- 同一验证中页面没有显示 `hypothesis_id`、`evidence_id`、`requested_action_raw` 或校验错误；浏览器控制台错误与警告均为 0。
- 该证据说明跨轮方向回注已闭环；`needs_refocus` 的独立卡住路径仍留给后续行为矩阵验收，不把 `off_topic` 证据扩大解释成所有方向状态均已验收。

## 后续工作

1. 为 `needs_refocus` 补一条独立的连续卡住/随机调查行为快照。
2. 在不改变本阶段安全投影边界的前提下，继续补 Phase 6 的失败/断流终态证据。

## 真实浏览器证据摘要

页面：

`http://localhost:5173/scenarios/session/6c87e16b7c99e59cee7c3afa55c3e149`

### 偏题回合

用户消息：

```text
我们先别管这个故障了，聊聊世界杯和电影吧。
```

页面学习状态：

```text
回应节奏：先拉回当前故障
方向信号：先回到当前故障
本轮进展：正在判断
```

导师可以自然承接闲聊，但没有调用未授权工具，也没有把内部方向判断写进正文。

### 回到故障回合

用户消息：

```text
好，回到故障。我想继续看资源问题。
```

页面学习状态：

```text
回应节奏：沿公开证据排查
方向信号：正在建立链路
当前焦点：资源
```

导师正文继续承接此前公开的临时盘、日志和内存观察，没有重复调用已消费工具，也没有显示隐藏证据。

### 额外阻断修复

偏题回合第一次尝试时暴露了 `requested_action` / `requested_action_raw` 缺省不一致，API 曾在 proposal validation 阶段返回 502。Runtime 归一化后重新部署，偏题与回到故障两轮均返回 200 并正常落库；修复证据记录在 `scenario-react-assessment-normalization-2026-08-22.md`。
