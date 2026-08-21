# Design：排查工坊单 Agent + 工具调用 Runtime 重构

证据基础：`research/code-evidence.md`（逐文件核对，行号以当前 main 为准）。

## 1. 总体架构与四层边界

```
┌─ Frontend (React/SSE) ──────────────────────────────┐
│ 只消费 RunEvent(判别联合 payload) + PublicContent     │
│ 渲染 markdown_ready；无身份文字；事件驱动状态          │
└──────────────▲──────────────────────────────────────┘
               │ SSE (schema_version 路由)
┌──────────────┴──────────────────────────────────────┐
│ Go httpapi（复核层，权威状态持有者）                    │
│ • 事件校验（判别联合矩阵）• 提议审批 • revision/幂等    │
│ • SSE 发布 + 持久化 • AllowedAction 生成              │
└──────────────▲──────────────────────────────────────┘
               │ HTTP /turn (agentclient, 严格解码)
┌──────────────┴──────────────────────────────────────┐
│ Python hiddenworld Runtime（确定性执行内核）            │
│ • ScenarioAgentLoop 调度（≤11 轮模型 / ≤10 次工具）    │
│ • 工具批次执行（无依赖并行/有依赖跨轮）                  │
│ • StateReducer 状态归约 + GuidanceState 投影           │
│ • 事件发布（严格判别联合）• 预算/超时/失败确定性处理      │
└──────────────▲──────────────────────────────────────┘
               │ AgentContext（唯一入口，投影而成）
┌──────────────┴──────────────────────────────────────┐
│ ScenarioAgent（单 LLM 节点）                          │
│ 理解 / 规划 / 决定 tool_calls 或 final_reply           │
│ 看不到 ScenarioContract、CanonicalAnswer、内部比较     │
└──────────────────────────────────────────────────────┘
```

**职责矩阵**

| 职责 | ScenarioAgent | Python Runtime | Go httpapi | Frontend |
|---|---|---|---|---|
| 理解用户消息、规划下一步 | ✅ | — | — | — |
| 决定是否调用工具 | ✅（只能选 Runtime 暴露的工具目录） | 复核合法性 | — | — |
| 工具执行（虚拟数据） | — | ✅ 确定性 | — | — |
| 权限/预算/超时/幂等 | — | ✅ | ✅ 复核 | — |
| 状态归约与 GuidanceState | — | ✅ StateReducer | ✅ 审批提议 | — |
| CanonicalAnswer 比较 | — | ✅ InternalComparison | ✅ 仅审计 | — |
| 事件发布与排序 | — | ✅ 产出 | ✅ 校验+SSE | 消费 |
| AllowedAction 生成 | — | ✅ 候选 | ✅ 终审 | 渲染 |
| 持久化 | — | — | ✅ | — |

## 2. AgentContext 与 ScenarioContract 字段边界

**ScenarioContract（Runtime-only）**：题目同源、同版本、同快照的完整不可变契约。
- 字段：PublicScenario、Hypotheses、ActionCatalog/VirtualTools、WorldObservations、ClueGraph、DiagnosticRelations、RootCause、CanonicalAnswer、SolutionRubric、misconception_rules。
- `CanonicalAnswer` 是独立持久化字段，不是运行时临时从 RootCause 拼出的对象；至少包含 `canonical_conclusion`、`required_evidence_ids`、`required_causal_relations`、`accepted_equivalents`、`solution_requirements`，并绑定 `answer_version`。
- 消费者仅限：工具执行引擎（WorldObservation 生成）、StateReducer（EvidenceEngine/AntiGuess/RootCauseVerifier 吸收）、InternalComparison（AnswerComparator）、Guard、审计。

**AgentContext（Agent 唯一输入）**：

| 字段 | 类型/来源 | 说明 |
|---|---|---|
| public_scenario | PublicScenario 投影 | 题面，同现状 |
| transcript | Turn[] | 对话历史 |
| learner_summary | LearnerStateView | 已确立事实/已做动作/当前焦点/排除项（抽象标签）——同现状 MentorDeps.learner_state |
| teaching_navigation | TeachingNavigation **新增** | 四个抽象维度：waiting_time / dependency_latency / capacity_saturation / causal_chain；每个取值 = {status: 未探索/进行中/已覆盖, hint_level}，由 GuidanceState 投影，**不含任何答案关键词** |
| action_catalog | ActionCatalogEntry[] | Runtime 暴露的可调用工具目录（tool_id/kind/target/参数 schema），即现有 virtual_tools 的 Agent-visible 投影（**去掉 simulated_output、evidence_ids、内部 observation 映射细节**） |
| tool_results | 最近一轮工具结果（Agent-visible 投影） | WorldObservation 的公开文本 + AgentComparison 信号 |
| budget | 剩余模型轮次/工具调用次数 | Agent 感知预算以自行收束 |
| turn_control | `AgentTurnControlView` | 只读暴露 `terminal`；不暴露 `completion_allowed` / `completion_ready` 或内部比较结果 |

