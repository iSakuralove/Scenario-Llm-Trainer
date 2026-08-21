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


def test_guard_rejects_retry_or_continue_guidance(teaching_constraints) -> None:
    action = mentor_action("这次没有形成可用观察，你可以稍后再试，或者继续梳理公开信息。")

    with pytest.raises(GuardViolation) as caught:
        Guard().validate(action, constraints=teaching_constraints, context=GuardContext())

    assert caught.value.code == "reply_policy_violation"
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


def test_guard_rejects_implicit_next_direction(teaching_constraints) -> None:
    action = mentor_action("已记录公开观察。链路中还有哪些环节可以进一步验证？")

    with pytest.raises(GuardViolation) as caught:
        Guard().validate(
            action,
            constraints=teaching_constraints,
            context=GuardContext(),
        )

    assert caught.value.code == "reply_policy_violation"


def test_guard_rejects_confirmation_and_scope_exclusion_framing(teaching_constraints) -> None:
    replies = [
        "刚才已经确认了订单库写入那段没什么异常。",
        "订单落库这一段看起来是正常的，剩下的链路再看。",
    ]

    for reply in replies:
        with pytest.raises(GuardViolation) as caught:
            Guard().validate(mentor_action(reply), constraints=teaching_constraints, context=GuardContext())
        assert caught.value.code == "reply_policy_violation"


def test_guard_rejects_paraphrased_observation_repetition(teaching_constraints) -> None:
    observation = "订单库写入日志：提交成功率 99.98%，返回码 200；未见写入超时。"
    reply = "刚才查到的订单库写入情况基本正常，提交成功率很高，没有看到写入超时。你怎么看这段表现？"

    with pytest.raises(GuardViolation) as caught:
        Guard().validate(
            mentor_action(reply),
            constraints=teaching_constraints,
            context=GuardContext(public_observation_texts=[observation]),
        )

    assert caught.value.code == "reply_repeats_observation"


def test_guard_rejects_positive_action_ack_when_no_observation_was_formed(teaching_constraints) -> None:
    action = mentor_action("好的，已经记录你想查看数据库锁等待的意图。")

    with pytest.raises(GuardViolation) as caught:
        Guard().validate(
            action,
            constraints=teaching_constraints,
            context=GuardContext(required_reply_mode="no_observation"),
        )

    assert caught.value.code == "reply_claims_observation_without_result"


def test_guard_rejects_success_claim_for_failed_observation(teaching_constraints) -> None:
    action = mentor_action("已经得到这项指标，可以继续判断了。")

    with pytest.raises(GuardViolation) as caught:
        Guard().validate(
            action,
            constraints=teaching_constraints,
            context=GuardContext(required_reply_mode="no_observation"),
        )

    assert caught.value.code == "reply_claims_observation_without_result"


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


def test_forbidden_entities_skip_bare_small_integers(hidden_world, public_scenario) -> None:
    """裸的一两位整数不进禁词表。

    实测固定题库 4 道题里有 3 道把 8 / 10 / 12 / 35 / 45 / 90 列成了禁词，
    结果 Mentor 连「10 分钟」都写不出来，只会被 Guard 打回后越改越空泛。
    """
    entities = extract_forbidden_entities(
        hidden_world,
        released_evidence_ids=[],
        public_scenario=public_scenario,
    )

    assert [item for item in entities if item.isdigit() and len(item) <= 2] == []
    # 真正指向隐藏内容的具体取值仍然守住：三位以上整数和带小数点的数字。
    assert "idx_user_created" in entities
    assert "240" in entities
    assert "3.8" in entities


def test_guard_allows_ordinary_small_numbers_but_still_blocks_precise_values(
    teaching_constraints,
    hidden_world,
    public_scenario,
) -> None:
    entities = extract_forbidden_entities(
        hidden_world,
        released_evidence_ids=[],
        public_scenario=public_scenario,
    )
    context = GuardContext(forbidden_entities=entities)

    ordinary = mentor_action("我们先看 3 个方向，每个大概花 10 分钟。")
    assert Guard().validate(ordinary, constraints=teaching_constraints, context=context) == ordinary

    with pytest.raises(GuardViolation) as caught:
        Guard().validate(
            mentor_action("慢查询平均耗时是 3.8 秒。"),
            constraints=teaching_constraints,
            context=context,
        )
    assert caught.value.code == "entity_leak"


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


def test_guard_allows_root_component_when_publicly_named(
    hidden_world, teaching_constraints
) -> None:
    from hiddenworld.contracts import PublicScenario

    public_scenario = PublicScenario(
        title="订单列表变慢",
        description="orders 表的请求响应变慢。",
        environment="MySQL orders",
    )
    entities = extract_forbidden_entities(
        hidden_world,
        released_evidence_ids=[],
        public_scenario=public_scenario,
    )
    assert hidden_world.root_cause.component not in entities
    action = mentor_action("orders 表的公开现象已经可见。")
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
