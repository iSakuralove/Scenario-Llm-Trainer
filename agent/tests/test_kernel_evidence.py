from hiddenworld.contracts import TurnAnalysis
from hiddenworld.kernel.evidence import EvidenceEngine
from hiddenworld.kernel.world import HiddenWorldEngine


def analysis_for(*, actions: list[str], **overrides) -> TurnAnalysis:
    values = {
        "actions": actions,
        "hypothesis_id": "",
        "hypothesis_raw": "",
        "made_claim": False,
        "contains_answer_attempt": False,
        "answer_attempt_text": "",
        "established_facts": [],
        "is_stuck": False,
        "is_noise": False,
        "student_affect": "engaged",
        "confidence": 0.9,
    }
    values.update(overrides)
    return TurnAnalysis.model_validate(values)


def test_negative_observation_advances_learner_state(hidden_world, learner_state) -> None:
    analysis = analysis_for(actions=["inspect:metrics.cpu"])
    observation = HiddenWorldEngine().observe(
        hidden_world,
        action="inspect:metrics.cpu",
        collected_evidence=learner_state.collected_evidence,
    )

    updated = EvidenceEngine().advance(learner_state, analysis=analysis, observations=[observation])

    assert learner_state.collected_evidence == []
    assert updated.collected_evidence == ["E_CPU_NORMAL"]
    assert updated.ruled_out_hypotheses == ["H_CPU_BOUND"]
    assert updated.actions_taken == ["inspect:metrics.cpu"]
    assert updated.effective_turns == 1
    assert updated.stalled_turns == 0


def test_neutral_observation_records_action_but_increments_stalled_turns(hidden_world, learner_state) -> None:
    learner_state.stalled_turns = 1
    analysis = analysis_for(actions=["inspect:runtime.thread_dump"])
    observation = HiddenWorldEngine().observe(
        hidden_world,
        action="inspect:runtime.thread_dump",
        collected_evidence=learner_state.collected_evidence,
    )

    updated = EvidenceEngine().advance(learner_state, analysis=analysis, observations=[observation])

    assert updated.actions_taken == ["inspect:runtime.thread_dump"]
    assert updated.effective_turns == 0
    assert updated.stalled_turns == 2


def test_student_fact_and_hypothesis_advance_state_without_world_observation(learner_state) -> None:
    analysis = analysis_for(
        actions=[],
        hypothesis_id="H_INDEX",
        made_claim=True,
        established_facts=["订单查询正在全表扫描"],
    )

    updated = EvidenceEngine().advance(learner_state, analysis=analysis, observations=[])

    assert updated.current_hypothesis == "H_INDEX"
    assert updated.established_facts == ["订单查询正在全表扫描"]
    assert updated.effective_turns == 1
    assert updated.stalled_turns == 0


def test_low_confidence_analysis_cannot_write_action_or_hypothesis(learner_state) -> None:
    analysis = analysis_for(
        actions=["inspect:data.explain"],
        hypothesis_id="H_INDEX",
        made_claim=True,
        established_facts=["模型不确定是否真是全表扫描"],
        confidence=0.2,
    )

    updated = EvidenceEngine().advance(learner_state, analysis=analysis, observations=[])

    assert updated.actions_taken == []
    assert updated.current_hypothesis is None
    assert updated.established_facts == []
    assert updated.effective_turns == 0
    assert updated.stalled_turns == 1
