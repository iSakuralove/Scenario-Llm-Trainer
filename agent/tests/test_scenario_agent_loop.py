from __future__ import annotations

import asyncio
import json
import logging
from pathlib import Path

import pytest

from hiddenworld.agents.scenario_agent import (
    build_scenario_agent_prompt,
    create_scenario_agent,
    create_scenario_agent_runner,
)
from hiddenworld.action_resolver import resolve_user_requested_actions
from hiddenworld.contracts import (
    ActionCatalogEntry,
    AgentSemanticDecision,
    AgentBudgetView,
    AgentContext,
    AgentToolResult,
    AgentOutputEnvelope,
    AuthorizedActionRef,
    ConceptDefinition,
    FinalReplyOutput,
    LearnerStateView,
    PublicScenario,
    ToolCall,
    ToolCallsOutput,
    VirtualTool,
)
from hiddenworld.contracts import AgentTurnRequest
from hiddenworld.scenario_runtime import (
    AgentLoop,
    AgentLoopBudgetExceeded,
    BatchScheduler,
    VirtualObservationExecutor,
    project_agent_context,
)
from hiddenworld.scenario_runtime.turn_runtime import SingleAgentRuntime, _analysis_from_single_agent


def _context(public_scenario: PublicScenario) -> AgentContext:
    return AgentContext(
        public_scenario=public_scenario,
        current_user_message="先看看 CPU",
        learner_summary=LearnerStateView(),
        action_catalog=[
            ActionCatalogEntry(tool_id="inspect:metrics.cpu", kind="metrics", target="数据库 CPU"),
            ActionCatalogEntry(tool_id="inspect:data.explain", kind="database", target="执行计划"),
        ],
        authorized_actions=[
            AuthorizedActionRef(
                authorization_id="auth-1",
                action_ref="inspect:metrics.cpu",
                tool_kind="metrics",
            )
        ],
        budget=AgentBudgetView(remaining_model_rounds=11, remaining_tool_calls=10),
    )


class SequenceAgent:
    def __init__(self, *outputs):
        self.outputs = list(outputs)
        self.contexts = []

    async def run(self, context: AgentContext):
        self.contexts.append(context)
        return self.outputs.pop(0)


class RecordingExecutor:
    def __init__(self):
        self.calls: list[str] = []

    async def execute(self, call: ToolCall, context: AgentContext) -> AgentToolResult:
        self.calls.append(call.tool_id)
        await asyncio.sleep(0)
        return AgentToolResult(
            call_id=call.call_id,
            tool_id=call.tool_id,
            tool_kind="metrics",
            status="succeeded",
            content=f"已查询 {call.tool_id}",
        )


@pytest.mark.asyncio
async def test_agent_loop_can_finish_without_tool_call(public_scenario) -> None:
    agent = SequenceAgent(FinalReplyOutput(kind="final_reply", reply="先从公开现象开始看。"))
    executor = RecordingExecutor()

    result, events = await AgentLoop(agent, executor).run(_context(public_scenario))

    assert result.reply == "先从公开现象开始看。"
    assert [item.kind for item in events] == ["final_reply"]
    assert executor.calls == []


@pytest.mark.asyncio
async def test_agent_loop_emits_structured_round_telemetry(public_scenario, caplog) -> None:
    caplog.set_level(logging.INFO, logger="hiddenworld.agent_loop")
    agent = SequenceAgent(FinalReplyOutput(kind="final_reply", reply="本轮已收束。"))

    await AgentLoop(agent, RecordingExecutor()).run(_context(public_scenario))

    assert "[hiddenworld-agent-round]" in caplog.text
    assert "round=1" in caplog.text
    assert "model_attempt=1" in caplog.text
    assert "tool_call_count=0" in caplog.text


@pytest.mark.asyncio
async def test_agent_loop_executes_authorized_tool_then_returns_reply(public_scenario) -> None:
    agent = SequenceAgent(
        ToolCallsOutput(
            kind="tool_calls",
            public_summary="你想先确认数据库 CPU 是否异常。",
            calls=[ToolCall(call_id="call-1", tool_id="inspect:metrics.cpu")],
        ),
        FinalReplyOutput(kind="final_reply", reply="CPU 目前看起来正常，可以继续查别的方向。"),
    )
    executor = RecordingExecutor()

    result, events = await AgentLoop(agent, executor).run(_context(public_scenario))

    assert result.reply.startswith("CPU")
    assert executor.calls == ["inspect:metrics.cpu"]
    assert [item.kind for item in events] == ["understanding", "tool_result", "final_reply"]
    assert agent.contexts[1].tool_results[0].content == "已查询 inspect:metrics.cpu"
    assert agent.contexts[0].phase == "new_user_turn"
    assert agent.contexts[1].phase == "after_tool_call"
    assert agent.contexts[1].original_user_message == "先看看 CPU"
    assert agent.contexts[1].tool_states["inspect:metrics.cpu"].state == "consumed"
    assert [item.tool_name for item in agent.contexts[1].action_history] == [
        "inspect:metrics.cpu",
        "inspect:metrics.cpu",
    ]


@pytest.mark.asyncio
async def test_agent_loop_does_not_execute_the_same_tool_again_in_a_later_round(public_scenario) -> None:
    repeated = ToolCallsOutput(
        kind="tool_calls",
        public_summary="继续确认同一项。",
        calls=[ToolCall(call_id="same-again", tool_id="inspect:metrics.cpu")],
    )
    agent = SequenceAgent(
        repeated,
        repeated,
        FinalReplyOutput(kind="final_reply", reply="这项观察已经返回，不再重复查询。"),
    )
    executor = RecordingExecutor()

    result, events = await AgentLoop(agent, executor).run(_context(public_scenario))

    assert result.reply.startswith("这项观察")
    assert executor.calls == ["inspect:metrics.cpu"]
    assert sum(
        item.kind == "tool_rejected" and item.payload.error_code == "already_completed"
        for item in events
    ) == 1


