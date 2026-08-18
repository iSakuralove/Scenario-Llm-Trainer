import pytest

from hiddenworld.contracts import GuardContext, MentorAction
from hiddenworld.kernel.guard import Guard, GuardViolation, extract_forbidden_entities


def mentor_action(reply: str) -> MentorAction:
    return MentorAction(
        reply=reply,
        rationale="根据公开观察继续引导",
        requested_releases=[],
        confirms_hypothesis=False,
        expected_effort="quick",
    )


def test_guard_uses_english_word_boundaries_for_forbidden_entities(teaching_constraints) -> None:
    action = mentor_action("The available explain output may fail, so inspect it first.")

    validated = Guard().validate(
        action,
        constraints=teaching_constraints,
        context=GuardContext(forbidden_entities=["ai"]),
    )

    assert validated == action


def test_guard_rejects_exact_forbidden_entity_without_echoing_it(teaching_constraints) -> None:
    action = mentor_action("下一步直接检查 idx_user_created 是否存在。")

    with pytest.raises(GuardViolation) as caught:
        Guard().validate(
            action,
            constraints=teaching_constraints,
            context=GuardContext(forbidden_entities=["idx_user_created"]),
        )

    assert caught.value.code == "entity_leak"
    assert "idx_user_created" not in str(caught.value)


def test_guard_rejects_confirmation_before_evidence_completion(teaching_constraints) -> None:
    action = mentor_action("这个方向可以确定了。")
    action.confirms_hypothesis = True

    with pytest.raises(GuardViolation) as caught:
        Guard().validate(
            action,
            constraints=teaching_constraints,
            context=GuardContext(completion_allowed=False),
        )

    assert caught.value.code == "premature_confirmation"


def test_guard_rejects_release_outside_approved_subset(teaching_constraints) -> None:
    action = mentor_action("可以继续核对变更记录。")
    action.requested_releases = ["E_DDL_DIFF"]

    with pytest.raises(GuardViolation) as caught:
        Guard().validate(
            action,
            constraints=teaching_constraints,
            context=GuardContext(may_release=["E_RELEASE_LOG"]),
        )

    assert caught.value.code == "release_not_approved"
    assert "E_DDL_DIFF" not in str(caught.value)


def test_forbidden_entity_extraction_catches_partial_identifiers_and_numbers(hidden_world) -> None:
    entities = extract_forbidden_entities(hidden_world, released_evidence=["E_CPU_NORMAL"])

    assert "idx_user_created" in entities
    assert "240" in entities
    assert hidden_world.root_cause.component in entities


def test_guard_normalizes_spacing_in_mixed_chinese_english_entities(teaching_constraints) -> None:
    action = mentor_action("下一步直接检查 orders表。")

    with pytest.raises(GuardViolation) as caught:
        Guard().validate(
            action,
            constraints=teaching_constraints,
            context=GuardContext(forbidden_entities=["orders 表"]),
        )

    assert caught.value.code == "entity_leak"