**禁止进入 AgentContext**：ScenarioContract 本体、CanonicalAnswer、InternalComparison、答案原文、精确缺失项（missing_evidence/missing_solution_requirements 原文）、相似度（claim_alignment 等）、未释放 EvidenceNode.content、发布策略、hidden_world 键。

**物理隔离方式**：AgentContext 是独立 dataclass（contracts/agent_context.py），Runtime 构造时逐字段白名单投影（类似现有 `to_public()` 的"笨搬运"模式 + `test_mentor_deps_field_boundary` 式字段白名单测试）。ScenarioContract 对象在 Runtime 进程内存在，但 Agent 构造函数签名只接受 AgentContext——类型层面不可误传。

## 3. RunEvent 严格判别联合

外层统一（Go/前端/SSE 传输层一致）：

```
RunEvent {
  request_id: string          // 幂等键
  sequence: int               // 单调递增且正式序列稳定（见 §4）
  schema_version: string      // "hiddenworld.v2"
  state_revision: int         // 每个事件必有，描述该事件所属的业务状态版本
  kind: enum
  payload: <按 kind 判别>
}
```

payload 判别联合（Python 端 Pydantic discriminated union，Go 端 sealed 结构，TS 端 discriminated union）：

| kind | payload 字段 | 触发时机 |
|---|---|---|
| turn_started | { turn_id, task_summary? } | 每轮开始 |
| task_upserted | { task_id, call_id?, title, state: ToolCallState, tool_ref? } | Agent 规划出多步任务或工具生命周期状态变化 |
| tool_result | { call_id, tool_id, tool_kind, result_status: succeeded/failed/timeout, duration_ms, content?: PublicContent, error_code? } | 工具执行结束后发布；不表示 pending/running，未执行的 pending/rejected/unsupported 只通过 task_upserted 表达 |
| clue_published | { clue_id, content: PublicContent, dimension: TeachingNavigation 维度 } | ClueGate 释放线索 |
| assistant_delta | { phase: understanding/replying, text } | 理解摘要流（phase=understanding，承接现有 public_summary 能力）与正文流（phase=replying） |
| turn_completed | { next_actions: AllowedAction[] } | 轮次成功收束 |
| turn_failed | { error_code, retryable } | 轮次失败 |

- **删除**：reasoning_summary_delta/completed、observation_result（并入 tool_result + PublicContent(content_type=observation)）、tool_started/tool_completed（并入 tool_result.status）、response_summary、mentor_buffered、guard_passed、proposal_approved（内部过程不再外发；Go 复核失败直接表现为 turn_failed）。
- `AgentModelOutput` 只有两个判别分支：
  - `kind=tool_calls`：`public_summary`（必填且超短）+ `calls[]`；
  - `kind=final_reply`：`public_summary?` + `reply`，纯 final_reply 默认不额外发送摘要。
  Runtime 收到完整结构后，将 tool_calls 分支的 `public_summary` 投影为 `assistant_delta(phase=understanding)`，再发布任务和工具事件；不额外运行摘要模型。
- `DebugTraceEvent` 是独立调试协议（如 `debug_trace_delta` / `debug_trace_completed`），仅当非生产且显式开启时通过单独调试通道外发；它不属于正式 `RunEvent` 联合、不进入正式 SSE、不落 public_trace 表、不写正式审计。GitHub 议题“冻结 reasoning 调试流与正式公开模式边界”确定的是双通道隔离语义，本地 V2 契约将公开命名统一为 `debug_trace_*`。

**PublicContent**（tool_result 与 clue_published 的内容外层）：

```
PublicContent {
  content_type: "observation" | "clue" | "assistant"
  markdown_ready: string      // 唯一渲染源
  display_variant?: "log" | "tool_return" | "clue" | "reply"
  meta?: { tool_kind?: logs/metrics/config/database/dependency, is_negative?: bool }
}
```
- 日志类 → 直接日志文本；指标/健康度/配置 → 简短 Tool return 风格；主动释放 → 线索卡。开头不出现"模拟订单库写入日志"等实现术语（文案规则：以对象与状态开头，如"订单库写入日志（10:00-10:20）：…"，实现术语"模拟"不出现在用户可见开头）。