@pytest.mark.asyncio
async def test_agent_loop_rejects_unassigned_observation_without_executing(public_scenario) -> None:
    agent = SequenceAgent(
        ToolCallsOutput(
            kind="tool_calls",
            public_summary="我准备查看执行计划。",
            calls=[ToolCall(call_id="call-1", tool_id="inspect:data.explain")],
        ),
        FinalReplyOutput(kind="final_reply", reply="这个检查需要你先明确请求。"),
    )
    executor = RecordingExecutor()

    result, events = await AgentLoop(agent, executor).run(_context(public_scenario))

    assert result.reply.startswith("这个检查")
    assert executor.calls == []
    rejected = [item.payload for item in events if item.kind == "tool_rejected"]
    assert rejected[0].error_code == "user_action_required"


def test_batch_scheduler_defers_dependency_and_deduplicates(public_scenario) -> None:
    context = _context(public_scenario)
    scheduler = BatchScheduler(dependency_map={"inspect:data.explain": {"inspect:metrics.cpu"}})
    plan = scheduler.plan(
        [
            ToolCall(call_id="cpu", tool_id="inspect:metrics.cpu"),
            ToolCall(call_id="explain", tool_id="inspect:data.explain"),
            ToolCall(call_id="cpu-duplicate", tool_id="inspect:metrics.cpu"),
        ],
        action_catalog=context.action_catalog,
        authorized_actions=[
            *context.authorized_actions,
            AuthorizedActionRef(
                authorization_id="auth-2",
                action_ref="inspect:data.explain",
                tool_kind="database",
            ),
        ],
        remaining_tool_calls=10,
    )

    assert [item.call_id for item in plan.accepted] == ["cpu"]
    assert [item.call_id for item in plan.deferred] == ["explain"]
    assert plan.rejected[0].error_code == "duplicate_call"


def test_batch_scheduler_rejects_a_call_completed_in_an_earlier_model_round(public_scenario) -> None:
    context = _context(public_scenario)
    call = ToolCall(call_id="cpu-2", tool_id="inspect:metrics.cpu")
    plan = BatchScheduler().plan(
        [call],
        action_catalog=context.action_catalog,
        authorized_actions=context.authorized_actions,
        remaining_tool_calls=10,
        completed_fingerprints={'{"arguments":{},"tool_id":"inspect:metrics.cpu"}'},
    )

    assert plan.accepted == []
    assert plan.rejected[0].error_code == "already_completed"


@pytest.mark.asyncio
async def test_agent_loop_enforces_model_round_budget(public_scenario) -> None:
    output = ToolCallsOutput(
        kind="tool_calls",
        public_summary="继续检查。",
        calls=[ToolCall(call_id="call-1", tool_id="inspect:metrics.cpu")],
    )
    agent = SequenceAgent(output, output)
    executor = RecordingExecutor()

    with pytest.raises(AgentLoopBudgetExceeded):
        await AgentLoop(agent, executor, max_model_rounds=2).run(_context(public_scenario))


@pytest.mark.asyncio
async def test_agent_loop_caps_logical_tool_calls_at_default_budget(public_scenario) -> None:
    context = _context(public_scenario).model_copy(
        update={
            "authorized_actions": [
                AuthorizedActionRef(
                    authorization_id="auth-1",
                    action_ref="inspect:metrics.cpu",
                    tool_kind="metrics",
                ),
                AuthorizedActionRef(
                    authorization_id="auth-2",
                    action_ref="inspect:data.explain",
                    tool_kind="database",
                ),
            ]
        }
    )
    calls = [
        ToolCall(call_id=f"call-{index}", tool_id="inspect:metrics.cpu" if index % 2 else "inspect:data.explain")
        for index in range(12)
    ]
    agent = SequenceAgent(
        ToolCallsOutput(kind="tool_calls", public_summary="批量检查。", calls=calls),
        FinalReplyOutput(kind="final_reply", reply="检查已收束。"),
    )
    executor = RecordingExecutor()

    result, events = await AgentLoop(agent, executor).run(context)

    assert result.reply == "检查已收束。"
    assert len(executor.calls) == 2
    assert sum(item.kind == "tool_rejected" for item in events) == 10


def test_agent_context_prompt_keeps_current_message_and_excludes_hidden_fields(public_scenario) -> None:
    prompt = build_scenario_agent_prompt(_context(public_scenario))

    assert "先看看 CPU" in prompt
    # 指令段可以在禁令里点名内部字段（防模型反推）；但 AgentContext 投影
    # 段绝不能携带隐藏世界或裁判结果。
    context_payload = prompt.split("AgentContext：", 1)[1]
    assert "hidden_world" not in context_payload
    assert "canonical_answer" not in context_payload
    assert "completion_allowed" not in context_payload


def test_agent_context_prompt_exposes_turn_phase_history_and_tool_state(public_scenario) -> None:
    context = _context(public_scenario).model_copy(
        update={
            "phase": "after_tool_call",
            "turn_id": "turn-1",
            "original_user_message": "先看看 CPU",
            "action_history": [
                {
                    "action": "tool_call",
                    "tool_name": "inspect:metrics.cpu",
                    "decision_summary": "先确认资源指标",
                },
                {
                    "action": "tool_result",
                    "tool_name": "inspect:metrics.cpu",
                    "decision_summary": "工具已返回公开观察",
                    "status": "succeeded",
                },
            ],
            "tool_states": {
                "inspect:metrics.cpu": {
                    "state": "consumed",
                    "reason": "本会话已使用，不可重复调用",
                }
            },
        }
    )
    prompt = build_scenario_agent_prompt(context)
    payload = json.loads(prompt.split("AgentContext：", 1)[1])

    assert payload["phase"] == "after_tool_call"
    assert payload["turn_id"] == "turn-1"
    assert payload["original_user_message"] == "先看看 CPU"
    assert payload["action_history"][1]["tool_name"] == "inspect:metrics.cpu"
    assert payload["tool_states"]["inspect:metrics.cpu"]["state"] == "consumed"


