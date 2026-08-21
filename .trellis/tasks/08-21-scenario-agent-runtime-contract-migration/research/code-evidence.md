# 代码证据核对（2026-08-21）

本文件记录规划阶段逐文件核对的结果，作为 prd/design/implement 的事实依据。所有行号以当前 main 分支为准。

## 1. Python：agent/src/hiddenworld/runtime.py（784 行）

- **确认双节点结构**：`HiddenWorldRuntime` dataclass 字段 `interpreter: Any` + `mentor: Any`（L83-84）。`_run_turn` 主链：interpreter(LLM) → `_resolve_declared_virtual_tool`（确定性工具映射）→ ClueGate/EvidenceEngine/RootCauseVerifier/AntiGuess（确定性内核）→ TeachingPolicy → mentor(LLM) → Guard → proposals。
- **旧事件序列**：`reasoning_summary_delta/completed → observation_result → tool_started/result/completed（仅 compare_answer）→ response_summary → mentor_buffered → guard_passed → proposal_approved(Go 补) → reply_delta → turn_completed`。
- `_public_trace_before_mentor`（L645）与 `_public_trace_after_mentor`（L734）产出旧 trace；`mentor_buffered`/`guard_passed` 是内部阶段事件外发。
- `_StreamSequencer`：实时通道序号与落库序号分离（L62-76）。
- `STALL_UNLOCK_THRESHOLD = 2`（L58），Go 侧必须同步。
- 意图理解摘要：`TurnAnalysis.public_summary` 由模型产出（turn.py L38-45 声明在第一位，驱动流式），非固定文案——**保留**。
- `_resolve_declared_virtual_tool`（L480-534）：确定性 aliases/query_patterns 映射 + 只读 SQL/外部命令白名单判断——**保留，不引入新的关键词意图分类器**。
- `compare_answer` 调用（L230-252）：Python 自建 `AnswerAttempt(answer_attempt_id=f"{request_id}:answer")` 后由 Runtime 主动执行——事实上已是 Runtime 驱动，只是工具签名仍带 `answer_attempt_id` 参数。

## 2. Python：contracts/events.py（134 行）

- `RunEventKind` 14 种字面量（L36-51）；`RunEvent` 是**外层统一 + Optional 字段**结构（reasoning/observation/tool 全 Optional，L101-117），不是判别联合。
- `PublicTraceEvent`（L120-134）落库条目，同源。
- `ReasoningStage` 固定四阶段：understanding_message / checking_observations / verifying_answer / composing_reply（L57-62）。

## 3. Python：contracts/world.py（208 行）

- `VirtualTool.simulated_output`（L105）：**旧字段仍在**，描述"该工具在题目虚拟数据集上的模拟输出"。固定题库 JSON（如 hw-network-vip-001.json L67-70）仍携带该字段。
- `HiddenWorld`（L187-208）：root_cause/hypotheses/evidence_graph/observations/virtual_tools/solution_rubric/misconception_rules——Runtime-only 数据源。
- `PublicScenario`（L173-184）：学生可见题面。

## 4. Python：contracts/answer.py（117 行）

- `PublicAnswerComparison`（L33-53）：tool/status/user_points/support_status/next_action，**字段白名单类型层面固定**。
- `InternalAnswerComparison`（L56-101）：claim_alignment/evidence_coverage/missing_evidence/completion_allowed 等，仅内部审计；`to_public()` 是唯一投影通道。
- 现有 support_status 四值（insufficiently_specific/needs_more_evidence/has_evidence_conflict/evidence_consistent）→ 新架构要求改为 AgentComparison 抽象教学决策信号（conclusion_status/evidence_status/causal_status/missing_dimensions/contradictions/terminal）。

## 5. Python：contracts/deps.py（92 行）

- `InterpreterDeps`：public_scenario + hypotheses（无正确性标记）+ transcript + known_actions + virtual_tools。
- `MentorDeps`：public_scenario/transcript/learner_state/teaching constraints/released_evidence(原文)/answer_comparison(公开投影) + `guard_only`（GuardContext，instructions 绝不读取）。
- 测试 `test_mentor_deps_field_boundary` 把字段集合断言成白名单（spec Scenario 5）。

## 6. Python：agents/tools.py（85 行）

- `compare_answer(ctx, answer_attempt_id: str)`（L69-75）：**仍接收 answer_attempt_id 参数**；`CompareAnswerRuntime.execute` 校验 attempt 绑定当前轮（session_id/turn_id/revision 三绑定，L61-66），幂等（同实例最多比较一次）。
- Go 侧硬校验 `redacted_arguments["answer_attempt_id"] == requestID+":answer"`（scenario_agent.go L514、L742）——无参数化迁移需两侧同改。

## 7. Python：contracts/transport.py（142 行）

- `ProposalKind` 10 种类型化提议（L16-31）；`ContractVersionMismatch`；契约版本常量 `CONTRACT_VERSION`（version.py，= hiddenworld.v1）。
- Go 不信任 Python：类型化提议逐条审批。

