"""阶段 0 契约测试。

这些用例保护的是**信息边界**，不是功能。它们全部应该在契约定稿的那一刻就通过，
并且在此后任何一次"顺手加个字段"的改动中立刻变红。

最关键的一条是 test_public_projection_is_indistinguishable：它证明学生无法通过
反复提交不同答案、观察公开返回的差异，来二分搜索出标准答案。
"""

from __future__ import annotations

import dataclasses
from typing import Any

import pytest
from pydantic import BaseModel, ValidationError

from hiddenworld.contracts import (
    CanonicalAnswer,
    CONTRACT_VERSION,
    FORBIDDEN_PUBLIC_FIELDS,
    MENTOR_VISIBLE_MODELS,
    PUBLIC_MODELS,
    AgentTurnRequest,
    ContractVersionMismatch,
    Hypothesis,
    InternalAnswerComparison,
    MentorDeps,
    PublicAnswerComparison,
    RunEvent,
    TurnAnalysis,
    ScenarioContractValidationError,
    ScenarioContractValidator,
)


def _all_property_names(model: type[BaseModel]) -> set[str]:
    """递归收集一个模型 JSON Schema 里出现的全部字段名，包含 $defs 中的嵌套类型。

    只看顶层 properties 是不够的：RunEvent.tool 指向 ToolEventPayload，
    而泄露最可能发生在这种嵌套载荷里。
    """
    names: set[str] = set()

    def walk(node: Any) -> None:
        if isinstance(node, dict):
            props = node.get("properties")
            if isinstance(props, dict):
                names.update(props.keys())
            for value in node.values():
                walk(value)
        elif isinstance(node, list):
            for value in node:
                walk(value)

    walk(model.model_json_schema())
    return names


@pytest.mark.parametrize("model", PUBLIC_MODELS, ids=lambda m: m.__name__)
def test_public_models_have_no_forbidden_fields(model: type[BaseModel]) -> None:
    """浏览器可见的类型不得包含任何秘密字段。"""
    leaked = _all_property_names(model) & FORBIDDEN_PUBLIC_FIELDS
    assert not leaked, f"{model.__name__} 的公开 schema 里出现了秘密字段：{sorted(leaked)}"


@pytest.mark.parametrize("model", MENTOR_VISIBLE_MODELS, ids=lambda m: m.__name__)
def test_mentor_visible_models_have_no_forbidden_fields(model: type[BaseModel]) -> None:
    """进入 Mentor prompt 的类型同样不得包含秘密字段。

    may_release 是允许的（Mentor 要据此决定请求释放什么），它本来就不在禁止清单里。
    """
    leaked = _all_property_names(model) & FORBIDDEN_PUBLIC_FIELDS
    assert not leaked, f"{model.__name__} 会进入 Mentor prompt，却含有：{sorted(leaked)}"


def test_hypothesis_has_no_correctness_marker() -> None:
    """TurnInterpreter 拿到的假设表必须是未标注的多选一。"""
    fields = set(Hypothesis.model_fields)
    assert "is_correct" not in fields
    assert "correct" not in fields
    assert "is_target" not in fields
    assert fields == {"hypothesis_id", "label"}


def test_mentor_deps_field_boundary() -> None:
    """MentorDeps 的字段构成本身就是安全边界。"""
    names = {f.name for f in dataclasses.fields(MentorDeps)}
    assert names == {
        "public_scenario",
        "transcript",
        "learner_state",
        "constraints",
        "current_user_message",
        "current_intent",
        "requested_action_raw",
        "action_match_status",
        "evidence_request",
        "simulation_tools",
        "authorized_actions",
        "released_evidence",
        "answer_comparison",
        "guard_only",
    }
    # guard_only 是 Guard 的私有上下文，靠"instructions 不读它"来保证不进 prompt。
    # 这条是约定而非类型强制，所以 test_mentor_prompt_leakage（阶段 2）必须存在。
    for forbidden in ("hidden_world", "root_cause", "verification", "internal_audit"):
        assert forbidden not in names


def test_unknown_field_is_rejected() -> None:
    """题库是生成出来的，多吐一个字段必须当场失败而不是静默丢弃。"""
    with pytest.raises(ValidationError):
        Hypothesis.model_validate(
            {"hypothesis_id": "H_INDEX", "label": "索引问题", "is_correct": True}
        )


def test_missing_required_field_is_rejected() -> None:
    with pytest.raises(ValidationError):
        TurnAnalysis.model_validate({"actions": [], "hypothesis_id": ""})


def test_contract_version_mismatch_raises(hidden_world, learner_state, public_scenario) -> None:
    request = AgentTurnRequest(
        contract_version="hiddenworld.v0",
        request_id="req-1",
        session_id="sess-1",
        state_revision=3,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="先看看 CPU",
    )
    with pytest.raises(ContractVersionMismatch):
        request.require_contract_version()


