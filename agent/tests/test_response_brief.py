from hiddenworld.contracts import (
    ConceptDefinition,
    HintStep,
    LearnerState,
    Observation,
    TurnAssessment,
)
from hiddenworld.scenario_runtime.response_brief import ResponseBriefBuilder


def _concepts():
    return [
        ConceptDefinition(
            concept_id="callback",
            label="支付回调",
            summary="支付平台通知商户系统处理结果的请求",
            aliases=["callback"],
        ),
        ConceptDefinition(
            concept_id="gateway",
            label="Gateway",
            summary="请求进入后端服务前的统一入口",
            aliases=["网关"],
        ),
        ConceptDefinition(
            concept_id="status_504",
            label="504",
            summary="网关等待后端超时返回的状态码",
            aliases=["超时状态码"],
        ),
        ConceptDefinition(
            concept_id="db_lock",
            label="DB lock wait",
            summary="数据库事务等待其他事务释放锁",
            aliases=["锁等待"],
        ),
    ]


def test_basic_student_gets_only_missing_concepts_and_explanation_task():
    brief = ResponseBriefBuilder().build(
        LearnerState(),
        concept_catalog=_concepts(),
        required_concepts=["callback", "gateway"],
        turn_assessment=TurnAssessment(
            intent="explanation_request",
            user_goal="支付回调是什么？",
        ),
    )

    assert brief.primary_task == "explain_concept"
    assert brief.explain_concepts == ["支付回调", "Gateway"]
    assert brief.known_concepts == []
    assert brief.investigation.missing_concepts == brief.explain_concepts


def test_engineer_skips_known_concepts_and_interprets_public_observation():
    state = LearnerState(concept_mastery={"callback": 3, "gateway": 3})
    brief = ResponseBriefBuilder().build(
        state,
        concept_catalog=_concepts(),
        required_concepts=["callback", "gateway"],
        turn_assessment=TurnAssessment(intent="investigate", progress_assessment="progress"),
        observations=[
            Observation(
                action="inspect:gateway.trace",
                result="Gateway 在等待 3 秒后返回 504。",
                is_negative=False,
                yields_evidence=["E_GATEWAY_TIMEOUT"],
            )
        ],
    )

    assert brief.primary_task == "interpret_evidence"
    assert brief.explain_concepts == []
    assert brief.known_concepts == ["支付回调", "Gateway"]
    assert brief.public_observations[0].result == "Gateway 在等待 3 秒后返回 504。"
    assert "inspect:gateway.trace" not in brief.public_observations[0].model_dump_json()
    assert "E_GATEWAY_TIMEOUT" not in brief.model_dump_json()


def test_fragmented_student_gets_selective_concept_explanation():
    state = LearnerState(concept_mastery={"callback": 3, "status_504": 3})
    brief = ResponseBriefBuilder().build(
        state,
        concept_catalog=_concepts(),
        required_concepts=["callback", "gateway", "status_504", "db_lock"],
        turn_assessment=TurnAssessment(intent="investigate"),
    )

    assert brief.known_concepts == ["支付回调", "504"]
    assert brief.explain_concepts == ["Gateway", "DB lock wait"]
    assert "支付回调" not in brief.do_not_repeat or "支付回调" in brief.known_concepts


def test_hint_level_tracks_stall_and_drops_after_real_progress():
    stalled = LearnerState(stalled_turns=3)
    brief = ResponseBriefBuilder().build(
        stalled,
        hint_steps=[
            HintStep(level=1, public_hint="先看故障前后有什么变化。"),
            HintStep(level=2, public_hint="比较切换前后的入口配置。"),
            HintStep(level=3, public_hint="关注 Gateway 的 timeout 配置。"),
        ],
        turn_assessment=TurnAssessment(is_stuck=True),
    )
    assert brief.primary_task == "release_hint"
    assert brief.hint_level == 3
    assert brief.hint_text == "关注 Gateway 的 timeout 配置。"

    recovered = ResponseBriefBuilder().build(
        LearnerState(stalled_turns=0, hint_level=3),
        turn_assessment=TurnAssessment(progress_assessment="progress"),
    )
    assert recovered.hint_level == 0
    assert recovered.hint_text == ""


def test_causal_boundaries_are_explicit_and_separate():
    brief = ResponseBriefBuilder().build(
        LearnerState(),
        direct_trigger="Gateway response_timeout 从 10 秒变成 3 秒。",
        latent_issue="少量请求存在较长的 DB lock wait。",
        causal_chain=[
            "DB lock wait 使 Callback 超过等待上限。",
            "Gateway 提前返回 504，支付平台随后重试。",
        ],
    )
    assert [item.role for item in brief.causal_boundaries] == [
        "direct_trigger",
        "latent_issue",
        "causal_chain",
        "causal_chain",
    ]
    assert brief.causal_boundaries[0].statement.startswith("Gateway")
    assert brief.causal_boundaries[1].statement.startswith("少量")
