import asyncio
import json
from types import SimpleNamespace

import pytest
from pydantic_ai.models.test import TestModel

from hiddenworld.agents.interpreter import create_interpreter_agent
from hiddenworld.agents.mentor import create_mentor_agent
from hiddenworld.contracts import AgentTurnRequest, MentorAction, Observation, TurnAnalysis, VirtualTool
from hiddenworld.runtime import (
    HiddenWorldRuntime,
    TurnDeadlineExceeded,
    _normalize_mentor_reply,
    _resolve_declared_virtual_tool,
)


def _analysis_for_runtime_test(**updates) -> TurnAnalysis:
    values = {
        "public_summary": "你想查看公开日志。",
        "intent": "investigate",
        "requested_action_raw": "",
        "clarification_target": "",
        "action_match_status": "none",
        "actions": [],
        "hypothesis_id": "",
        "hypothesis_raw": "",
        "made_claim": False,
        "contains_answer_attempt": False,
        "answer_attempt_text": "",
        "established_facts": [],
        "is_stuck": False,
        "is_noise": False,
        "student_affect": "engaged",
        "confidence": 0.95,
    }
    values.update(updates)
    return TurnAnalysis(**values)


def test_runtime_matches_explicit_order_log_alias_to_declared_virtual_tool(
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
                    # 模拟已经创建的旧 session：没有本次新增的正式别名。
                    aliases=["数据库日志"],
                    query_patterns=[],
                    redacted_parameters=["time_range"],
                    simulated_output="返回订单库写入观察。",
                    observation_action=action,
                    evidence_ids=[],
                )
            ],
            "observations": [
                *hidden_world.observations,
                Observation(
                    action=action,
                    result="订单库回调写入观察已返回。",
                    is_negative=False,
                    yields_evidence=[],
                ),
            ],
        },
    )
    request = AgentTurnRequest(
        request_id="runtime-order-log-alias",
        session_id="session-1",
        state_revision=1,
        public_scenario=public_scenario,
        hidden_world=world,
        learner_state=learner_state,
        user_message="发订单库日志给我",
    )

    result = _resolve_declared_virtual_tool(
        request,
        _analysis_for_runtime_test(),
    )

    assert result.action_match_status == "matched"
    assert result.actions == [action]


def test_runtime_keeps_model_guidance_after_public_observation(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    action = hidden_world.observations[0].action
    request = AgentTurnRequest(
        request_id="runtime-observation-boundary",
        session_id="session-1",
        state_revision=1,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="查看回调访问日志",
    )
    analysis = _analysis_for_runtime_test(
        requested_action_raw="查看回调访问日志",
        action_match_status="matched",
        actions=[action],
    )
    observations = [hidden_world.observations[0]]
    reply = (
        "根据刚才查询的回调访问日志，问题已经比较清晰：zone-b 出现了大量 HTTP 401 和连接超时，"
        "建议下一步检查网关 VIP 的发布记录。"
    )

    normalized = _normalize_mentor_reply(request, analysis, observations, reply)

    assert "问题已经比较清晰" in normalized
    assert "建议下一步检查网关 VIP 的发布记录" in normalized


@pytest.mark.asyncio
async def test_runtime_turn_returns_typed_proposals_without_mutating_input_state(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    interpreter = create_interpreter_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "public_summary": "你说你完全不知道从哪下手，希望先拿到一点方向。",
                    "actions": ["inspect:metrics.cpu"],
                    "hypothesis_id": "H_CPU_BOUND",
                    "hypothesis_raw": "",
                    "made_claim": False,
                    "contains_answer_attempt": False,
                    "answer_attempt_text": "",
                    "established_facts": [],
                    "is_stuck": False,
                    "is_noise": False,
                    "student_affect": "engaged",
                    "confidence": 0.95,
                },
                ensure_ascii=False,
            )
        )
    )
    mentor = create_mentor_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "reply": "CPU 和内存都在正常区间，这条观察排除了什么方向？",
                    "rationale": "让学生使用刚获得的阴性观察继续推理。",
                    "requested_releases": [],
                    "confirms_hypothesis": False,
                    "expected_effort": "quick",
                },
                ensure_ascii=False,
            )
        )
    )
    request = AgentTurnRequest(
        request_id="request-1",
        session_id="session-1",
        state_revision=7,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="先看看数据库 CPU",
    )

    result = await HiddenWorldRuntime(interpreter=interpreter, mentor=mentor).run_turn(request)

    assert learner_state.collected_evidence == []
    assert result.request_id == "request-1"
    assert result.expected_revision == 7
    assert result.turn_analysis.actions == ["inspect:metrics.cpu"]
    assert result.internal_verification.relation == "ruled_out"
    assert result.internal_verification.ruled_out_this_turn == ["H_CPU_BOUND"]
    proposal_pairs = {
        (item.kind, item.evidence_id or item.hypothesis_id or item.action) for item in result.proposals
    }
    assert ("release_evidence", "E_CPU_NORMAL") in proposal_pairs
    assert ("rule_out_hypothesis", "H_CPU_BOUND") in proposal_pairs
    assert ("record_action", "inspect:metrics.cpu") in proposal_pairs
    assert result.reply.startswith("CPU 和内存")
    assert all(item.tool_name != "compare_answer" for item in result.public_trace)