## 8. Go：backend/internal/agent/runtime.go（279 行）

- **通用 ToolResult**：`type ToolResult struct { Summary string; Metadata map[string]string }`（L18-21）——非类型化。
- **旧 stage 机制**：`emitStage(onStage func(step, message string), ...)`（L275-279）。
- `Runtime.Execute` 顺序跑 Step 列表，记录 AgentTrace（domain.AgentStep）。

## 9. Go：backend/internal/httpapi/scenario_agent.go（1139 行）

- `phaseByKind` 旧阶段机两份（批量 L422-432 / 流式 L647-657）：reasoning→observation→tool→response_summary→mentor_buffered→guard_passed 强制顺序。
- 强制每轮**恰好一条 guard_passed**（L557-559、L725-730）——新协议删除该要求。
- toolStage 状态机（L734-768）只支持 compare_answer 三段生命周期；`shouldCompareAnswer` 门控。
- `validateScenarioReply`（L357）实体禁词复核；`extractScenarioSensitiveTokens` + `scenarioIsDistinctiveNumber`（与 Python guard 同步，spec Scenario 1）。
- `approveScenarioProposals`（L202-355）：release_evidence / release_evidence_on_stall / record_action / … 类型化审批，lowConfidence 闸门。
- `buildScenarioRunEvents`（L954-1011）：Go 侧补 proposal_approved、切 reply_delta、补 turn_completed。
- "排查导师" 文案 6 处（L169-183）。

## 10. Go：backend/internal/agentclient/types.go（159 行）

- `TurnRequest` 把 `HiddenWorld domain.HiddenWorld` **整包传给 Python**（L13）——现状是 Python Runtime 拿全量；新架构语义改为"ScenarioContract 只属于 Runtime"，HTTP 契约不变但 Python 内部 Agent 只收 AgentContext。
- `TurnAnalysis` 全字段镜像（L55-77），`client.go` 终值 `DisallowUnknownFields()`。
- `InternalAnswerComparison` 全字段（L137-149）仅审计。
- `state_revision` 已存在（TurnRequest L11、scenario_sessions 表 schema.go L45）。
- **schema_version 尚不存在**：目前由 contract_version=hiddenworld.v1 一并承担协议版本职责。

## 11. Go：backend/internal/domain/scenario_agent.go / scenario_content.go

- `ScenarioRunEvent` 通用结构（Kind string + Optional 载荷，L49-60）。
- `HiddenWorldContractVersion = "hiddenworld.v1"`；`WithHiddenWorldCompatibility` 旧投影（root_cause/key_evidence/reveal_strategy 兼容旧 Go 运行时）。

## 12. 前端：frontend/src/types/agentRun.ts（56 行）

- 旧 14 种 kind 联合（L3-17）；`ScenarioPublicReasoningSummary` 固定四阶段（L19-22）；`ScenarioToolEventPayload.result` 只有 compare_answer 形状（L38-43）。

## 13. 前端：frontend/src/features/scenarios/agentrun/AgentRun.tsx（250 行）

- **"排查导师" aria-label**（L50）。
- **固定 thinking 文案**（thinkingLabel L218-222）："正在校验本轮答案表述 / 正在整理导师回复 / 正在理解本轮排查意图"——违反"不允许固定步骤顺序"。
- `dedupeEvents` 按 request_id+sequence 去重（L145-149）——断线恢复语义已有。
- 注释（L224-229）确认 guard_passed 等事件"不能从协议里删"的原因是 Go 强制——新协议删除该强制后前端可真正收敛。

## 14. 前端：frontend/src/types/index.ts

- `hidden_world?: HiddenWorld`（L98）挂在 `ScenarioContent` 上，注释说明"社区结构化预览仍使用；排查工坊 public projection 不返回"。

## 15. 前端：frontend/src/api/client.ts

- `requestScenarioMessageStream`（L389+）：POST /scenarios/sessions/{id}/messages，Accept: text/event-stream，payload 带 after_sequence 断线恢复。

## 16. QuickActions / AllowedAction / ActionCatalog / StructuredUserAction

- 仅存在于 CONTEXT.md 术语表（L99-105），**代码零实现**。全新增。

## 17. 抽象教学导航（waiting_time / dependency_latency / capacity_saturation / causal_chain）

- 现有代码无此枚举；现有最接近物是 `validScenarioFocus`（logs/metrics/config/change/dependency/data/resource，scenario_agent.go L1122-1129）与 EvidenceCategory。新增 GuidanceState 字段。

## 18. 测试布局

- Python：agent/tests/（test_runtime.py、test_contracts.py、test_tools.py、test_kernel_*.py 等 18 个文件）。
- Go：backend/internal/httpapi/scenario_agent_integration_test.go、scenario_validation_mode_test.go、agentclient/client_test.go、testdata/turn_result_golden.json。
- 前端：frontend/src（vitest）。
- spec：.trellis/spec/backend/scenario-agent-contracts.md（6 个 Scenario + 2 个 Common Mistake，迁移时需同步更新）。
