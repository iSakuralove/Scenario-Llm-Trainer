from __future__ import annotations

from hiddenworld.contracts import (
    AgentTurnRequest,
    GuidanceState,
    InternalAnswerComparison,
    TeachingDecision,
    TurnAnalysis,
    TurnControl,
    VirtualTool,
)
from hiddenworld.scenario_runtime.context import project_agent_context
from hiddenworld.scenario_runtime.state_reducer import StateReducer


def _request(hidden_world, learner_state, public_scenario, *, message: str = ""):
    return AgentTurnRequest(
        request_id="context-reducer-test",
        session_id="session-1",
        state_revision=4,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message=message,
    )


def test_context_reinjects_prior_guidance_navigation_and_terminal_without_private_flags(
    hidden_world,
    learner_state,
    public_scenario,
):
    request = _request(hidden_world, learner_state, public_scenario)
    prior_guidance = GuidanceState(
        teaching_state="conclusion_grilling",
        progress_assessment="unsupported",
        navigation=[
            {
                "dimension_id": "dimension:data",
                "category": "data",
                "status": "in_progress",
                "hint_level": "light",
            }
        ],
        stalled_turns=2,
        current_focus="当前核验焦点",
    )
    prior_control = TurnControl(
        terminal=True,
        completion_allowed=True,
        completion_ready=True,
        allowed_action_ids=["inspect:metrics.cpu"],
    )

    context = project_agent_context(
        request,
        prior_guidance_state=prior_guidance,
        prior_turn_control=prior_control,
    )

    assert context.guidance_state == prior_guidance
    assert context.teaching_navigation == prior_guidance.navigation
    assert context.turn_control.terminal is True
    # completion 判定与动作列表不能越过 Agent 安全边界。
    dumped = context.model_dump(mode="json")
    assert "completion_allowed" not in dumped["turn_control"]
    assert "completion_ready" not in dumped["turn_control"]
    assert "allowed_action_ids" not in dumped["turn_control"]


def test_context_accepts_legacy_dict_state_snapshot_without_exposing_private_control(
    hidden_world,
    learner_state,
    public_scenario,
):
    request = _request(hidden_world, learner_state, public_scenario)
    # 旧 HTTP 适配器可能在显式参数接入前挂载 dict 快照。
    object.__setattr__(
        request,
        "prior_guidance_state",
        {
            "teaching_state": "guided_inquiry",
            "progress_assessment": "partial",
            "navigation": [],
            "stalled_turns": 1,
            "current_focus": "证据",
        },
    )
    object.__setattr__(
        request,
        "prior_turn_control",
        {"terminal": True, "completion_allowed": True, "completion_ready": True},
    )

    context = project_agent_context(request)

    assert context.guidance_state.teaching_state == "guided_inquiry"
    assert context.turn_control.terminal is True


def test_context_deduplicates_navigation_dimensions_from_a_replayed_snapshot(
    hidden_world,
    learner_state,
    public_scenario,
):
    request = _request(hidden_world, learner_state, public_scenario)
    prior = GuidanceState(
        navigation=[
            {"dimension_id": "dimension:data", "category": "data"},
            {"dimension_id": "dimension:data", "category": "data", "status": "covered"},
        ]
    )

    context = project_agent_context(request, prior_guidance_state=prior)

    assert [item.dimension_id for item in context.teaching_navigation] == ["dimension:data"]


def test_context_does_not_mutate_terminal_guidance_from_a_newer_learner_snapshot(
    hidden_world,
    learner_state,
    public_scenario,
):
    request = _request(
        hidden_world,
        learner_state.model_copy(update={"stalled_turns": 9, "current_focus": "新焦点"}),
        public_scenario,
    )
    prior = GuidanceState(
        teaching_state="debrief",
        stalled_turns=1,
        current_focus="已结束焦点",
    )

    context = project_agent_context(
        request,
        prior_guidance_state=prior,
        prior_turn_control=TurnControl(terminal=True),
    )

    assert context.guidance_state.stalled_turns == 1
    assert context.guidance_state.current_focus == "已结束焦点"


