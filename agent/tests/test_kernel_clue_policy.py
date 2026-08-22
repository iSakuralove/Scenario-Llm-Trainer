from hiddenworld.contracts import TurnAnalysis
from hiddenworld.kernel.cluegate import ClueGate
from hiddenworld.kernel.policy import TeachingPolicy


def test_clue_gate_releases_multiple_explicitly_requested_evidence_in_one_turn(hidden_world) -> None:
    releases = ClueGate().approve(
        hidden_world,
        actions=[
            "inspect:logs.slow_query",
            "inspect:data.explain",
            "inspect:change.release_log",
        ],
        collected_evidence=[],
        max_releases=3,
    )

    assert releases == ["E_SLOW_SQL", "E_EXPLAIN_FULLSCAN", "E_RELEASE_LOG"]


def test_clue_gate_gives_nothing_when_the_student_names_no_action(hidden_world) -> None:
    """没有明确动作时（包括卡住状态）不能自动释放证据。"""
    assert ClueGate().approve(
        hidden_world,
        actions=[],
        collected_evidence=[],
        max_releases=3,
    ) == []


def test_stalled_policy_keeps_evidence_release_separate_from_hint_state(learner_state) -> None:
    """连续卡住只进入教学状态，不能被伪装成学生获得了证据。"""
    learner_state.stalled_turns = 3
    analysis = TurnAnalysis(
        public_summary="我不知道从哪里开始。",
        actions=[],
        hypothesis_id="",
        hypothesis_raw="",
        made_claim=False,
        contains_answer_attempt=False,
        answer_attempt_text="",
        established_facts=[],
        is_stuck=True,
        is_noise=False,
        student_affect="frustrated",
        confidence=0.3,
    )

    constraints = TeachingPolicy().compile(
        learner_state,
        analysis=analysis,
        completion_allowed=False,
        evidence_coverage="0/2",
        may_release=[],
        allowed_category=None,
        contradictions=[],
    )

    assert constraints.may_release == []
    assert constraints.facts.stalled_turns == 3


def test_policy_compiles_boundaries_without_choosing_a_mentor_action(learner_state) -> None:
    learner_state.current_hypothesis = "H_INDEX"
    learner_state.established_facts = ["订单查询正在扫全表"]
    learner_state.stalled_turns = 2
    analysis = TurnAnalysis(
        public_summary="你说你完全不知道从哪下手，希望先拿到一点方向。",
        actions=["inspect:change.release_log"],
        hypothesis_id="H_INDEX",
        hypothesis_raw="",
        made_claim=True,
        contains_answer_attempt=False,
        answer_attempt_text="",
        established_facts=[],
        is_stuck=False,
        is_noise=False,
        student_affect="frustrated",
        confidence=0.9,
    )

    constraints = TeachingPolicy().compile(
        learner_state,
        analysis=analysis,
        completion_allowed=False,
        evidence_coverage="1/2",
        may_release=["E_RELEASE_LOG"],
        allowed_category="change",
        contradictions=[],
    )

    assert constraints.must_not == [
        "confirm_hypothesis",
        "reveal_unreleased",
        "start_debrief",
    ]
    assert constraints.may_release == ["E_RELEASE_LOG"]
    assert constraints.allowed_direction == "change_history"
    assert constraints.facts.hypothesis_supported is True
    assert constraints.facts.evidence_coverage == "1/2"
    assert constraints.facts.stalled_turns == 2
    assert constraints.facts.student_affect == "frustrated"
    assert not hasattr(constraints, "action")
