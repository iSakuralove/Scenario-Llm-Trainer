from hiddenworld.contracts import AnswerAttempt
from hiddenworld.kernel.comparator import AnswerComparator


def test_answer_comparator_keeps_target_and_missing_evidence_internal(hidden_world, learner_state) -> None:
    learner_state.collected_evidence = ["E_RELEASE_LOG"]
    attempt = AnswerAttempt(
        answer_attempt_id="attempt-1",
        session_id="session-1",
        turn_id="turn-3",
        revision=2,
        text="根因是发布时漏掉了订单表索引，我会重建 idx_user_created 索引。",
    )

    comparison = AnswerComparator().compare(
        hidden_world,
        learner_state=learner_state,
        attempt=attempt,
        hypothesis_id="H_INDEX",
        contradictions=[],
    )

    assert comparison.relation == "target"
    assert comparison.evidence_coverage == 0.5
    assert comparison.best_evidence_set == ["E_RELEASE_LOG", "E_DDL_DIFF"]
    assert comparison.missing_evidence == ["E_DDL_DIFF"]
    assert comparison.completion_allowed is False
    assert comparison.solution_coverage == 0.5

    public = comparison.to_public().model_dump()
    assert public["support_status"] == "needs_more_evidence"
    for forbidden in ("relation", "completion_allowed", "missing_evidence", "claim_alignment"):
        assert forbidden not in public