def test_context_accepts_explicit_json_snapshots_for_back_injection(
    hidden_world,
    learner_state,
    public_scenario,
):
    request = _request(hidden_world, learner_state, public_scenario)

    context = project_agent_context(
        request,
        prior_guidance_state={
            "teaching_state": "guided_inquiry",
            "progress_assessment": "partial",
            "navigation": [],
            "stalled_turns": 2,
            "current_focus": "日志",
        },
        prior_turn_control={"terminal": True},
    )

    assert context.guidance_state.teaching_state == "guided_inquiry"
    assert context.turn_control.terminal is True


def test_context_does_not_expose_internal_answer_comparison_tool(
    hidden_world,
    learner_state,
    public_scenario,
):
    world = hidden_world.model_copy(
        deep=True,
        update={
            "virtual_tools": [
                VirtualTool(
                    tool_id="tool.compare",
                    kind="answer_comparison",
                    target="内部答案比较",
                    aliases=["提交答案"],
                    query_patterns=[],
                    redacted_parameters=[],
                    simulated_output="不得进入 Agent",
                    observation_action="compare_answer",
                    evidence_ids=[],
                ),
                VirtualTool(
                    tool_id="tool.cpu",
                    kind="metrics",
                    target="数据库 CPU",
                    aliases=[],
                    query_patterns=[],
                    redacted_parameters=[],
                    simulated_output="CPU 正常",
                    observation_action="inspect:metrics.cpu",
                    evidence_ids=[],
                ),
            ]
        },
    )
    request = _request(world, learner_state, public_scenario, message="提交答案")

    context = project_agent_context(request)

    assert [item.tool_id for item in context.action_catalog] == ["inspect:metrics.cpu"]
    assert context.authorized_actions == []


def test_reducer_projects_navigation_and_remaining_declared_actions(
    hidden_world,
    learner_state,
    public_scenario,
):
    request = _request(hidden_world, learner_state, public_scenario, message="先看看 CPU")
    analysis = TurnAnalysis(
        public_summary="你想先确认 CPU。",
        intent="investigate",
        actions=["inspect:metrics.cpu"],
        hypothesis_id="",
        hypothesis_raw="",
        made_claim=False,
        contains_answer_attempt=False,
        answer_attempt_text="",
        established_facts=[],
        is_stuck=False,
        is_noise=False,
        student_affect="engaged",
        confidence=0.95,
    )

    reduction = StateReducer().reduce(request, analysis=analysis, observations=[hidden_world.observations[4]])

    assert reduction.turn_control.terminal is False
    assert reduction.turn_control.completion_allowed is False
    assert reduction.turn_control.completion_ready is False
    assert "inspect:metrics.cpu" not in reduction.turn_control.allowed_action_ids
    assert "inspect:resource.pool" in reduction.turn_control.allowed_action_ids
    navigation = {item.category: item for item in reduction.guidance_state.navigation}
    assert navigation["capacity"].status == "in_progress"
    assert navigation["capacity"].hint_level == "light"


def test_reducer_does_not_reoffer_virtual_tool_when_its_observation_is_already_collected(
    hidden_world,
    learner_state,
    public_scenario,
):
    world = hidden_world.model_copy(
        deep=True,
        update={
            "virtual_tools": [
                VirtualTool(
                    tool_id="tool.cpu",
                    kind="metrics",
                    target="数据库 CPU",
                    aliases=[],
                    query_patterns=[],
                    redacted_parameters=[],
                    simulated_output="CPU 正常",
                    observation_action="inspect:metrics.cpu",
                    evidence_ids=[],
                )
            ]
        },
    )
    request = _request(
        world,
        learner_state.model_copy(update={"collected_evidence": ["E_CPU_NORMAL"]}),
        public_scenario,
    )
    analysis = TurnAnalysis(
        public_summary="已有 CPU 观察。",
        intent="investigate",
        actions=[],
        hypothesis_id="",
        hypothesis_raw="",
        made_claim=False,
        contains_answer_attempt=False,
        answer_attempt_text="",
        established_facts=[],
        is_stuck=False,
        is_noise=False,
        student_affect="engaged",
        confidence=0.9,
    )

    reduction = StateReducer().reduce(request, analysis=analysis)

    assert "inspect:metrics.cpu" not in reduction.turn_control.allowed_action_ids


