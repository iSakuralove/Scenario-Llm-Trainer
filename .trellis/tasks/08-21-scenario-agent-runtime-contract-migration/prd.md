# PRD：排查工坊单 Agent + 工具调用 Runtime 重构

## 1. 目标和用户价值

把排查工坊从「Interpreter + Mentor 双 LLM 节点 + 阶段化 trace 协议」重构为「单 ScenarioAgent（理解/规划/决定工具调用）+ 确定性 Runtime（执行/权限/预算/状态归约/事件发布/持久化）」架构，并同步迁移 Go 复核层与前端展示层。

用户价值：

- 学生看到的是**真实的动态过程**：理解摘要后紧跟真实工具调用、线索释放或回复，不再出现固定步骤动画和内部机器汇报（"导师回复已完成私有缓冲""回复已通过安全校验"）。
- 界面不再出现"排查导师"等产品术语文案，只有头像和内容。
- 工具调用有明确的图标与状态；多任务场景有动态 Task List。
- 答案不可探测：CanonicalAnswer 与 Agent 上下文物理隔离，anti-guessing 边界从"提示词约束"升级为"类型与数据流约束"。
- 协议可长期维护：RunEvent 严格判别联合，sequence / state_revision / schema_version 三者职责分离。

## 2. 已确认事实（不再重复询问 Q1-Q8，全部为最终决策）

以下十一条为用户确认的最终架构约束，任何后续设计不得违背：