def test_continuation_prompt_omits_long_history_and_keeps_structured_state(public_scenario) -> None:
    context = _context(public_scenario).model_copy(
        update={
            "phase": "after_tool_call",
            "turn_context": _context(public_scenario).turn_context.model_copy(
                update={"phase": "after_tool_call", "continuation": True}
            ),
            "transcript": [{"role": "user", "content": "旧消息"}],
            "conversation_summary": "旧的长摘要",
        }
    )

    prompt = build_scenario_agent_prompt(context)
    payload = json.loads(prompt.split("AgentContext：", 1)[1])

    assert "transcript" not in payload
    assert "conversation_summary" not in payload
    assert payload["turn_context"]["phase"] == "after_tool_call"
    assert "Raw Chain-of-Thought" in prompt


def test_response_brief_marks_only_concepts_explicitly_named_by_student(public_scenario) -> None:
    context = _context(public_scenario).model_copy(
        update={
            "current_user_message": "支付回调是什么？",
            "concept_catalog": [
                ConceptDefinition(
                    concept_id="callback",
                    label="支付回调",
                    summary="支付平台通知业务系统处理结果的请求。",
                    aliases=["回调"],
                ),
                ConceptDefinition(
                    concept_id="gateway",
                    label="Gateway",
                    summary="请求进入后端前的入口层。",
                    aliases=["网关"],
                ),
            ],
        }
    )

    prompt = build_scenario_agent_prompt(context)

    assert '"primary_task": "explain_concept"' in prompt
    assert '"explain_concepts": ["支付回调"]' in prompt
    assert '"known_concepts": []' in prompt


def test_structured_action_model_prompt_is_non_empty_without_fabricating_user_message(public_scenario) -> None:
    from hiddenworld.agents.scenario_agent import _model_prompt, build_scenario_agent_prompt

    context = _context(public_scenario).model_copy(
        update={
            "current_user_message": "",
            "authorized_actions": [
                AuthorizedActionRef(
                    authorization_id="auth-quick",
                    action_ref="inspect:metrics.cpu",
                    tool_kind="metrics",
                )
            ],
        }
    )

    assert _model_prompt(context)
    assert context.current_user_message == ""
    prompt = build_scenario_agent_prompt(context)
    assert '"current_turn_input"' in prompt
    assert '"source": "structured_action"' in prompt
    assert '"current_user_message": ""' not in prompt


@pytest.mark.asyncio
async def test_provider_delta_is_reframed_without_changing_content() -> None:
    from hiddenworld.agents.scenario_agent import _emit_stream_frames

    emitted: list[str] = []

    async def collect(piece: str) -> None:
        emitted.append(piece)

    source = "一段由模型真实返回的长文本，用于验证传输层不会把整段内容一次性倾倒。"
    await _emit_stream_frames(collect, source)

    assert len(emitted) > 1
    assert "".join(emitted) == source


def test_agent_context_rejects_hidden_world_fields(public_scenario) -> None:
    with pytest.raises(ValueError):
        AgentContext(
            public_scenario=public_scenario,
            learner_summary=LearnerStateView(),
            budget=AgentBudgetView(remaining_model_rounds=11, remaining_tool_calls=10),
            hidden_world={"root_cause": "must not enter AgentContext"},
        )


def test_pydantic_scenario_agent_accepts_the_two_output_shapes() -> None:
    agent = create_scenario_agent()

    assert agent is not None


def test_agent_output_envelope_ignores_provider_extra_fields() -> None:
    from hiddenworld.contracts import AgentOutputEnvelope

    output = AgentOutputEnvelope.model_validate(
        {
            "kind": "final_reply",
            "reply": "可以继续判断。",
            "turn_assessment": {},
            "teaching_decision": {},
            "reasoning": "provider metadata",
        }
    )

    assert output.to_contract().reply == "可以继续判断。"


def test_agent_output_envelope_rejects_reply_without_semantic_decision() -> None:
    from pydantic import ValidationError
    from hiddenworld.contracts import AgentOutputEnvelope

    with pytest.raises(ValidationError):
        AgentOutputEnvelope.model_validate({"kind": "final_reply", "reply": "只能回复正文"})


def test_agent_output_envelope_places_reply_before_tool_fields_for_streaming() -> None:
    properties = list(AgentOutputEnvelope.model_json_schema()["properties"])

    assert properties[:2] == ["kind", "reply"]


@pytest.mark.asyncio
async def test_pydantic_scenario_agent_runner_returns_contract_union(public_scenario) -> None:
    import json
    from pydantic_ai.models.test import TestModel

    runner = create_scenario_agent_runner(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "kind": "final_reply",
                    "reply": "ok",
                    "turn_assessment": {},
                    "teaching_decision": {},
                }
            )
        )
    )
    output = await runner.run(_context(public_scenario))

    assert isinstance(output, FinalReplyOutput)
    assert output.reply == "ok"


