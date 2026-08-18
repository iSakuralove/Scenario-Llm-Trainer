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


def test_policy_compiles_boundaries_without_choosing_a_mentor_action(learner_state) -> None:
    learner_state.current_hypothesis = "H_INDEX"
    learner_state.established_facts = ["订单查询正在扫全表"]
    learner_state.stalled_turns = 2
    analysis = TurnAnalysis(
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