1. **边界**：只有一个 ScenarioAgent；不拆 Interpreter/Mentor/TeachingPolicy Agent。Agent 负责理解、规划、推荐下一步、追问和生成回复；Runtime 负责确定性执行、权限、预算、状态归约、事件发布和持久化。Agent 的 `tool_calls` 不是学生授权本身，不能替学生自主排查。前端不显示"排查导师"、Mentor、Agent 等身份文字，只保留头像；删除"排查导师"产品术语；用户可见文案短、自然，不用固定模板。
2. **上下文隔离**：ScenarioContract 只属于 Runtime；Agent 只接收 AgentContext；ScenarioContract 不得进入 AgentContext。CanonicalAnswer 与题目同源、同版本、同快照持久化，但与 Agent 上下文物理隔离。CanonicalAnswer、InternalComparison、答案原文、精确缺失项、相似度信息不得进入 ScenarioAgentContext。Agent 只能看到题目生成且经 Runtime 白名单过滤的教学维度，以及只读的 `turn_control.terminal` 生命周期信号。当前轮 `user_message` 必须作为 AgentContext 的独立输入传入；不能只进入解析/授权路径，也不能假设它已经追加到历史 `transcript`。
3. **状态与事件契约**：sequence（事件排序/去重/断线恢复）、state_revision（业务状态版本/并发控制）、schema_version（协议版本）严格区分。每个正式 `RunEvent` 外层都必须携带 `state_revision`；同一状态快照产生的连续事件可以复用同一 revision，只有 Runtime 成功提交业务状态时才提升。`RunEvent.payload` 必须是严格判别联合，禁止 Dict[str, Any]；外层统一但 payload 按事件类型严格建模。正式事件包括：turn_started / task_upserted / tool_result / clue_published / assistant_delta / turn_completed / turn_failed。不扩展 reasoning_summary、mentor_buffered 等内部阶段事件为用户流程。
4. **Agent Loop 与工具调用**：模型输出只能是 tool_calls 或 final_reply。单轮最多 10 次逻辑业务工具调用、最多 11 次模型轮次。同一批 tool_calls 只能包含互相无数据依赖的调用；有依赖的进入下一轮；无依赖只读调用可并行；有依赖调用串行。Observation Tool 执行必须匹配 Runtime 生成的 `UserActionAuthorization`，授权来源只能是当前 `user_message` 明确表达的检查意图，或 `StructuredUserAction/QuickAction` 点击；Agent 不能创建、扩大或伪造授权。`compare_answer`、StateReducer、ClueGate 等 Runtime 内部能力不受该用户动作授权约束。工具失败、超时、重复、预算耗尽由 Runtime 确定性处理。工具调用必须有明确 UI 图标和状态；多工具/多任务支持动态 Task List。不允许伪造工具调用或固定延时制造"思考感"。
5. **semantic_data 权限**：semantic_data 继续区分 Agent-visible 与 Runtime-only；Runtime-only 数据不能通过 AgentContext、工具结果、RunEvent、PublicContent 或前端类型泄露。
6. **推理展示**：正式模式不公开模型原始 reasoning 或 CoT。生产 `RunEvent` 不包含调试事件；`DebugTraceEvent` 是独立调试协议，统一使用受控 `debug_trace_*` 命名，只在非生产、显式开启的调试配置下输出；调试 trace 不写入业务历史或正式审计。前端用 Thinking State / Thinking Reasoning 风格展示动态状态，但不得写死"理解中→查询中→正在整理线索→回复中"；Agent 根据实际事件动态显示状态。只有模型提供安全 reasoning stream 且配置开启时才允许调试输出，否则正常流式输出。
7. **线索与 PublicContent**：内部对象继续分离 WorldObservation / TeachingClue / AgentReply / ToolResult；对外统一 PublicContent{content_type, markdown_ready}。前端只渲染 markdown_ready；日志类工具结果直接显示日志内容；指标/健康度/配置用简短 Tool return 风格；主动释放的信息显示为"线索"。不在开头暴露"模拟订单库写入日志""模拟回调访问日志"等实现术语；不向前端返回 hidden_world、内部根因、内部 ID、发布策略或答案提示。
8. **CanonicalAnswer 与答案比较**：数据流 CanonicalAnswer → InternalComparison → StateReducer → TurnControl；WorldObservations / DiagnosticRelations / ClueGraph / GuidancePolicy → GuidanceState → ScenarioAgent。ScenarioContract 明确持久化独立的 `CanonicalAnswer`，至少包含 `canonical_conclusion`、`required_evidence_ids`、`required_causal_relations`、`accepted_equivalents`、`solution_requirements`，并绑定 `answer_version`。compare_answer 只能比较服务端绑定的真实 AnswerAttempt；Agent 不得自由构造 answer 或反复探测；不接受用户答案正文作为工具参数；迁移为**无参数调用**，由 Runtime 自动绑定当前轮 AnswerAttempt。InternalComparison 仅 Runtime 可见；AgentComparison 只返回 conclusion_status / evidence_status / causal_status / missing_dimensions / contradictions，不包含 terminal；terminal 属于 TurnControl / SessionState，Agent 只通过 `AgentContext.turn_control.terminal` 观察。不得返回 CanonicalAnswer 原文；missing_dimensions 不得动态提取标准答案缺失点；completion_allowed / completion_ready 属于 Runtime + StateReducer。
9. **QuickActions**：AllowedAction 由 Runtime 根据题目动态 `ActionCatalog`、固定 `ToolKind`、GuidanceState、当前状态和预算生成，并经 Runtime 白名单校验；Agent 不直接生成按钮；前端只渲染结构化动作；点击产生带授权的 `StructuredUserAction`，与自然语言工具调用共用 10 次逻辑调用预算、幂等机制、state_revision；QuickAction 与自由输入并存；按钮只表达抽象下一步，不泄露隐藏答案关键词。
10. **保留项**：现有意图识别已较准确，不为重构引入关键词意图分类器；现有"学生问数据库是否有异常，并询问日志显示的内容"式理解摘要保留，但摘要后必须继续显示真实动态状态、工具调用、线索释放或回复，不能停住。
11. **迁移冲突清单**（详见 design.md §10）：runtime.py 双节点、world.py simulated_output、agentRun.ts 旧事件、AgentRun.tsx 固定文案与"排查导师"标签、前端 hidden_world 类型、Go 旧 stage 与通用 ToolResult、compare_answer 参数化。
12. **已冻结的交互投影**：安全摘要的内部语义沿用 `public_summary`，V2 正式事件统一投影为 `assistant_delta(phase=understanding)`，不再使用 reasoning 命名；Thinking State 只表示本轮仍在处理，不代表固定步骤；Task List 内嵌在 Agent 流中，处理中展开、完成后保留任务摘要并折叠详情；Runtime 返回稳定 `tool_kind`，前端固定映射图标；日志直接显示，指标/健康度/配置显示为简短 Tool return，主动释放内容单独显示为线索。
13. **已冻结的预算规则**：所有 Agent 业务工具（查询、线索、答案比较等）共用每轮 10 次逻辑调用预算；相同工具、规范化参数和同一题目状态的重复调用合并；批次中的每个独立查询分别计数；用户明确请求但超预算的任务进入 `pending` 且不自动执行，Agent 自主超额调用直接拒绝；必要的答案核验不能被普通查询静默挤掉；无依赖只读调用按拓扑层并行。
14. **已冻结的模型输出契约**：`AgentModelOutput` 只有 `kind=tool_calls` 或 `kind=final_reply` 两种判别分支，二者互斥；`tool_calls` 分支必须包含一句超短 `public_summary` 和 `calls[]`，`final_reply` 分支可带 `public_summary` 和 `reply`。纯 `final_reply` 默认不额外发送摘要。Runtime 收到完整结构后再投影为 `assistant_delta(phase=understanding)`，不额外运行摘要模型。
15. **已冻结的兼容策略**：旧 v1 事件通过 `LegacyEventAdapter` 转换为统一 ViewModel，继续使用同一套 AgentRun UI；适配只改变展示模型，不把旧事件伪装成新的 Runtime 事实。正式事件序列一旦确定并持久化，`sequence` 在断线重放或重建 SSE 连接时不得重新编号。
16. **已冻结的范围边界**：`agent/runtime.go` 的通用 ToolResult 仅做 scenario 链路隔离和用途注记，本任务不改面试舱与 Community Review；类型化收敛另开后续任务。
17. **已冻结的用户动作授权边界**：Observation Tool 不能因为 Agent 认为“有帮助”就执行；Runtime 必须把每次观察调用绑定到当前轮的 `UserActionAuthorization`。没有匹配授权的调用只能产生 `task_upserted(state=rejected, error_code=user_action_required)`，不得读取新的场景事实。
18. **已冻结的教学维度契约**：`TeachingDimension` 由题目生成时定义安全维度实例，并携带 Runtime 固定允许的通用 category；`waiting_time`、`dependency_latency`、`capacity_saturation`、`causal_chain` 只是现有示例映射，不是全产品永久枚举。`AgentComparison.missing_dimensions` 与 `TeachingNavigation` 使用同一维度引用模型，不能再出现四维导航与第五个独立 `verification` 值不一致。