## 4. sequence / state_revision / schema_version 生命周期

| 维度 | 生成者 | 作用域 | 生命周期 |
|---|---|---|---|
| sequence | Runtime/Go 在正式事件序列确定时分配并持久化 | 单 request 单轮 | 严格递增；断线重连按 request_id + after_sequence 恢复；已持久化 sequence 在重连、重放、重新建立 SSE 连接时不得重新编号；去重键 request_id+sequence |
| state_revision | Go（DB scenario_sessions.state_revision） | 会话级业务状态 | 每个正式事件外层必带；同一状态快照的连续事件可保持同一 revision；只有 Runtime 成功提交业务状态时才提升；冲突返回 409；QuickAction 与自然语言共用同一 CAS/revision 链路 |
| schema_version | Go（事件出口）+ Python（契约常量） | 协议级 | "hiddenworld.v1" → "hiddenworld.v2"；SSE 首事件携带；前端按版本路由解析器；落库旧 v1 trace 只读兼容、新写入一律 v2 |

要点：实时序号与落库序号分离的教训（spec Scenario 4）保留，但不得在每次 SSE 重连时重新编号。新 v2 事件序列直接持久化；旧 v1 事件由 `LegacyEventAdapter` 使用稳定源序号或一次性确定性索引适配，适配结果仅用于统一展示模型。

**ToolCallState 与 ToolResult 分层**：

```text
ToolCallState = pending | running | completed | failed | unsupported | rejected | expired | already_completed
ToolResult    = result_status(succeeded | failed | timeout) + optional PublicContent
```

`task_upserted` 负责表达 ToolCallState 生命周期；`tool_result` 只在工具真正结束后表达执行结果。`started` 不是 ToolResult，预算拒绝、unsupported、pending 也不伪造工具结果。

## 5. ToolCall 批次调度模型

```
ScenarioAgentLoop:
  for round in 1..11:                       # 模型轮次上限
    output = agent.run(AgentContext + 本轮累积 tool_results)
    if output == final_reply: break
    batch = output.tool_calls
    # Runtime 批次校验（确定性，不信任模型自报独立性）:
    #  - 每个 call: 工具在 action_catalog 内? 参数过 schema? 预算剩余>0?
    #  - 同批去重（同工具+同参 hash）
    #  - 依赖判定: Runtime 按 ToolDependencyGraph 判定（静态声明:
    #     logs/metrics/config/database/dependency 只读查询互相无依赖；
    #     compare_answer 依赖本轮 answer_attempt 存在;
    #     任何写类/状态类工具与其余全部串行）
    #  - 违规处理: 拆批（把依赖后置到下一轮）或拒绝违规 call（通过
    #     task_upserted(state=rejected, error_code=dependency_violation) 表达）
    independent = batch 中无依赖的只读调用
    results = await asyncio.gather(*[execute(c) for c in independent])   # 并行
    tool_results → 注入 AgentContext → 下一轮
  预算耗尽且无 final_reply → Runtime 强制收束: 用已收集公开材料生成
  一次"总结回复"提示模型收束（一次机会），仍无 → turn_failed(error_code=budget_exhausted)
```

- 逻辑工具调用计数：一次业务调用 =1（含重试内幕不计），QuickAction 点击同样 +1，上限 10。
- 工具失败/超时：单工具失败不终止轮次，`tool_result(result_status=failed/timeout)` 返回给 Agent 决策；连续失败 2 次同一工具 → Runtime 通过 `task_upserted(state=rejected)` 拒绝再调用该工具（error_code=duplicate_refused）；deadline 到 → TurnDeadlineExceeded（现状类保留），已发事件不撤回，补 turn_failed。

## 6. GuidanceState / TurnControl / StateReducer 数据流

```
ScenarioContract ──┬─ WorldObservations(工具执行产出)
                   ├─ DiagnosticRelations(RootCauseVerifier 关系)
                   ├─ ClueGraph(EvidenceGraph+ClueGate)
                   └─ GuidancePolicy(TeachingPolicy 约束编译)
                          │
                          ▼
CanonicalAnswer → InternalComparison(AnswerComparator, Runtime-only)
                          │
                          ▼
              StateReducer(EvidenceEngine+AntiGuess 吸收)
                ├─ LearnerState 投影（提议，Go 审批）
                ├─ GuidanceState（Agent 可见导航切片）
                │     → teaching_navigation 四维度
                │     → AgentComparison(若本轮有 AnswerAttempt)
                │        {conclusion_status, evidence_status, causal_status,
                │         missing_dimensions(抽象枚举), contradictions(学生自述)}
                └─ TurnControl（轮次控制）
                      ├─ completion_allowed / completion_ready   # Runtime+StateReducer 私有
                      ├─ allowed_actions(ActionCatalog∩GuidanceState∩预算)
                      └─ terminal → AgentContext.turn_control 只读观察
```

