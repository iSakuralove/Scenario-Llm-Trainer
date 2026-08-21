from __future__ import annotations

import asyncio
import json
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
async def test_agent_loop_caps_logical_tool_calls_at_ten(public_scenario) -> None:
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
    assert "hidden_world" not in prompt
    assert "canonical_answer" not in prompt
    assert "completion_allowed" not in prompt


def test_structured_action_model_prompt_is_non_empty_without_fabricating_user_message(public_scenario) -> None:
    from hiddenworld.agents.scenario_agent import _model_prompt

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
        {"kind": "final_reply", "reply": "可以继续判断。", "reasoning": "provider metadata"}
    )

    assert output.to_contract().reply == "可以继续判断。"


def test_agent_output_envelope_places_reply_before_tool_fields_for_streaming() -> None:
    properties = list(AgentOutputEnvelope.model_json_schema()["properties"])

    assert properties[:2] == ["kind", "reply"]


@pytest.mark.asyncio
async def test_pydantic_scenario_agent_runner_returns_contract_union(public_scenario) -> None:
    import json
    from pydantic_ai.models.test import TestModel

    runner = create_scenario_agent_runner(
        TestModel(custom_output_text=json.dumps({"kind": "final_reply", "reply": "ok"}))
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
    assert "hidden_world" not in dumped
    assert "canonical_answer" not in dumped
    assert "root_cause" not in dumped


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

    assert analysis.hypothesis_id == ""
    assert analysis.hypothesis_raw == "一个题目没有列出的组件问题"


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

    assert result.turn_analysis.hypothesis_id == ""
    assert all(item.kind != "set_current_hypothesis" for item in result.proposals)


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
    assert any(item.kind == "observation_result" for item in result.public_trace)


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
async def test_quick_action_executes_locally_before_one_final_agent_call(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    class CountingRunner:
        def __init__(self):
            self.calls = 0
            self.seen_tool_results = []

        async def run(self, context):
            self.calls += 1
            self.seen_tool_results = list(context.tool_results)
            return FinalReplyOutput(kind="final_reply", reply="快捷检查结果已经返回。")

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

    result = await runtime.run_turn(request)

    assert runner.calls == 1
    assert len(runner.seen_tool_results) == 1
    assert runner.seen_tool_results[0].status == "succeeded"
    assert result.turn_assessment is not None
    assert result.turn_assessment.requested_action == action
    assert result.turn_assessment.requested_action_raw == action
    assert any(item.kind == "observation_result" for item in result.public_trace)