## 3. 功能要求

### F1 Python Runtime 重构（agent/src/hiddenworld）
- F1.1 新增 ScenarioAgentLoop：单 Agent 多轮循环，模型输出为严格判别的 `AgentModelOutput(tool_calls | final_reply)`；tool_calls 必须携带超短 public_summary，最多 11 轮模型调用、10 次逻辑工具调用，不另跑摘要模型。
- F1.2 ToolCall 批次调度器：批内无依赖只读调用并行执行；有依赖调用由 Runtime 强制串行（跨轮）；Runtime 持有依赖校验权。
- F1.3 AgentContext（新契约）：由 Runtime 从 ScenarioContract 投影生成，只含当前 `user_message`、历史 transcript、Agent-visible 数据、题目安全教学维度引用、当前授权动作的安全投影和抽象 turn_control；不暴露答案或未授权事实。结构化动作轮的自然语言字段为空，动作通过同一授权投影进入。
- F1.4 语义工具集（logs/metrics/config/database/dependency 查询 + compare_answer + 提交结论）：Observation Tool 只能在 UserActionAuthorization 匹配后于题目虚拟数据上确定性执行；compare_answer 无参数化，Runtime 自动绑定 AnswerAttempt。
- F1.5 状态归约 StateReducer + TurnControl：吸收现有 EvidenceEngine/AntiGuess/RootCauseVerifier 的状态推进逻辑，产出 GuidanceState 与 proposals。
- F1.6 新事件发布器：产出严格判别联合 payload 的新 RunEvent 序列；tool_calls 分支的 public_summary 投影为 assistant 可见前置摘要事件，纯 final_reply 默认不额外发摘要；删除 mentor_buffered/guard_passed/response_summary 等内部阶段事件。
- F1.7 独立 DebugTraceEvent：仅非生产配置显式开启时通过独立调试通道输出；不进入正式 RunEvent 联合，不写业务历史/正式审计。
- F1.8 保留 _resolve_declared_virtual_tool 的确定性映射与现有意图理解能力；授权判定复用语义帧/动作解析结果，不引入新的关键词意图分类器。
- F1.9 新增 `ScenarioContractValidator`：题目生成和加载时校验 CanonicalAnswer 的唯一性、证据/因果引用、RootCause/DiagnosticRelations/SolutionRubric 一致性和 accepted_equivalents 的同根因约束；不通过则拒绝入库或进入人工审核。