- 现有 kernel 组件去向：cluegate/evidence/verifier/antiguess/policy **保留实现、重新编组**为 StateReducer 的子模块（不重写算法，只改调用边界与输出形状）。
- missing_dimensions：只允许 {waiting_time, dependency_latency, capacity_saturation, causal_chain, verification} 的子集——从 InternalComparison.missing_evidence 的 evidence category **映射到抽象维度**，绝不下发原文。
- contradictions：沿用现有"只引用学生自己原话"的实现。
- completion_allowed（原 AntiGuess.completion_allowed）：不再进 AgentComparison（现状 PublicAnswerComparison 无此字段，继续保持），仅 TurnControl 内部用于 turn_completed 是否允许携带"提交结论"动作；terminal 同样只属于 TurnControl/SessionState，不进入 AgentComparison。

## 7. CanonicalAnswer 隔离方式

1. 存储：CanonicalAnswer 作为 ScenarioContract 的独立字段，与题目同快照、同版本、同哈希持久化，绑定 `answer_version`；不再把它定义为 RootCause 的临时派生物。
2. 运行时：AnswerComparator 只被 Runtime 调用（现状已如此）；InternalComparison 只进 InternalVerification 审计通道（现状保持）。
3. Agent 侧唯一投影 = AgentComparison（§6 白名单），由 StateReducer 从 InternalComparison **二次投影**；字段白名单测试固定，terminal 不进入比较结果。
4. compare_answer 无参数化：`Tool(compare_answer, takes_ctx=True)` 签名改为零参数；Runtime 在检测到本轮 user_message 构成答案尝试（沿用现有 contains_answer_attempt 判定）时自动创建 AnswerAttempt 并绑定；Agent 调用即比较当前绑定 attempt；无绑定时返回 `tool_result(result_status=failed, error_code=no_answer_attempt)`。Go 侧校验从 `redacted_arguments["answer_attempt_id"]` 改为"答案尝试轮才允许出现 compare_answer tool_result 且无参数"。

## 8. PublicContent 映射

| 内部对象 | PublicContent.content_type | 渲染（前端） |
|---|---|---|
| WorldObservation（工具查询结果） | observation | 日志类：日志正文块；指标/健康度/配置：简短 Tool return 卡 |
| TeachingClue（主动释放） | clue | 线索卡（维度标签 + 文本） |
| AgentReply（final_reply） | assistant | 聊天正文（markdown） |
| ToolResult 执行包 | 不属于 `PublicContent` 类型；作为 `tool_result` 事件的执行元数据外层，可选携带 observation/clue 的 `PublicContent` | 工具行显示状态、图标、耗时；正文仍只取 `content.markdown_ready` |

前端只渲染 markdown_ready；meta.kind 驱动图标（logs=文件、metrics=仪表、config=滑杆、database=库、dependency=链路）。

## 9. 兼容迁移与回滚策略

**双形状共存期**（单一部署内）：
- Go SSE 出口按 schema_version 路由：v1 事件（存量 DB trace 重放/历史会话加载）原样透传；v2 事件新解析器。前端两种解析器并存，按首事件版本选择。
- 新写入（新一轮对话）一律 v2。历史会话只读兼容，不回填。
- v1 读取先经过 `LegacyEventAdapter`，转换为统一 ViewModel 后仍使用同一 AgentRun UI；适配层不生成新的 Runtime 事实，也不改变稳定的历史顺序。
- Python /turn 服务同时可输出 v2（新主链）；v1 输出代码在切换 commit 中删除——Go 端通过 contract_version 拒绝旧版（现状 ContractVersionMismatch 整轮拒绝机制沿用，直接把 CONTRACT_VERSION 升到 v2 即天然拒绝旧 Python）。

**回滚策略**：
- 每个实施阶段独立 commit（见 implement.md）；任一阶段失败回滚该 commit。
- 数据库无 schema 破坏性变更（新增列除外），回滚代码即可。
- 最终删除 v1 解析器前设一个"确认点"（用户验收 SSE 全链路后），删除动作单独 commit，可单独 revert。

