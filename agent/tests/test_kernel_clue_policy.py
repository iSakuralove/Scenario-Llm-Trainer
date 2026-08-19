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
    """常规通路的死角：说不出动作的学生一条都拿不到。"""
    assert ClueGate().approve(
        hidden_world,
        actions=[],
        collected_evidence=[],
        max_releases=3,
    ) == []


def test_stall_unlock_releases_one_entry_level_evidence(hidden_world) -> None:
    release = ClueGate().approve_on_stall(hidden_world, collected_evidence=[])

    assert release == "E_SLOW_SQL"
    node = hidden_world.evidence_by_id(release)
    assert node is not None
    # 兜底释放只放入口级证据：有前置的节点会跳过整段推理链。
    assert node.prerequisites == []


def test_stall_unlock_skips_already_collected_and_prerequisite_gated_evidence(hidden_world) -> None:
    collected = [
        node.evidence_id for node in hidden_world.evidence_graph if not node.prerequisites
    ]

    # 所有无前置证据都拿过之后不再兜底，绝不退而释放有前置的节点。
    assert ClueGate().approve_on_stall(hidden_world, collected_evidence=collected) == ""


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