### F2 Go 迁移（backend/internal）
- F2.1 agentclient 契约升级：新事件类型、AgentModelOutput、ToolCallState/ToolResult 分离结构、schema_version/state_revision 字段；终值仍 DisallowUnknownFields。
- F2.2 scenario_agent.go 校验重写：phaseByKind 阶段机替换为判别联合校验；删除"恰好一条 guard_passed"强制；校验 task_upserted 承载 ToolCallState、tool_result 只承载执行终态；compare_answer 无参数校验。
- F2.3 RunEvent 持久化与 SSE 下发改用新结构；每个事件写入 state_revision；Go 是 public sequence 唯一生成者，Python 只产生内部 event index；schema_version 写入事件流并做兼容路由，断线重放复用已持久化 sequence。
- F2.4 通用 agent/runtime.go ToolResult 仅做 scenario 链路隔离和用途注记；类型化收敛另开后续任务，不改面试舱与 Community Review。
- F2.5 "排查导师"文案全部替换（scenario_agent.go 6 处 + handlers_scenarios.go 5 处）。
- F2.6 AllowedAction 生成器（ActionCatalog + GuidanceState + 预算）与 StructuredUserAction 入口（与自然语言共用预算/幂等/state_revision）。

### F3 前端迁移（frontend/src）
- F3.1 agentRun.ts 类型重写为新判别联合；删除 hidden_world 暴露面；增加 LegacyEventAdapter → UnifiedViewModel，旧 v1 与新 v2 共用 AgentRun UI。
- F3.2 AgentRun.tsx：删除"排查导师"aria-label；thinkingLabel 固定三段式改为事件驱动动态状态；工具行图标+状态保留并适配多工具；仅在单轮至少 2 个工具/任务或明确多步计划时显示内嵌 Task List（task_upserted）。
- F3.3 PublicContent 渲染：content_type 只分发 observation→日志/工具结果风格、clue→线索卡；assistant 回复只走独立 `assistant_delta` 回复流，不再存在 `PublicContent.content_type=assistant` 的第二条渲染路径。
- F3.4 QuickActions 渲染与 StructuredUserAction 提交。
- F3.5 断线恢复：沿用 request_id + sequence 去重；after_sequence 语义不变。

## 4. 安全边界

- S1 ScenarioContract（含 CanonicalAnswer/RootCause/未释放 EvidenceNode content/发布策略）不得进入 AgentContext：以类型构造 + 单测双保险（渲染 Agent prompt 断言无禁止实体）。
- S2 AgentComparison 白名单：conclusion_status / evidence_status / causal_status / missing_dimensions / contradictions；terminal 只属于 TurnControl / SessionState，并通过 AgentContext.turn_control.terminal 只读暴露；missing_dimensions 必须引用与 TeachingNavigation 相同的安全教学维度，不得从标准答案动态提取。
- S3 UserActionAuthorization：没有当前用户明确检查意图或 StructuredUserAction 授权时，Observation Tool 必须被 Runtime 拒绝；Agent 不得通过预算内调用绕过该边界。
- S4 compare_answer 无参数 + Runtime 绑定：Agent 无法构造候选答案探测；幂等（同一 attempt 只比较一次）；低置信轮不触发比较。
- S5 debug_trace_* 生产禁用：配置门控 + 测试断言生产模式事件流无 debug_trace。
- S6 PublicContent 白名单：不返回 hidden_world、内部根因、内部 ID、发布策略、答案提示；实现术语（"模拟订单库写入日志"等）不出现在开头可见文案；assistant 回复不通过 PublicContent 分支传输。
- S7 CanonicalAnswer 强一致性：ScenarioContractValidator 在生成和加载时拒绝空结论、缺失引用、根因/因果/评分规则不一致、第二个完整答案或无法确认同根因的 accepted_equivalents。
- S8 Go 对 Python 的不信任原则保持：所有事件、提议、工具结果仍经 Go 独立复核后才能下发/落库；Go 独占 public sequence。
- S9 10 次逻辑调用硬预算由 Runtime 强制；QuickAction 与自然语言共用，防止通过按钮绕过预算。

