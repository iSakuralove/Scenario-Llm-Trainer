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


def test_forbidden_entity_extraction_catches_partial_identifiers_and_numbers(
    hidden_world, public_scenario
) -> None:
    entities = extract_forbidden_entities(
        hidden_world,
        released_evidence_ids=["E_CPU_NORMAL"],
        public_scenario=public_scenario,
    )

    assert "idx_user_created" in entities
    assert "240" in entities
    assert hidden_world.root_cause.component in entities


def test_guard_allows_public_and_released_numeric_facts(
    hidden_world, public_scenario, teaching_constraints
) -> None:
    entities = extract_forbidden_entities(
        hidden_world,
        released_evidence_ids=["E_SLOW_SQL"],
        public_scenario=public_scenario,
    )

    assert "P99" not in entities
    assert "3.8" not in entities
    action = mentor_action("慢查询日志里这条 SQL 平均耗时 3.8s，接口 P99 约 4s。")
    assert Guard().validate(
        action,
        constraints=teaching_constraints,
        context=GuardContext(forbidden_entities=entities),
    ) == action


def test_guard_rejects_unreleased_numeric_fact_with_unit(
    hidden_world, public_scenario, teaching_constraints
) -> None:
    entities = extract_forbidden_entities(
        hidden_world,
        released_evidence_ids=[],
        public_scenario=public_scenario,
    )

    assert "3.8" in entities
    with pytest.raises(GuardViolation) as caught:
        Guard().validate(
            mentor_action("慢查询平均耗时 3.8s。"),
            constraints=teaching_constraints,
            context=GuardContext(forbidden_entities=entities),
        )
    assert caught.value.code == "entity_leak"


def test_guard_normalizes_spacing_in_mixed_chinese_english_entities(teaching_constraints) -> None:
    action = mentor_action("下一步直接检查 orders表。")

    with pytest.raises(GuardViolation) as caught:
        Guard().validate(
            action,
            constraints=teaching_constraints,
            context=GuardContext(forbidden_entities=["orders 表"]),
        )

    assert caught.value.code == "entity_leak"
