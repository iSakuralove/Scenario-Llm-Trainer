"""支付回调题的行为金标：校验教学动作和状态，不锁死整段自然语言。"""

from hiddenworld.contracts import ConceptDefinition, HintStep, LearnerState, Observation, TurnAssessment
from hiddenworld.scenario_runtime.response_brief import ResponseBriefBuilder


def _payment_concepts() -> list[ConceptDefinition]:
    return [
        ConceptDefinition(
            concept_id="callback",
            label="Callback",
            summary="支付平台通知业务系统处理结果的请求。",
            aliases=["支付回调", "回调"],
        ),
        ConceptDefinition(
            concept_id="gateway",
            label="Gateway",
            summary="请求进入后端前的入口层，负责转发和超时控制。",
            aliases=["网关", "云 VIP"],
        ),
        ConceptDefinition(
            concept_id="http_504",
            label="HTTP 504",
            summary="网关等待上游响应超时后返回的状态。",
            aliases=["504"],
        ),
        ConceptDefinition(
            concept_id="db_lock",
            label="DB Lock",
            summary="事务等待其他事务释放数据锁产生的延迟。",
            aliases=["数据库锁", "锁等待"],
        ),
        ConceptDefinition(
            concept_id="idempotency",
            label="幂等",
            summary="同一支付事件重复到达时业务结果只生效一次。",
            aliases=["幂等性"],
        ),
    ]


def test_three_student_profiles_choose_different_first_tasks() -> None:
    concepts = _payment_concepts()
    builder = ResponseBriefBuilder()

    basic = builder.build(
        LearnerState(),
        concept_catalog=concepts,
        required_concepts=["Callback", "Gateway"],
        turn_assessment=TurnAssessment(intent="explanation_request"),
    )
    engineer = builder.build(
        LearnerState(concept_mastery={"callback": 3, "gateway": 3}),
        concept_catalog=concepts,
        required_concepts=["Callback", "Gateway"],
        observations=[
            Observation(
                action="inspect:logs.callback_timeout",
                result="Gateway 在 3 秒返回 504。",
                is_negative=False,
                yields_evidence=["E_GATEWAY_TIMEOUT_CONFIG"],
            )
        ],
        turn_assessment=TurnAssessment(intent="investigate", progress_assessment="progress"),
    )
    fragmented = builder.build(
        LearnerState(concept_mastery={"callback": 3, "http_504": 3}),
        concept_catalog=concepts,
        required_concepts=["Callback", "Gateway", "HTTP 504", "DB Lock"],
        turn_assessment=TurnAssessment(intent="investigate"),
    )

    assert basic.primary_task == "explain_concept"
    assert basic.explain_concepts == ["Callback", "Gateway"]
    assert engineer.primary_task == "interpret_evidence"
    assert engineer.explain_concepts == []
    assert fragmented.primary_task == "explain_concept"
    assert fragmented.explain_concepts == ["Gateway", "DB Lock"]


def test_payment_trace_stays_distinct_after_an_eighteen_turn_projection() -> None:
    concepts = _payment_concepts()
    builder = ResponseBriefBuilder()
    state = LearnerState(concept_mastery={"callback": 3, "gateway": 3, "http_504": 3})
    hint_steps = [
        HintStep(level=1, public_hint="先对齐故障发生前后的入口变化。"),
        HintStep(level=2, public_hint="比较切换前后的 Gateway 配置。"),
        HintStep(level=3, public_hint="重点看 Gateway timeout。"),
        HintStep(level=4, public_hint="把 Gateway、Nginx 和 Callback 的时间线放在一起。"),
    ]

    for turn in range(1, 19):
        if turn == 13:
            state = state.model_copy(update={"stalled_turns": 1})
            brief = builder.build(
                state,
                concept_catalog=concepts,
                hint_steps=hint_steps,
                turn_assessment=TurnAssessment(is_stuck=True, intent="help_request"),
            )
            assert brief.primary_task == "release_hint"
            assert brief.hint_level == 1
            assert brief.hint_text
            continue

        if turn == 14:
            state = state.model_copy(update={"stalled_turns": 0, "hint_level": 1, "last_hint": ""})
            brief = builder.build(
                state,
                concept_catalog=concepts,
                hint_steps=hint_steps,
                public_clues=["Gateway timeout 从 10 秒变成 3 秒"],
                turn_assessment=TurnAssessment(progress_assessment="progress"),
            )
            assert brief.primary_task == "acknowledge_progress"
            assert brief.hint_level == 0
            continue

        brief = builder.build(
            state,
            concept_catalog=concepts,
            public_observations=["Gateway 在 3 秒返回 504，Nginx 在 3.842 秒返回 200"],
            direct_trigger="Gateway timeout 从 10 秒变成 3 秒",
            latent_issue="少量请求存在 DB lock wait",
            causal_chain=["DB lock wait → Callback 超过 3 秒", "Gateway 504 → 支付平台重试"],
            turn_assessment=TurnAssessment(intent="investigate"),
        )
        assert brief.investigation.current_focus == ""
        assert brief.explain_concepts == []
        assert {item.role for item in brief.causal_boundaries} == {
            "direct_trigger",
            "latent_issue",
            "causal_chain",
        }
        assert "Gateway timeout 从 10 秒变成 3 秒" in brief.causal_boundaries[0].statement
        assert all("E_" not in item.result for item in brief.public_observations)
