import pytest

from hiddenworld.agents.tools import (
    CompareAnswerAuthorizationError,
    CompareAnswerRuntime,
    compare_answer_tool,
)
from hiddenworld.contracts import AnswerAttempt, TurnAnalysis


def answer_analysis() -> TurnAnalysis:
    return TurnAnalysis(
        public_summary="你说你完全不知道从哪下手，希望先拿到一点方向。",
        actions=[],
        hypothesis_id="H_INDEX",
        hypothesis_raw="",
        made_claim=True,
        contains_answer_attempt=True,
        answer_attempt_text="根因是发布时漏掉了订单表索引。",
        established_facts=[],
        is_stuck=False,
        is_noise=False,
        student_affect="engaged",
        confidence=0.95,
    )


def test_compare_answer_rejects_unbound_attempt_id(hidden_world, learner_state) -> None:
    runtime = CompareAnswerRuntime(
        request_id="request-1",
        session_id="session-1",
        turn_id="turn-3",
        revision=2,
        world=hidden_world,
        learner_state=learner_state,
        analysis=answer_analysis(),
        attempts={},
    )

    with pytest.raises(CompareAnswerAuthorizationError):
        runtime.execute("model-invented-id")


def test_compare_answer_tool_schema_has_no_model_parameters() -> None:
    schema = compare_answer_tool.function_schema.json_schema

    assert schema == {
        "additionalProperties": False,
        "properties": {},
        "type": "object",
    }


def test_compare_answer_is_idempotent_and_only_returns_public_projection(hidden_world, learner_state) -> None:
    learner_state.collected_evidence = ["E_RELEASE_LOG"]
    attempt = AnswerAttempt(
        answer_attempt_id="attempt-1",
        session_id="session-1",
        turn_id="turn-3",
        revision=2,
        text="根因是发布时漏掉了订单表索引。",
    )
    runtime = CompareAnswerRuntime(
        request_id="request-1",
        session_id="session-1",
        turn_id="turn-3",
        revision=2,
        world=hidden_world,
        learner_state=learner_state,
        analysis=answer_analysis(),
        attempts={attempt.answer_attempt_id: attempt},
    )

    first = runtime.execute_bound()
    second = runtime.execute_bound()

    assert first == second
    assert runtime.execution_count == 1
    assert runtime.internal_result is not None
    assert runtime.internal_result.relation == "target"
    public = first.model_dump()
    assert "relation" not in public
    assert "completion_allowed" not in public
    assert "claim_alignment" not in public

    with pytest.raises(CompareAnswerAuthorizationError):
        runtime.execute("different-unbound-id")
    assert runtime.execution_count == 1