def test_reducer_keeps_completion_permission_distinct_from_ready_and_terminal(
    hidden_world,
    learner_state,
    public_scenario,
):
    learner = learner_state.model_copy(
        update={"current_hypothesis": "H_INDEX", "collected_evidence": ["E_SLOW_SQL", "E_EXPLAIN_FULLSCAN"]}
    )
    request = _request(hidden_world, learner, public_scenario)
    analysis = TurnAnalysis(
        public_summary="已有足够公开观察。",
        intent="hypothesis",
        actions=[],
        hypothesis_id="H_INDEX",
        hypothesis_raw="索引问题",
        made_claim=True,
        contains_answer_attempt=False,
        answer_attempt_text="",
        established_facts=[],
        is_stuck=False,
        is_noise=False,
        student_affect="engaged",
        confidence=0.9,
    )

    reduction = StateReducer().reduce(request, analysis=analysis)

    assert reduction.turn_control.completion_allowed is True
    assert reduction.turn_control.completion_ready is False
    assert reduction.turn_control.terminal is False


def test_reducer_preserves_internal_answer_comparison_and_uses_its_permission(
    hidden_world,
    learner_state,
    public_scenario,
):
    request = _request(hidden_world, learner_state, public_scenario)
    analysis = TurnAnalysis(
        public_summary="用户提交了一个结论。",
        intent="answer_attempt",
        actions=[],
        hypothesis_id="H_INDEX",
        hypothesis_raw="索引问题",
        made_claim=True,
        contains_answer_attempt=True,
        answer_attempt_text="索引问题",
        established_facts=[],
        is_stuck=False,
        is_noise=False,
        student_affect="engaged",
        confidence=0.95,
    )
    comparison = InternalAnswerComparison(
        answer_attempt_id="attempt-1",
        relation="target",
        claim_alignment=1.0,
        evidence_coverage=0.0,
        best_evidence_set=["E_SLOW_SQL"],
        missing_evidence=["E_SLOW_SQL"],
        contradictions=[],
        solution_coverage=0.0,
        missing_solution_requirements=[],
        completion_allowed=False,
        user_points=["索引问题"],
    )

    reduction = StateReducer().reduce(request, analysis=analysis, answer_comparison=comparison)

    assert reduction.answer_comparison is comparison
    assert reduction.turn_control.completion_allowed is False
    assert reduction.turn_control.completion_ready is False
    assert reduction.guidance_state.teaching_state in {"conclusion_grilling", "premature_conclusion"}


def test_reducer_rejects_illegal_jump_and_preserves_terminal_state(
    hidden_world,
    learner_state,
    public_scenario,
):
    request = _request(hidden_world, learner_state, public_scenario)
    analysis = TurnAnalysis(
        public_summary="继续聊天。",
        intent="chat",
        actions=[],
        hypothesis_id="",
        hypothesis_raw="",
        made_claim=False,
        contains_answer_attempt=False,
        answer_attempt_text="",
        established_facts=[],
        is_stuck=False,
        is_noise=False,
        student_affect="engaged",
        confidence=0.9,
    )
    prior_guidance = GuidanceState(teaching_state="conclusion_grilling", current_focus="核验")
    prior_control = TurnControl(terminal=True, completion_allowed=True, completion_ready=True)

    reduction = StateReducer().reduce(
        request,
        analysis=analysis,
        teaching_decision=TeachingDecision(teaching_state="casual_chat"),
        prior_guidance_state=prior_guidance,
        prior_turn_control=prior_control,
    )

    assert reduction.guidance_state.teaching_state == "conclusion_grilling"
    assert reduction.turn_control.terminal is True
    assert reduction.turn_control.allowed_action_ids == []
    assert reduction.turn_control.completion_allowed is True
    assert reduction.turn_control.completion_ready is True