def test_contract_version_default_matches(hidden_world, learner_state, public_scenario) -> None:
    request = AgentTurnRequest(
        request_id="req-1",
        session_id="sess-1",
        state_revision=3,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="先看看 CPU",
    )
    assert request.contract_version == CONTRACT_VERSION
    request.require_contract_version()


def _comparison(**overrides: Any) -> InternalAnswerComparison:
    base: dict[str, Any] = {
        "answer_attempt_id": "att-1",
        "relation": "unrelated",
        "claim_alignment": 0.0,
        "evidence_coverage": 0.5,
        "completion_allowed": False,
        "user_points": ["索引可能被删了"],
    }
    base.update(overrides)
    return InternalAnswerComparison.model_validate(base)


def test_public_projection_is_indistinguishable() -> None:
    """**最重要的一条。**

    猜中真相和完全猜错，只要学生自己拿到的观察一样多、说的要点一样，
    公开投影就必须逐字节相同。否则学生可以反复提交不同答案，
    靠比对返回差异二分搜索出标准答案——辅导工具就变成了答案探测器。
    """
    guessed_right = _comparison(
        relation="target",
        claim_alignment=0.97,
        completion_allowed=False,
        best_evidence_set=["E_SLOW_SQL", "E_EXPLAIN_FULLSCAN"],
        missing_evidence=["E_EXPLAIN_FULLSCAN"],
    )
    guessed_wrong = _comparison(
        relation="unrelated",
        claim_alignment=0.02,
        completion_allowed=False,
        best_evidence_set=[],
        missing_evidence=["E_SLOW_SQL", "E_EXPLAIN_FULLSCAN"],
    )

    assert guessed_right.to_public() == guessed_wrong.to_public()
    assert guessed_right.to_public().model_dump_json() == guessed_wrong.to_public().model_dump_json()


def test_public_projection_ignores_completion_allowed() -> None:
    """completion_allowed 翻转也不能改变任何公开字段。"""
    allowed = _comparison(relation="target", evidence_coverage=1.0, completion_allowed=True)
    not_allowed = _comparison(relation="unrelated", evidence_coverage=1.0, completion_allowed=False)
    assert allowed.to_public() == not_allowed.to_public()


def test_public_projection_reports_conflict_first() -> None:
    """学生自己前后说法打架时，先告诉他这件事。"""
    public = _comparison(contradictions=["你刚才说缓存命中率是正常的"]).to_public()
    assert public.support_status == "has_evidence_conflict"


def test_public_answer_comparison_has_no_verdict_fields() -> None:
    fields = set(PublicAnswerComparison.model_fields)
    assert fields == {
        "tool",
        "status",
        "user_points",
        "conclusion_status",
        "evidence_status",
        "causal_status",
        "missing_dimensions",
        "contradictions",
    }


def test_run_event_roundtrip_preserves_sequence() -> None:
    event = RunEvent(request_id="req-1", sequence=7, kind="reply_delta", text="先看")
    restored = RunEvent.model_validate_json(event.model_dump_json())
    assert restored == event
    assert restored.sequence == 7


def test_agent_turn_request_roundtrip(hidden_world, learner_state, public_scenario) -> None:
    request = AgentTurnRequest(
        request_id="req-1",
        session_id="sess-1",
        state_revision=3,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="先看看 CPU",
    )
    restored = AgentTurnRequest.model_validate_json(request.model_dump_json())
    assert restored == request
    assert restored.hidden_world.root_cause.id == "RC_INDEX_DROPPED"


def test_canonical_answer_validator_rejects_missing_evidence(hidden_world) -> None:
    world = hidden_world.model_copy(
        update={
            "canonical_answer": CanonicalAnswer(
                canonical_conclusion="发布重建索引时遗漏订单表索引",
                root_cause_id=hidden_world.root_cause.id,
                required_evidence_ids=["E_NOT_FOUND"],
                required_causal_relations=[],
                solution_requirements=list(hidden_world.root_cause.solution_requirements),
                answer_version="answer-v1",
            )
        }
    )
    with pytest.raises(ScenarioContractValidationError, match="missing evidence"):
        ScenarioContractValidator().validate(world)


def test_canonical_answer_validator_accepts_aligned_snapshot(hidden_world) -> None:
    rubric = hidden_world.solution_rubric.model_copy(
        update={
            "required_actions": list(hidden_world.root_cause.solution_requirements),
        }
    )
    world = hidden_world.model_copy(
        update={
            "diagnostic_relations": ["root_cause->symptom"],
            "solution_rubric": rubric,
            "canonical_answer": CanonicalAnswer(
                canonical_conclusion="发布重建索引时遗漏订单表索引",
                root_cause_id=hidden_world.root_cause.id,
                required_evidence_ids=["E_RELEASE_LOG", "E_DDL_DIFF"],
                required_causal_relations=["root_cause->symptom"],
                accepted_equivalents=["发布脚本漏了订单表索引"],
                solution_requirements=list(hidden_world.root_cause.solution_requirements),
                answer_version="answer-v1",
            ),
        }
    )
    assert ScenarioContractValidator().validate(world) == world