@pytest.mark.asyncio
async def test_runtime_executes_compare_answer_only_for_answer_attempt(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    learner_state.collected_evidence = ["E_RELEASE_LOG", "E_DDL_DIFF"]
    answer = (
        "根因是发布脚本重建 orders 表时漏掉了 idx_user_created 索引；"
        "我会重建 idx_user_created 索引，补充上线前的索引校验，"
        "重建后复跑 EXPLAIN 确认走索引，并观察 P99 回落。"
    )
    interpreter = create_interpreter_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "public_summary": "你说你完全不知道从哪下手，希望先拿到一点方向。",
                    "actions": [],
                    "hypothesis_id": "H_INDEX",
                    "hypothesis_raw": "",
                    "made_claim": True,
                    "contains_answer_attempt": True,
                    "answer_attempt_text": answer,
                    "established_facts": [],
                    "is_stuck": False,
                    "is_noise": False,
                    "student_affect": "engaged",
                    "confidence": 0.98,
                },
                ensure_ascii=False,
            )
        )
    )
    mentor = create_mentor_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "reply": "证据链已经闭合，可以进入复盘。",
                    "rationale": "学生给出的结论已有完整观察支持。",
                    "requested_releases": [],
                    "confirms_hypothesis": True,
                    "expected_effort": "quick",
                },
                ensure_ascii=False,
            )
        )
    )
    request = AgentTurnRequest(
        request_id="request-answer",
        session_id="session-1",
        state_revision=8,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message=answer,
    )

    result = await HiddenWorldRuntime(interpreter=interpreter, mentor=mentor).run_turn(request)

    comparison = result.internal_verification.answer_comparison
    assert comparison is not None
    assert comparison.answer_attempt_id == "request-answer:answer"
    assert comparison.completion_allowed is True
    assert [item.kind for item in result.public_trace if item.tool_name] == [
        "tool_started",
        "tool_result",
        "tool_completed",
    ]
    tool_result = next(item for item in result.public_trace if item.kind == "tool_result")
    assert tool_result.tool is not None
    assert tool_result.tool.redacted_arguments == {}
    public_tool_json = tool_result.tool.model_dump_json()
    for forbidden in ["correct", "target", "claim_alignment", "missing_evidence", "completion_allowed"]:
        assert forbidden not in public_tool_json


@pytest.mark.asyncio
async def test_runtime_rejects_agent_observation_without_user_action_authorization(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    interpreter = create_interpreter_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "public_summary": "我判断这轮可以查一下 CPU。",
                    "actions": ["inspect:metrics.cpu"],
                    "hypothesis_id": "",
                    "hypothesis_raw": "",
                    "made_claim": False,
                    "contains_answer_attempt": False,
                    "answer_attempt_text": "",
                    "established_facts": [],
                    "is_stuck": False,
                    "is_noise": False,
                    "student_affect": "engaged",
                    "confidence": 0.95,
                },
                ensure_ascii=False,
            )
        )
    )
    mentor = _plain_mentor()
    request = AgentTurnRequest(
        request_id="request-agent-only-observation",
        session_id="session-1",
        state_revision=12,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="我先说说自己的判断，不查任何东西",
    )

    result = await HiddenWorldRuntime(interpreter=interpreter, mentor=mentor).run_turn(request)

    assert result.public_trace
    assert all(item.observation is None for item in result.public_trace)
    assert all(item.action != "inspect:metrics.cpu" for item in result.proposals)