## 5. 跨层迁移范围

| 层 | 主要文件 |
|---|---|
| Python | agent/src/hiddenworld/runtime.py、contracts/（events/world/deps/answer/turn/transport、authorization/dimensions/validator 新增与改写）、agents/（tools.py 重写、interpreter.py+mentor.py 合并删除）、kernel/（cluegate/evidence/verifier/antiguess/policy 吸收进 StateReducer 或保留复用）、app.py（服务入口） |
| Go | backend/internal/agentclient/（types.go/client.go/testdata golden）、backend/internal/httpapi/（scenario_agent.go、handlers_scenarios.go、sse 链路、集成测试）、backend/internal/agent/runtime.go、backend/internal/domain/（scenario_agent.go、scenario_content.go） |
| 前端 | frontend/src/types/agentRun.ts、types/index.ts（hidden_world）、features/scenarios/agentrun/（AgentRun.tsx、ThinkingState.tsx、ThinkingReasoning.tsx、新增 TaskList/QuickActions）、api/client.ts |
| 数据 | scenario_messages/scenario_agent_turns 中已落库 public_trace 旧形状的读取兼容（schema_version 路由） |
| Spec | .trellis/spec/backend/scenario-agent-contracts.md 全篇同步更新 |

## 6. 可测试验收标准

- A1 Python：pytest（agent/tests）全绿；新增：AgentContext 构造测试断言不含 ScenarioContract 且保留当前 `user_message`；最终 ScenarioAgent 能读取澄清/解释原话；Observation Tool 无授权拒绝、有 user_message/QuickAction 授权成功；TeachingDimension 映射一致；ScenarioContractValidator 唯一性/引用/一致性测试；AgentModelOutput、DebugTraceEvent、RunEvent 判别联合测试。
- A2 Go：go test ./... 全绿；agentclient golden 重生成后过严格解码；新事件校验矩阵测试（未知 kind 拒绝、payload 错型拒绝、每个事件必有 state_revision、ToolCallState/ToolResult 分层、Go 唯一生成 public sequence 且重放稳定）；schema_version 兼容路由测试（旧 v1 经 LegacyEventAdapter、新 v2、混合拒绝）。
- A3 前端：pnpm lint / type-check / test 全绿；UI 测试断言渲染输出不含"排查导师"/"Mentor"/"Agent"文字；LegacyEventAdapter 与 v2 事件生成同一 UnifiedViewModel；assistant 回复只有 assistant_delta 路径；thinking 状态由事件驱动；Task List 阈值与 QuickActions 渲染/交互测试。
- A4 端到端：SSE 全链路（Go 复核后下发的新事件序列前端可渲染）；答案尝试轮 AgentComparison 信号正确；QuickAction 点击走 StructuredUserAction 且计入同一预算。
- A5 安全：AgentContext 不含 ScenarioContract 的安全测试（prompt 渲染断言）；PublicContent 无 hidden_world/根因/内部 ID 断言。

验收命令见 implement.md §测试与验收命令。

## 7. 明确不做的事项

- 不拆分 Interpreter Agent / Mentor Agent / TeachingPolicy Agent（反向拆分禁止）。
- 不引入新的关键词意图分类器；不重写已准确的意图识别。
- 不删除/替换现有理解摘要能力（"学生问了什么"式复述保留）。
- 不把 reasoning_summary / mentor_buffered / guard_passed 等内部阶段事件扩展为用户流程事件；安全摘要使用 `public_summary_*`，调试专用通道使用受控 `debug_trace_*`。
- 不在正式模式公开模型原始 reasoning 或 CoT；不保留 debug_reasoning_*。
- 不让 Agent 自由构造 answer 参数或生成按钮。
- 不执行真实 SQL/Shell/HTTP；虚拟工具语义不变。
- 本轮不做历史会话数据的批量回填重写（只做读取兼容）。
- 不动面试舱/题库/案例工坊等其他模块。

## 8. 开放问题

无阻塞产品决策。understanding 摘要条件、Task List 阈值、动态 ActionCatalog、LegacyEventAdapter 兼容策略和 `agent/runtime.go` scenario 隔离范围均已由用户当前消息、Wayfinder 历史讨论及 GitHub 议题冻结。