**旧协议删除顺序**（依赖倒序）：
1. 前端 v1 类型与渲染分支（等 Go 不再产 v1 新事件）。
2. Go v1 校验矩阵（phaseByKind/guard_passed 强制/toolStage 三段机）与"排查导师"文案。
3. Python 旧 runtime.py 双节点主链、interpreter.py/mentor.py、events.py 旧 kind、deps.py 旧 Deps。
4. contracts/world.py `VirtualTool.simulated_output`（题库 JSON 字段保留数据、代码不再读取，加 deprecation 注记；后续题库版本再清理数据）。
5. agent/runtime.go 通用 ToolResult/emitStage（确认无其他链路引用后收敛）。
6. 前端 types/index.ts hidden_world（社区预览若仍需，收敛到管理端专用类型）。

## 10. 十项已知冲突的迁移方案（对应需求 §10）

| # | 冲突（证据） | 迁移方案 |
|---|---|---|
| 1 | runtime.py interpreter+mentor 双节点（L83-84） | 新建 agents/scenario_agent.py 单节点 + runtime/agent_loop.py 调度；interpreter/mentor 的指令精华合并进单 Agent system prompt；AgentModelOutput 的 tool_calls 分支同时承载超短 public_summary 与工具调用决策，不另跑摘要模型 |
| 2 | world.py VirtualTool.simulated_output（L105） | 契约中将该字段标记 Runtime-only（AgentContext 投影时剔除，§2 action_catalog）；WorldObservation 生成继续用 Observation.result 为权威，simulated_output 降级为题库冗余注记 |
| 3 | agentRun.ts 旧 reasoning/tool 事件（L3-43） | 整文件重写为 §3 判别联合 + PublicContent |
| 4 | AgentRun.tsx 固定 thinking 文案（L218-222）+ "排查导师" aria-label（L50） | aria-label 改中性（如"助手"或空）；thinkingLabel 改为纯事件驱动：由最近事件 kind 推导（收到 tool_result→"查询返回"；收到 clue_published→"新线索"；收到 assistant_delta(phase=replying)→隐藏 thinking），同轮不同事件序列产生不同文案 |
| 5 | 前端 hidden_world 类型（types/index.ts L98） | 从 ScenarioContent 学生侧类型移除；管理端预览如需另建 admin 类型（本轮只做移除+引用收敛） |
| 6 | Go 旧 stage 与通用 ToolResult（runtime.go L18/L275；scenario_agent.go phaseByKind） | scenario 链路改判别联合校验；agent/runtime.go 经引用排查后隔离为非 scenario 专用或类型化收敛 |
| 7 | compare_answer answer_attempt_id 参数（tools.py L69；scenario_agent.go L514/L742） | §7 无参数迁移，两侧同 commit 改 |
| 8 | 不重复引入关键词意图分类器 | _resolve_declared_virtual_tool 逻辑保留并挂在工具执行入口（Runtime 侧），Agent 输出的 tool_calls 经同一映射校验 |
| 9 | 理解摘要保留但不停住 | 仅 tool_calls 分支必须提供 public_summary；Runtime 先发 assistant_delta(phase=understanding)，再发 task_upserted/tool_result/clue_published/assistant_delta(replying) 至少其一；turn_completed 前禁止最后一条实质事件是 understanding 摘要 |
| 10 | guard_passed 等强制事件（Go L557/L725） | 新协议删除该强制；复核失败直接映射 turn_failed（前端撤回正文逻辑现状保留） |

## 11. 产品决策已冻结

本节不再保留阻塞实现的问题。以下决策已经由用户当前消息、Wayfinder 历史讨论和 GitHub 议题共同确认：

1. **understanding 摘要**：有 `tool_calls` 时必发一句超短 `public_summary`；纯 `final_reply` 默认不额外发。
2. **Task List**：采用 Agent 流内嵌；单轮至少 2 个工具/任务，或 Agent 明确多步计划时显示；处理中展开，完成后保留摘要并折叠详情，不常驻侧栏。
3. **ActionCatalog**：题目动态生成实例 + 固定 `ToolKind` 枚举 + Runtime 白名单校验；Agent 不生成按钮，前端只渲染 AllowedAction。
4. **旧 v1 会话**：`LegacyEventAdapter → UnifiedViewModel → 同一套 AgentRun UI`，旧事件只适配展示，不伪装成 v2 Runtime 事实；序号保持稳定。
5. **agent/runtime.go**：本任务仅确认 scenario 链路隔离并加用途注记，不改面试舱和 Community Review，类型化收敛另开任务。