@pytest.mark.asyncio
async def test_quickaction_turn_executes_authorized_observation_without_user_text(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    """QuickAction 点击轮没有自然语言；结构化动作本身就是授权，必须能触发观察。"""

    interpreter = create_interpreter_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "public_summary": "学生通过按钮发起了一次检查。",
                    "actions": [],
                    "hypothesis_id": "",
                    "hypothesis_raw": "",
                    "made_claim": False,
                    "contains_answer_attempt": False,
                    "answer_attempt_text": "",
                    "established_facts": [],
                    "is_stuck": False,
                    "is_noise": False,
                    "student_affect": "engaged",
                    "confidence": 0.95,
                },
                ensure_ascii=False,
            )
        )
    )
    request = AgentTurnRequest(
        request_id="request-quickaction",
        session_id="session-1",
        state_revision=12,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="",
        structured_user_action={
            "action_id": "inspect:metrics.cpu",
            "catalog_version": "catalog-1",
            "state_revision": 12,
        },
    )

    result = await HiddenWorldRuntime(interpreter=interpreter, mentor=_plain_mentor()).run_turn(request)

    assert result.turn_analysis.actions == ["inspect:metrics.cpu"]
    assert any(item.observation is not None for item in result.public_trace)
    proposal_pairs = {
        (item.kind, item.evidence_id or item.action) for item in result.proposals
    }
    assert ("release_evidence", "E_CPU_NORMAL") in proposal_pairs
    assert ("record_action", "inspect:metrics.cpu") in proposal_pairs


@pytest.mark.asyncio
async def test_quickaction_rejects_an_action_outside_the_runtime_catalog(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    """结构化动作也必须经过 Runtime 准入，不能凭 action_id 读取世界。"""

    request = AgentTurnRequest(
        request_id="request-quickaction-invalid",
        session_id="session-1",
        state_revision=12,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="",
        structured_user_action={
            "action_id": "inspect:metrics.not_declared",
            "catalog_version": "catalog-1",
            "state_revision": 12,
        },
    )

    result = await HiddenWorldRuntime(
        interpreter=_stuck_interpreter(is_stuck=False),
        mentor=_plain_mentor(),
    ).run_turn(request)

    assert result.turn_analysis.actions == []
    assert all(item.kind != "record_action" for item in result.proposals)
    assert all(item.observation is None for item in result.public_trace)


@pytest.mark.asyncio
async def test_runtime_streams_analysis_and_public_trace_before_returning_result(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    answer = "我认为目前的证据还需要继续验证索引问题。"
    interpreter = create_interpreter_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "public_summary": "你说你完全不知道从哪下手，希望先拿到一点方向。",
                    "actions": [],
                    "hypothesis_id": "H_INDEX",
                    "hypothesis_raw": "",
                    "made_claim": True,
                    "contains_answer_attempt": True,
                    "answer_attempt_text": answer,
                    "established_facts": [],
                    "is_stuck": False,
                    "is_noise": False,
                    "student_affect": "engaged",
                    "confidence": 0.98,
                },
                ensure_ascii=False,
            )
        )
    )
    mentor = create_mentor_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "reply": "继续补充能支撑这个结论的直接观察。",
                    "rationale": "证据还不足，只给公开下一步。",
                    "requested_releases": [],
                    "confirms_hypothesis": False,
                    "expected_effort": "quick",
                },
                ensure_ascii=False,
            )
        )
    )
    request = AgentTurnRequest(
        request_id="request-stream-events",
        session_id="session-1",
        state_revision=11,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message=answer,
    )
    observed: list[tuple[str, str]] = []

    async def on_analysis(analysis) -> None:
        observed.append(("analysis", str(analysis.contains_answer_attempt)))

    async def on_trace(event) -> None:
        observed.append(("trace", event.kind))

    result = await HiddenWorldRuntime(interpreter=interpreter, mentor=mentor).run_turn(
        request,
        on_turn_analysis=on_analysis,
        on_public_trace=on_trace,
    )

    streamed = [value for kind, value in observed if kind == "trace"]
    # 推理增量在 interpreter 流式生成 public_summary 期间就外发，因此合法地
    # 早于 turn_analysis 到达——学生看到的第一个字来自本轮真实解析，不是固定动画。
    assert streamed[0] == "reasoning_summary_delta"
    assert observed.index(("analysis", "True")) < observed.index(
        ("trace", "reasoning_summary_completed")
    )
    # 增量只走实时通道，不落库：一条摘要能拆出几十个增量，落库会撞穿 Go 侧
    # 64 条 public trace 上限。落库的是同一段文本的 completed 事件。
    persisted = [item.kind for item in result.public_trace]
    assert "reasoning_summary_delta" not in persisted
    assert [kind for kind in streamed if kind != "reasoning_summary_delta"] == persisted
    assert observed.index(("trace", "tool_started")) < observed.index(("trace", "mentor_buffered"))
    assert observed[-1] == ("trace", "guard_passed")


