import pytest

from hiddenworld.contracts import AnswerAttempt, CanonicalAnswer
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
    assert comparison.to_public().support_status == "needs_more_evidence"
    for forbidden in ("relation", "completion_allowed", "missing_evidence", "claim_alignment"):
        assert forbidden not in public


def test_v2_comparator_ignores_forged_hypothesis_id(hidden_world, learner_state) -> None:
    """有 CanonicalAnswer 时，模型伪造的 hypothesis_id 不能改变裁判。"""

    canonical = CanonicalAnswer(
        canonical_conclusion="发布漏掉 idx_user_created 索引，使订单查询走全表扫描。",
        root_cause_id=hidden_world.root_cause.id,
        required_evidence_ids=["E_RELEASE_LOG", "E_DDL_DIFF"],
        accepted_equivalents=["发布漏掉 idx_user_created 索引"],
        solution_requirements=list(hidden_world.root_cause.solution_requirements),
        answer_version="answer-v2",
    )
    world = hidden_world.model_copy(update={"canonical_answer": canonical})
    attempt = AnswerAttempt.from_user_message(
        answer_attempt_id="attempt-v2",
        session_id="session-1",
        turn_id="turn-3",
        revision=2,
        user_message="我认为发布漏掉 idx_user_created 索引，使订单查询走全表扫描。",
    )

    comparison = AnswerComparator().compare(
        world,
        learner_state=learner_state,
        attempt=attempt,
        hypothesis_id="H_POOL",  # 故意伪造，不能覆盖原文连接
        contradictions=[],
    )

    assert comparison.relation == "target"


def test_v2_comparator_does_not_trust_forged_target_id(hidden_world, learner_state) -> None:
    canonical = CanonicalAnswer(
        canonical_conclusion="发布漏掉 idx_user_created 索引，使订单查询走全表扫描。",
        root_cause_id=hidden_world.root_cause.id,
        required_evidence_ids=["E_RELEASE_LOG", "E_DDL_DIFF"],
        accepted_equivalents=["发布漏掉 idx_user_created 索引"],
        solution_requirements=list(hidden_world.root_cause.solution_requirements),
        answer_version="answer-v2",
    )
    world = hidden_world.model_copy(update={"canonical_answer": canonical})
    attempt = AnswerAttempt.from_user_message(
        answer_attempt_id="attempt-v2-wrong",
        session_id="session-1",
        turn_id="turn-3",
        revision=2,
        user_message="我认为是连接池打满。",
    )

    comparison = AnswerComparator().compare(
        world,
        learner_state=learner_state,
        attempt=attempt,
        hypothesis_id="H_INDEX",  # 故意伪造 target
        contradictions=[],
    )

    assert comparison.relation == "unrelated"