def test_project_agent_context_is_a_safe_whitelist_projection(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    request = AgentTurnRequest(
        request_id="project-1",
        session_id="session-1",
        state_revision=3,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="先看看 CPU",
    )

    context = project_agent_context(request)
    dumped = context.model_dump(mode="json")
    assert context.current_user_message == "先看看 CPU"
    assert any(item.tool_id == "inspect:metrics.cpu" for item in context.action_catalog)
    assert any(item.action_ref == "inspect:metrics.cpu" for item in context.authorized_actions)
    assert {item.hypothesis_id for item in context.hypothesis_catalog} == {
        "H_INDEX",
        "H_CPU_BOUND",
        "H_POOL",
        "H_CACHE",
        "H_OTHER",
    }
    assert "索引问题" in build_scenario_agent_prompt(context)
    assert "H_OTHER" in build_scenario_agent_prompt(context)
    assert "hidden_world" not in dumped
    assert "canonical_answer" not in dumped
    assert "root_cause" not in dumped


def test_prompt_reasoning_mode_follows_backend_flag(
    hidden_world,
    learner_state,
    public_scenario,
    monkeypatch,
) -> None:
    """后端开关关闭=生产禁令；打开=默认逐步推理，且 JSON 契约边界不变。"""

    request = AgentTurnRequest(
        request_id="cot-flag-1",
        session_id="session-1",
        state_revision=1,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="先看看 CPU",
    )
    context = project_agent_context(request)

    monkeypatch.delenv("HIDDENWORLD_TEST_STREAM_RAW_REASONING", raising=False)
    default_prompt = build_scenario_agent_prompt(context)
    assert "不要输出 reasoning、chain of thought、rationale 或任何额外字段。" in default_prompt
    assert "思考输出已开启" not in default_prompt

    monkeypatch.setenv("HIDDENWORLD_TEST_STREAM_RAW_REASONING", "1")
    enabled_prompt = build_scenario_agent_prompt(context)
    assert "思考输出已开启" in enabled_prompt
    assert "逐步推理" in enabled_prompt
    assert "不要输出 reasoning、chain of thought、rationale 或任何额外字段。" not in enabled_prompt
    # 两种模式都不允许在 JSON 输出里加契约外字段，保证解析稳定。
    assert "不要在输出对象里新增 reasoning 等额外字段。" in enabled_prompt


def test_project_agent_context_hypothesis_catalog_has_no_answer_markers(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    request = AgentTurnRequest(
        request_id="project-hypothesis-catalog",
        session_id="session-1",
        state_revision=3,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="我怀疑是索引问题",
    )

    context = project_agent_context(request)
    prompt = build_scenario_agent_prompt(context)
    assert "accepted_hypotheses" not in prompt
    assert hidden_world.root_cause.description not in prompt
    assert all(not hasattr(item, "is_correct") for item in context.hypothesis_catalog)


def test_project_agent_context_keeps_order_log_alias_for_legacy_session_snapshot(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    action = "inspect:database.order_write"
    world = hidden_world.model_copy(
        deep=True,
        update={
            "virtual_tools": [
                VirtualTool(
                    tool_id="tool.database.orders",
                    kind="database",
                    target="订单库回调写入日志",
                    aliases=["数据库日志"],
                    query_patterns=[],
                    redacted_parameters=[],
                    simulated_output="订单库观察",
                    observation_action=action,
                    evidence_ids=[],
                )
            ],
        },
    )
    request = AgentTurnRequest(
        request_id="legacy-order-log-context",
        session_id="session-1",
        state_revision=1,
        public_scenario=public_scenario,
        hidden_world=world,
        learner_state=learner_state,
        user_message="发订单库日志给我",
    )

    context = project_agent_context(request)

    assert [item.action_ref for item in context.authorized_actions] == [action]


def test_project_agent_context_authorizes_mysql_slow_query_log_request(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    request = AgentTurnRequest(
        request_id="mysql-slow-query-context",
        session_id="session-1",
        state_revision=1,
        public_scenario=public_scenario,
        hidden_world=hidden_world.model_copy(
            deep=True,
            update={
                "virtual_tools": [
                    VirtualTool(
                        tool_id="tool.database.slow_query",
                        kind="database",
                        target="MySQL 慢查询日志",
                        aliases=["数据库日志", "MySQL慢查询日志"],
                        query_patterns=["SHOW ENGINE INNODB STATUS"],
                        redacted_parameters=["time_range"],
                        simulated_output="订单库观察",
                        observation_action="inspect:database.slow_query",
                        evidence_ids=[],
                    )
                ],
            },
        ),
        learner_state=learner_state,
        user_message="MySQL慢查询日志",
    )

    context = project_agent_context(request)

    assert [item.action_ref for item in context.authorized_actions] == ["inspect:database.slow_query"]


def test_fixed_vip_bank_mysql_slow_query_action_round_trips_to_declared_observation() -> None:
    path = Path(__file__).parents[1] / "src" / "hiddenworld" / "bank" / "fixed" / "hw-network-vip-001.json"
    payload = json.loads(path.read_text(encoding="utf-8"))
    request = AgentTurnRequest(
        request_id="fixed-bank-mysql-slow-query",
        session_id="session-1",
        state_revision=1,
        public_scenario=payload["public_scenario"],
        hidden_world=payload["hidden_world"],
        learner_state={"collected_evidence": [], "ruled_out_hypotheses": [], "actions_taken": []},
        user_message="MySQL慢查询日志",
    )

    context = project_agent_context(request)

    assert [item.action_ref for item in context.authorized_actions] == ["inspect:database.slow_query"]


def test_fixed_vip_bank_natural_language_route_diff_request_resolves_unique_action() -> None:
    path = Path(__file__).parents[1] / "src" / "hiddenworld" / "bank" / "fixed" / "hw-network-vip-001.json"
    payload = json.loads(path.read_text(encoding="utf-8"))
    items = [VirtualTool.model_validate(item) for item in payload["hidden_world"]["virtual_tools"]]

    assert resolve_user_requested_actions(
        "那看一下网关切换前后的配置差异。",
        items,
        action_attr="observation_action",
    ) == ["inspect:config.route_diff"]


def test_fixed_vip_bank_natural_language_can_authorize_related_log_pair(
    learner_state,
    public_scenario,
) -> None:
    path = Path(__file__).parents[1] / "src" / "hiddenworld" / "bank" / "fixed" / "hw-network-vip-001.json"
    payload = json.loads(path.read_text(encoding="utf-8"))
    request = AgentTurnRequest(
        request_id="fixed-bank-log-pair",
        session_id="session-1",
        state_revision=1,
        public_scenario=payload["public_scenario"],
        hidden_world=payload["hidden_world"],
        learner_state=learner_state,
        user_message="按同一个 request_id 对比 Gateway 和 Nginx 的完成时间，看看为什么会超时",
    )

    context = project_agent_context(request)

    assert [item.action_ref for item in context.authorized_actions] == [
        "inspect:logs.callback_timeout",
        "inspect:logs.nginx_callback",
    ]


def test_fixed_vip_latency_scope_starts_from_the_deepest_ready_action() -> None:
    path = Path(__file__).parents[1] / "src" / "hiddenworld" / "bank" / "fixed" / "hw-network-vip-001.json"
    payload = json.loads(path.read_text(encoding="utf-8"))
    request = AgentTurnRequest(
        request_id="fixed-bank-pay-b72-scope",
        session_id="session-1",
        state_revision=9,
        public_scenario=payload["public_scenario"],
        hidden_world=payload["hidden_world"],
        learner_state={
            "collected_evidence": ["E_CALLBACK_TIMEOUT", "E_NGINX_LATE_200"],
            "actions_taken": [
                "inspect:logs.callback_timeout",
                "inspect:logs.nginx_callback",
            ],
        },
        user_message="看看 pay_b72 为什么这么慢",
    )

    context = project_agent_context(request)

    assert context.investigation_scope is not None
    assert context.investigation_scope.subject_id == "pay_b72"
    assert context.investigation_scope.entry_action_ids == ["inspect:logs.service_callback"]
    assert context.investigation_scope.allowed_action_ids == [
        "inspect:logs.service_callback",
        "inspect:database.lock_wait",
    ]
    assert context.investigation_scope.max_depth == 1
    assert context.investigation_scope.max_tool_calls == 2
    assert [item.action_ref for item in context.authorized_actions] == [
        "inspect:logs.service_callback",
    ]
    assert [item.tool_id for item in context.available_tools] == [
        "inspect:logs.service_callback",
    ]


@pytest.mark.asyncio
async def test_fixed_vip_latency_scope_runs_service_then_lock_then_stops() -> None:
    path = Path(__file__).parents[1] / "src" / "hiddenworld" / "bank" / "fixed" / "hw-network-vip-001.json"
    payload = json.loads(path.read_text(encoding="utf-8"))

    class PayB72Runner:
        def __init__(self) -> None:
            self.calls = 0
            self.contexts: list[AgentContext] = []

        async def run(self, context: AgentContext):
            self.calls += 1
            self.contexts.append(context)
            if self.calls == 1:
                assert context.investigation_scope is not None
                assert [item.tool_id for item in context.available_tools] == [
                    "inspect:logs.service_callback",
                ]
                return ToolCallsOutput(
                    kind="tool_calls",
                    public_summary="先拆开 pay_b72 的回调服务耗时。",
                    calls=[ToolCall(call_id="pay-b72-service", tool_id="inspect:logs.service_callback")],
                )
            if self.calls == 2:
                assert context.turn_context.phase == "after_tool_call"
                assert [item.tool_id for item in context.current_turn_observations] == [
                    "inspect:logs.service_callback",
                ]
                assert [item.action_ref for item in context.authorized_actions] == [
                    "inspect:database.lock_wait",
                ]
                return ToolCallsOutput(
                    kind="tool_calls",
                    public_summary="数据库阶段占主要耗时，继续看锁等待。",
                    calls=[ToolCall(call_id="pay-b72-lock", tool_id="inspect:database.lock_wait")],
                )
            assert self.calls == 3
            assert context.turn_context.phase == "after_tool_call"
            assert [item.tool_id for item in context.current_turn_observations] == [
                "inspect:logs.service_callback",
                "inspect:database.lock_wait",
            ]
            return FinalReplyOutput(
                kind="final_reply",
                reply="pay_b72 的主要耗时在数据库阶段，其中锁等待约 3.31 秒。",
            )

    request = AgentTurnRequest(
        request_id="fixed-bank-pay-b72-runtime",
        session_id="session-1",
        state_revision=9,
        public_scenario=payload["public_scenario"],
        hidden_world=payload["hidden_world"],
        learner_state={
            "collected_evidence": ["E_CALLBACK_TIMEOUT", "E_NGINX_LATE_200"],
            "actions_taken": [
                "inspect:logs.callback_timeout",
                "inspect:logs.nginx_callback",
            ],
        },
        user_message="看看 pay_b72 为什么这么慢",
    )
    runner = PayB72Runner()

    result = await SingleAgentRuntime(runner).run_turn(request)

    assert runner.calls == 3
    assert result.reply.startswith("pay_b72")
    successful = [
        item.tool_name
        for item in result.public_trace
        if item.kind == "agent_tool_result" and item.observation is not None
    ]
    assert successful == ["inspect:logs.service_callback", "inspect:database.lock_wait"]


def test_database_status_question_does_not_resolve_as_observation_request() -> None:
    path = Path(__file__).parents[1] / "src" / "hiddenworld" / "bank" / "fixed" / "hw-network-vip-001.json"
    payload = json.loads(path.read_text(encoding="utf-8"))
    items = [VirtualTool.model_validate(item) for item in payload["hidden_world"]["virtual_tools"]]

    assert resolve_user_requested_actions(
        "数据库有显示什么异常？",
        items,
        action_attr="observation_action",
    ) == []


def test_project_agent_context_does_not_turn_database_status_question_into_observation_request(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    request = AgentTurnRequest(
        request_id="database-status-question-context",
        session_id="session-1",
        state_revision=1,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="数据库有显示什么异常？",
    )

    context = project_agent_context(request)

    assert context.authorized_actions == []


def test_single_agent_uses_structured_semantic_decision_instead_of_keyword_reconstruction(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    request = AgentTurnRequest(
        request_id="semantic-decision-1",
        session_id="session-1",
        state_revision=1,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="我认为是数据库问题，但先解释上一句",
    )
    output = FinalReplyOutput(
        kind="final_reply",
        reply="这里的连接是指请求到数据库之间的连接。",
        semantic=AgentSemanticDecision(
            intent="clarification",
            clarification_target="上一句的连接两端",
            made_claim=True,
            contains_answer_attempt=False,
            confidence=0.93,
        ),
    )

    analysis = _analysis_from_single_agent(request, output, [], [])

    assert analysis.intent == "clarification"
    assert analysis.clarification_target == "上一句的连接两端"
    assert analysis.contains_answer_attempt is False
    assert analysis.confidence == 0.93


def test_single_agent_normalizes_undeclared_hypothesis_id(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    request = AgentTurnRequest(
        request_id="invalid-hypothesis-normalization",
        session_id="session-1",
        state_revision=1,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="我怀疑是一个题目没有列出的组件问题",
    )
    output = FinalReplyOutput(
        kind="final_reply",
        reply="先把这条观察和已知现象分开。",
        semantic=AgentSemanticDecision(
            intent="hypothesis",
            hypothesis_id="H_MODEL_INVENTED",
            hypothesis_raw="一个题目没有列出的组件问题",
            claim_type="hypothesis",
            made_claim=True,
            confidence=0.9,
        ),
    )

    analysis = _analysis_from_single_agent(request, output, [], [])

    assert analysis.hypothesis_id == "H_OTHER"
    assert analysis.hypothesis_raw == "一个题目没有列出的组件问题"


def test_single_agent_does_not_silently_drop_hypothesis_when_model_omits_id(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    request = AgentTurnRequest(
        request_id="missing-hypothesis-id-normalization",
        session_id="session-1",
        state_revision=1,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="我猜超时可能与网关切换后的后端池变化有关。",
    )
    output = FinalReplyOutput(
        kind="final_reply",
        reply="这个方向可以先保留下来继续理解。",
        semantic=AgentSemanticDecision(
            intent="hypothesis",
            hypothesis_id="",
            hypothesis_raw="超时可能与网关切换后的后端池变化有关",
            claim_type="hypothesis",
            made_claim=True,
            confidence=0.8,
        ),
    )

    analysis = _analysis_from_single_agent(request, output, [], [])

    assert analysis.hypothesis_id == "H_OTHER"
    assert analysis.hypothesis_raw == "超时可能与网关切换后的后端池变化有关"


def test_single_agent_does_not_turn_observation_claim_into_other_hypothesis(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    request = AgentTurnRequest(
        request_id="observation-claim-no-hypothesis",
        session_id="session-1",
        state_revision=1,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="我看到网关日志里的失败请求都停在 3 秒左右。",
    )
    output = FinalReplyOutput(
        kind="final_reply",
        reply="这是你从公开日志中读到的一个现象。",
        semantic=AgentSemanticDecision(
            intent="investigate",
            claim_type="observation",
            made_claim=True,
            established_facts=["失败请求都停在 3 秒左右"],
            confidence=0.9,
        ),
    )

    analysis = _analysis_from_single_agent(request, output, [], [])

    assert analysis.hypothesis_id == ""
    assert analysis.hypothesis_raw == ""


def test_single_agent_repairs_contradictory_claim_semantics(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    """claim_type 表明有主张而 made_claim=false 时，以枚举为准回填布尔。

    Go 侧 validateScenarioAssessmentConsistency 会把这个组合判成整轮失败；
    模型对"学生隐含方向但未明确断言"的输入会系统性输出该组合。
    """

    request = AgentTurnRequest(
        request_id="claim-semantic-repair",
        session_id="session-1",
        state_revision=1,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="订单服务前面是Nginx网关服务，所以我想看看NGINX网关服务日志，健康度怎么样",
    )
    output = FinalReplyOutput(
        kind="final_reply",
        reply="这个请求目前没有对应的公开题面证据。",
        semantic=AgentSemanticDecision(
            intent="probe_plan",
            claim_type="hypothesis",
            made_claim=False,
            confidence=0.8,
        ),
    )

    analysis = _analysis_from_single_agent(request, output, [], [])

    assert analysis.made_claim is True


def test_single_agent_repairs_contradictory_answer_attempt(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    """answer_attempt 布尔与文本矛盾时，以是否存在非空文本为准。"""

    request = AgentTurnRequest(
        request_id="answer-attempt-repair",
        session_id="session-1",
        state_revision=1,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="我感觉就是网关超时配置改短了。",
    )
    output = FinalReplyOutput(
        kind="final_reply",
        reply="这个方向先记录为你的当前假设。",
        semantic=AgentSemanticDecision(
            intent="answer_attempt",
            contains_answer_attempt=False,
            answer_attempt_text="网关超时配置改短了",
            confidence=0.7,
        ),
    )

    analysis = _analysis_from_single_agent(request, output, [], [])

    assert analysis.contains_answer_attempt is True

    empty_text_output = FinalReplyOutput(
        kind="final_reply",
        reply="这个方向先记录为你的当前假设。",
        semantic=AgentSemanticDecision(
            intent="answer_attempt",
            contains_answer_attempt=True,
            answer_attempt_text="  ",
            confidence=0.7,
        ),
    )
    empty_analysis = _analysis_from_single_agent(request, empty_text_output, [], [])
    assert empty_analysis.contains_answer_attempt is False


@pytest.mark.asyncio
async def test_single_agent_does_not_emit_undeclared_hypothesis_proposal(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    class InvalidHypothesisRunner:
        async def run(self, context):
            return FinalReplyOutput(
                kind="final_reply",
                reply="我听到你提出了一个额外的假设。",
                semantic=AgentSemanticDecision(
                    intent="hypothesis",
                    hypothesis_id="H_MODEL_INVENTED",
                    hypothesis_raw="一个题目没有列出的组件问题",
                    claim_type="hypothesis",
                    made_claim=True,
                    confidence=0.9,
                ),
            )

    request = AgentTurnRequest(
        request_id="invalid-hypothesis-proposal",
        session_id="session-1",
        state_revision=1,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="我怀疑是一个题目没有列出的组件问题",
    )

    result = await SingleAgentRuntime(InvalidHypothesisRunner()).run_turn(request)

    assert result.turn_analysis.hypothesis_id == "H_OTHER"
    assert any(
        item.kind == "set_current_hypothesis" and item.hypothesis_id == "H_OTHER"
        for item in result.proposals
    )


@pytest.mark.asyncio
async def test_projected_context_and_virtual_executor_complete_authorized_observation(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    request = AgentTurnRequest(
        request_id="project-loop-1",
        session_id="session-1",
        state_revision=3,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="先看看 CPU",
    )
    context = project_agent_context(request)
    executor = VirtualObservationExecutor(request)
    result = await executor.execute(
        ToolCall(call_id="cpu", tool_id="inspect:metrics.cpu"),
        context,
    )

    assert result.status == "succeeded"
    assert "CPU" in result.content


@pytest.mark.asyncio
async def test_single_agent_runtime_keeps_transport_contract_with_one_model_runner(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    class FinalOnlyRunner:
        calls = 0

        async def run(self, context):
            self.calls += 1
            return FinalReplyOutput(kind="final_reply", reply="先根据公开现象继续判断。")

    runner = FinalOnlyRunner()
    runtime = SingleAgentRuntime(runner)
    request = AgentTurnRequest(
        request_id="single-runtime-1",
        session_id="session-1",
        state_revision=3,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="先根据现象判断",
    )

    result = await runtime.run_turn(request)

    assert runner.calls == 1
    assert result.reply.startswith("先根据公开")
    assert result.internal_audit.reason_codes[0] == "single_agent_runtime"
    assert result.expected_revision == 3


@pytest.mark.asyncio
async def test_single_agent_runtime_retries_reply_after_public_boundary_rejection(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    class ToolThenReplyRunner:
        def __init__(self):
            self.calls = 0

        async def run(self, context):
            self.calls += 1
            if self.calls == 1:
                return ToolCallsOutput(
                    kind="tool_calls",
                    public_summary="先确认数据库 CPU 是否异常。",
                    calls=[ToolCall(call_id="cpu", tool_id="inspect:metrics.cpu")],
                )
            assert context.tool_results[0].status == "succeeded"
            if self.calls == 2:
                return FinalReplyOutput(kind="final_reply", reply="CPU 观察结果已返回，可以继续排除其他方向。")
            return FinalReplyOutput(kind="final_reply", reply="这条观察已经提供了一个事实，你怎么理解它？")

    runner = ToolThenReplyRunner()
    runtime = SingleAgentRuntime(runner)
    request = AgentTurnRequest(
        request_id="single-runtime-tool-1",
        session_id="session-1",
        state_revision=3,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="先看看 CPU",
    )

    result = await runtime.run_turn(request)

    assert runner.calls == 3
    # Guard 拒绝明确排除路径后，Runtime 让同一个 Agent 重生成，而不是替换成固定话术。
    assert "继续排除" not in result.reply
    assert result.reply == "这条观察已经提供了一个事实，你怎么理解它？"
    # 循环内旁路先发 agent_tool_result(含 observation 负载)，收尾不再重复；
    # 断言观察以任一形态进入公开 trace。
    assert any(
        item.kind == "observation_result"
        or (item.kind == "agent_tool_result" and item.observation is not None)
        for item in result.public_trace
    )


@pytest.mark.asyncio
async def test_single_agent_runtime_forwards_streaming_reply_deltas(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    class StreamingRunner:
        async def run(self, context):
            return FinalReplyOutput(kind="final_reply", reply="完整回复")

        async def run_stream(self, context, *, on_reply_delta):
            await on_reply_delta("完整")
            await on_reply_delta("回复")
            return FinalReplyOutput(kind="final_reply", reply="完整回复")

    deltas: list[str] = []
    async def collect_delta(text: str) -> None:
        deltas.append(text)

    runtime = SingleAgentRuntime(StreamingRunner())
    request = AgentTurnRequest(
        request_id="single-runtime-stream-1",
        session_id="session-1",
        state_revision=3,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="继续判断",
    )

    result = await runtime.run_turn(request, on_reply_delta=collect_delta)

    assert result.reply == "完整回复"
    assert deltas == ["完整", "回复"]


@pytest.mark.asyncio
async def test_quick_action_is_planned_by_agent_before_runtime_executes_tool(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    class CountingRunner:
        def __init__(self):
            self.calls = 0
            self.seen_tool_results = []
            self.order = []

        async def run(self, context):
            self.calls += 1
            self.seen_tool_results = list(context.tool_results)
            self.order.append("model")
            if self.calls == 1:
                return ToolCallsOutput(
                    kind="tool_calls",
                    public_summary="执行学生点击的观察。",
                    calls=[ToolCall(call_id="quick-tool", tool_id=action)],
                )
            assert len(context.tool_results) == 1
            assert context.tool_results[0].status == "succeeded"
            return FinalReplyOutput(kind="final_reply", reply="模型已读取快捷检查结果。")

    action = "inspect:metrics.cpu"
    runner = CountingRunner()
    runtime = SingleAgentRuntime(runner)
    request = AgentTurnRequest(
        request_id="quick-action-single-call",
        session_id="session-1",
        state_revision=3,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="",
        structured_user_action={
            "action_id": action,
            "catalog_version": "catalog-test",
            "state_revision": 3,
            "normalized_scope": action,
        },
    )

    async def on_trace(event) -> None:
        if event.kind in {"agent_tool_started", "agent_tool_result"}:
            runner.order.append("tool")

    result = await runtime.run_turn(request, on_public_trace=on_trace)

    assert runner.calls == 2
    assert runner.order.index("model") < runner.order.index("tool")
    assert runner.seen_tool_results[0].status == "succeeded"
    assert result.reply == "模型已读取快捷检查结果。"
    assert result.turn_assessment is not None
    assert result.turn_assessment.requested_action == action
    assert result.turn_assessment.requested_action_raw == action
    # 循环内旁路先发 agent_tool_result(含 observation 负载)，收尾不再重复；
    # 断言观察以任一形态进入公开 trace。
    assert any(
        item.kind == "observation_result"
        or (item.kind == "agent_tool_result" and item.observation is not None)
        for item in result.public_trace
    )


@pytest.mark.asyncio
async def test_quick_action_retries_when_first_model_round_returns_reply_without_tool_call(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    """模型首轮误收束时只允许受控重试，Runtime 不得直接替它执行动作。"""

    action = "inspect:metrics.cpu"

    class RetryRunner:
        def __init__(self) -> None:
            self.calls = 0
            self.tool_results_seen: list[int] = []
            self.order: list[str] = []

        async def run(self, context):
            return await self.run_stream(context)

        async def run_stream(self, context, *, on_reply_delta=None, on_reasoning_delta=None):
            del on_reasoning_delta
            self.calls += 1
            self.tool_results_seen.append(len(context.tool_results))
            self.order.append("model")
            if self.calls == 1:
                if on_reply_delta is not None:
                    await on_reply_delta("首轮误输出")
                return FinalReplyOutput(kind="final_reply", reply="首轮误输出")
            if self.calls == 2:
                return ToolCallsOutput(
                    kind="tool_calls",
                    public_summary="按快捷动作检查 CPU。",
                    calls=[ToolCall(call_id="quick-retry", tool_id=action)],
                )
            assert context.tool_results[0].status == "succeeded"
            if on_reply_delta is not None:
                await on_reply_delta("重试后已读取 CPU。")
            return FinalReplyOutput(kind="final_reply", reply="重试后已读取 CPU。")

    runner = RetryRunner()
    runtime = SingleAgentRuntime(runner)
    request = AgentTurnRequest(
        request_id="quick-action-retry",
        session_id="session-1",
        state_revision=3,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="",
        structured_user_action={
            "action_id": action,
            "catalog_version": "catalog-test",
            "state_revision": 3,
            "normalized_scope": action,
        },
    )

    streamed_reply: list[str] = []

    async def collect_reply(text: str) -> None:
        streamed_reply.append(text)

    result = await runtime.run_turn(request, on_reply_delta=collect_reply)

    assert runner.calls == 3
    assert runner.tool_results_seen == [0, 0, 1]
    assert result.reply == "重试后已读取 CPU。"
    assert streamed_reply == ["重试后已读取 CPU。"]


@pytest.mark.asyncio
async def test_runtime_streams_tool_activity_during_the_loop(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    """循环内工具活动实时外发：开始→带观察的结果→收尾摘要，序号连续不重复。"""

    class ToolThenReplyRunner:
        async def run(self, context):
            if not context.tool_results:
                return ToolCallsOutput(
                    kind="tool_calls",
                    public_summary="你想先确认数据库 CPU。",
                    calls=[ToolCall(call_id="call-1", tool_id="inspect:metrics.cpu")],
                )
            return FinalReplyOutput(kind="final_reply", reply="CPU 没有异常，可以换个方向看。")

    streamed: list[PublicTraceEvent] = []

    async def on_trace(event) -> None:
        streamed.append(event)

    runtime = SingleAgentRuntime(ToolThenReplyRunner())
    request = AgentTurnRequest(
        request_id="loop-stream-1",
        session_id="session-1",
        state_revision=3,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="先看看 CPU",
    )

    result = await runtime.run_turn(request, on_public_trace=on_trace)

    kinds = [item.kind for item in streamed]
    assert kinds.index("agent_tool_started") < kinds.index("agent_tool_result")
    tool_started_event = next(item for item in streamed if item.kind == "agent_tool_started")
    assert tool_started_event.call_id == "call-1"
    assert tool_started_event.round == 1
    tool_result_event = next(item for item in streamed if item.kind == "agent_tool_result")
    assert tool_result_event.call_id == "call-1"
    assert tool_result_event.round == 1
    assert tool_result_event.observation is not None
    assert tool_result_event.observation.action == "inspect:metrics.cpu"
    # 序号严格递增：循环旁路 1..k，收尾事件接着续编。
    sequences = [item.sequence for item in streamed]
    assert sequences == sorted(sequences)
    assert len(set(sequences)) == len(sequences)
    # 已实时外发的观察不再以 observation_result 重复出现。
    assert not any(
        item.kind == "observation_result"
        and item.observation is not None
        and item.observation.action == "inspect:metrics.cpu"
        for item in streamed
    )
    # 落库 trace 完整包含旁路事件，且与流式顺序一致。
    assert result.public_trace[: len(streamed) - 0][0].kind == "agent_tool_started"
    assert [item.sequence for item in result.public_trace] == list(
        range(1, len(result.public_trace) + 1)
    )