@pytest.mark.asyncio
async def test_runtime_enforces_total_turn_deadline(hidden_world, learner_state, public_scenario) -> None:
    class SlowInterpreter:
        async def run(self, *_args, **_kwargs):
            await asyncio.sleep(0.05)

    request = AgentTurnRequest(
        request_id="request-timeout",
        session_id="session-1",
        state_revision=9,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="先看看 CPU",
        budget={"deadline_ms": 1, "max_releases": 3},
    )

    with pytest.raises(TurnDeadlineExceeded):
        await HiddenWorldRuntime(interpreter=SlowInterpreter(), mentor=object()).run_turn(request)


@pytest.mark.asyncio
async def test_low_confidence_answer_signal_does_not_call_tool_or_write_hypothesis(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    answer = "我猜可能是索引问题。"
    interpreter = create_interpreter_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "public_summary": "你说你完全不知道从哪下手，希望先拿到一点方向。",
                    "actions": ["inspect:data.explain"],
                    "hypothesis_id": "H_INDEX",
                    "hypothesis_raw": "",
                    "made_claim": True,
                    "contains_answer_attempt": True,
                    "answer_attempt_text": answer,
                    "established_facts": [],
                    "is_stuck": False,
                    "is_noise": False,
                    "student_affect": "confused",
                    "confidence": 0.2,
                },
                ensure_ascii=False,
            )
        )
    )
    mentor = create_mentor_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "reply": "你可以先说明想验证哪条可观察现象。",
                    "rationale": "解析置信度较低，只做澄清。",
                    "requested_releases": [],
                    "confirms_hypothesis": False,
                    "expected_effort": "quick",
                },
                ensure_ascii=False,
            )
        )
    )
    request = AgentTurnRequest(
        request_id="request-low-confidence",
        session_id="session-1",
        state_revision=10,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message=answer,
    )

    result = await HiddenWorldRuntime(interpreter=interpreter, mentor=mentor).run_turn(request)

    assert result.internal_verification.answer_comparison is None
    assert all(item.tool_name != "compare_answer" for item in result.public_trace)
    assert all(item.kind != "set_current_hypothesis" for item in result.proposals)
    assert all(item.kind != "record_action" for item in result.proposals)


def _stuck_interpreter(*, is_stuck: bool = True, summary: str | None = None):
    return create_interpreter_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "public_summary": summary or "你说你完全不知道从哪下手，想先要一点方向。",
                    "actions": [],
                    "hypothesis_id": "",
                    "hypothesis_raw": "",
                    "made_claim": False,
                    "contains_answer_attempt": False,
                    "answer_attempt_text": "",
                    "established_facts": [],
                    "is_stuck": is_stuck,
                    "is_noise": False,
                    "student_affect": "frustrated",
                    "confidence": 0.3,
                },
                ensure_ascii=False,
            )
        )
    )


def _plain_mentor():
    return create_mentor_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "reply": "我们先找一个能看的地方，从这里开始。",
                    "rationale": "他连续几轮没有方向，先把起点定下来。",
                    "requested_releases": [],
                    "confirms_hypothesis": False,
                    "expected_effort": "quick",
                },
                ensure_ascii=False,
            )
        )
    )


@pytest.mark.asyncio
async def test_stuck_student_does_not_release_evidence_without_a_declared_hint_ladder(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    """卡住时逐级增加 Hint，不把系统提示伪装成学生获得的证据。"""
    learner_state.stalled_turns = 2
    request = AgentTurnRequest(
        request_id="request-stalled",
        session_id="session-1",
        state_revision=3,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="能不能给我点信息，我什么都不知道啊，我是菜鸟",
    )

    result = await HiddenWorldRuntime(
        interpreter=_stuck_interpreter(), mentor=_plain_mentor()
    ).run_turn(request)

    # 提示不是学生独立发现的证据，不应生成任何 release_evidence 提议。
    assert all(item.kind not in {"release_evidence", "release_evidence_on_stall"} for item in result.proposals)
    # 卡住时仍然累加 stalled_turns，且不计入有效推进。
    assert all(item.kind != "advance_effective_turn" for item in result.proposals)
    stalled = next(item for item in result.proposals if item.kind == "set_stalled_turns")
    assert stalled.value == 3
    # 该最小测试世界没有声明 HintStep，Runtime 不能凭空编造提示文本或证据。
    assert all(item.kind not in {"set_hint_level", "set_last_hint"} for item in result.proposals)


@pytest.mark.asyncio
async def test_stall_release_is_withheld_below_the_threshold(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    learner_state.stalled_turns = 1
    request = AgentTurnRequest(
        request_id="request-not-yet-stalled",
        session_id="session-1",
        state_revision=3,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="我不太确定要看什么",
    )

    result = await HiddenWorldRuntime(
        interpreter=_stuck_interpreter(), mentor=_plain_mentor()
    ).run_turn(request)

    assert all(item.kind != "release_evidence_on_stall" for item in result.proposals)


@pytest.mark.asyncio
async def test_public_reasoning_summary_comes_from_the_model_not_a_constant(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    """两轮不同输入必须产生不同的推理摘要。

    回归的是"每轮重复走固定步骤"那个观感问题：以前这两条 trace 是字面常量，
    不读 analysis，学生连发十条不同的话看到的过程一字不差。
    """
    summaries = []
    for index, text in enumerate(["先看日志", "改看发布记录"]):
        request = AgentTurnRequest(
            request_id=f"request-summary-{index}",
            session_id="session-1",
            state_revision=3,
            public_scenario=public_scenario,
            hidden_world=hidden_world,
            learner_state=learner_state,
            user_message=text,
        )
        result = await HiddenWorldRuntime(
            interpreter=_stuck_interpreter(summary=f"你想{text}。"),
            mentor=_plain_mentor(),
        ).run_turn(request)
        reasoning = [item.reasoning.text for item in result.public_trace if item.reasoning]
        summaries.append(reasoning)

    assert summaries[0] == ["你想先看日志。"]
    assert summaries[1] == ["你想改看发布记录。"]


@pytest.mark.asyncio
async def test_legacy_reply_deltas_are_emitted_only_after_guard(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    class StreamingLegacyRuntime(HiddenWorldRuntime):
        async def _run_mentor(self, _deps, *, on_reply_delta):
            await on_reply_delta("部分")
            await on_reply_delta("回复")
            return SimpleNamespace(
                output=MentorAction(
                    rationale="测试",
                    reply="部分回复",
                    requested_releases=[],
                    confirms_hypothesis=False,
                    expected_effort="quick",
                )
            )

    request = AgentTurnRequest(
        request_id="request-legacy-buffered-reply",
        session_id="session-1",
        state_revision=3,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="先看看 CPU",
    )
    events: list[tuple[str, str]] = []

    async def on_trace(event) -> None:
        events.append(("trace", event.kind))

    async def on_delta(piece: str) -> None:
        events.append(("delta", piece))

    result = await StreamingLegacyRuntime(
        interpreter=_stuck_interpreter(is_stuck=False),
        mentor=object(),
    ).run_turn(request, on_public_trace=on_trace, on_reply_delta=on_delta)

    guard_index = next(index for index, item in enumerate(events) if item == ("trace", "guard_passed"))
    delta_indexes = [index for index, item in enumerate(events) if item[0] == "delta"]
    assert delta_indexes
    assert min(delta_indexes) > guard_index
    assert result.reply == "部分回复"


@pytest.mark.asyncio
async def test_legacy_mentor_fallback_builds_a_typed_action(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    class FailingMentor:
        async def run(self, *_args, **_kwargs):
            raise RuntimeError("mentor unavailable")

    request = AgentTurnRequest(
        request_id="request-legacy-fallback",
        session_id="session-1",
        state_revision=3,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="先看看 CPU",
    )

    result = await HiddenWorldRuntime(
        interpreter=_stuck_interpreter(is_stuck=False),
        mentor=FailingMentor(),
    ).run_turn(request)

    assert result.reply
    assert "mentor_fallback" in result.internal_audit.reason_codes

