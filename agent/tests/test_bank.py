"""固定题库与三层校验测试。

负向用例是这组测试的重点：校验器只有在真的能拦住坏题时才有价值。
每个负向用例都对应一类现实中会发生的出题错误。
"""

from __future__ import annotations

import pytest

from hiddenworld.bank import (
    FIXED_BANK_IDS,
    ValidationError,
    list_fixed_questions,
    load_fixed_question,
    validate_question,
)
from hiddenworld.bank.validation import (
    MIN_EVIDENCE_NODES,
    MIN_HYPOTHESES,
    MIN_SUFFICIENT_SETS,
)
from hiddenworld.contracts import CONTRACT_VERSION, Observation

FIRST_FIXED_ID = "hw-db-index-001"


def test_first_fixed_question_loads_and_validates() -> None:
    question = load_fixed_question(FIRST_FIXED_ID)
    assert question.question_id == FIRST_FIXED_ID
    assert question.source == "fixed_hiddenworld"
    assert question.status == "active"
    assert question.version == 1
    assert question.difficulty == "L3"
    assert question.scenario_type == "performance"
    assert question.model_version == CONTRACT_VERSION


def test_first_fixed_question_meets_bank_scale() -> None:
    """006 定下的固定题最小规模。"""
    world = load_fixed_question(FIRST_FIXED_ID).hidden_world
    assert len(world.hypotheses) >= MIN_HYPOTHESES
    assert len(world.evidence_graph) >= MIN_EVIDENCE_NODES
    assert len(world.root_cause.sufficient_evidence_sets) >= MIN_SUFFICIENT_SETS
    assert any(observation.is_negative for observation in world.observations)
    assert world.misconception_rules
    assert "H_OTHER" in world.hypothesis_ids()


def test_both_evidence_paths_are_completable() -> None:
    """两条充分证据路径都必须走得通。

    只有一条能走通的题会悄悄惩罚走另一条路的学生——他做对了，
    系统却一直说他证据不够。
    """
    question = load_fixed_question(FIRST_FIXED_ID)
    report = validate_question(
        question.public_scenario, question.hidden_world, require_fixed_bank_scale=True
    )
    assert report.ok
    assert len(report.completable_sets) == len(
        question.hidden_world.root_cause.sufficient_evidence_sets
    )


def test_public_payload_excludes_hidden_world() -> None:
    payload = load_fixed_question(FIRST_FIXED_ID).public_payload()
    assert "hidden_world" not in payload
    assert payload["scenario_type"] == "performance"
    assert payload["source"] == "fixed_hiddenworld"
    assert payload["status"] == "active"
    serialized = str(payload)
    assert "idx_user_created" not in serialized
    assert "RC_INDEX_DROPPED" not in serialized


def test_prerequisite_prompt_is_natural_not_systemic() -> None:
    """前置未满足时，世界给的是情境化回应，不是「尚未解锁」。

    「该线索尚未解锁」既暴露了系统的存在，也暗示了存在一条前置。
    """
    world = load_fixed_question(FIRST_FIXED_ID).hidden_world
    gated = [o for o in world.observations if o.unmet_prerequisite_result]
    assert gated, "至少要有一个动作在前置未满足时给出自然回应"
    for observation in gated:
        text = observation.unmet_prerequisite_result
        for forbidden in ("解锁", "未解锁", "权限", "尚未开放", "不允许"):
            assert forbidden not in text, f"{observation.action} 的前置回应暴露了系统机制：{text}"


def test_list_fixed_questions_skips_missing() -> None:
    questions = list_fixed_questions()
    loaded_ids = {q.question_id for q in questions}
    assert FIRST_FIXED_ID in loaded_ids
    assert loaded_ids <= set(FIXED_BANK_IDS)


def test_unknown_question_raises() -> None:
    with pytest.raises(FileNotFoundError):
        load_fixed_question("hw-does-not-exist-999")


# ---- 负向用例：校验器必须拦住这些坏题 ----


def test_fixture_world_is_valid(public_scenario, hidden_world) -> None:
    """负向用例的对照组。

    没有这一条，下面每个 test_rejects_* 都可能是假阳性——夹具本身就是坏题时，
    "断言校验失败"永远成立，却什么也没证明。
    """
    report = validate_question(public_scenario, hidden_world)
    assert report.ok, f"夹具世界本身应当合法，却报错：{report.errors}"


def test_rejects_dangling_prerequisite(public_scenario, hidden_world) -> None:
    broken = hidden_world.model_copy(deep=True)
    broken.evidence_graph[1].prerequisites = ["E_DOES_NOT_EXIST"]
    report = validate_question(public_scenario, broken)
    assert not report.ok
    assert report.layer == "graph"
    assert any("不存在" in error for error in report.errors)


def test_rejects_prerequisite_cycle(public_scenario, hidden_world) -> None:
    broken = hidden_world.model_copy(deep=True)
    broken.evidence_graph[0].prerequisites = ["E_EXPLAIN_FULLSCAN"]
    report = validate_question(public_scenario, broken)
    assert not report.ok
    assert any("环" in error for error in report.errors)


def test_rejects_h_other_as_answer(public_scenario, hidden_world) -> None:
    """H_OTHER 表示「候选表之外」，设成正确答案会让 Verifier 对任何未知说法都判命中。"""
    broken = hidden_world.model_copy(deep=True)
    broken.root_cause.accepted_hypotheses = ["H_OTHER"]
    report = validate_question(public_scenario, broken)
    assert not report.ok
    assert any("H_OTHER" in error for error in report.errors)


def test_rejects_unreachable_evidence(public_scenario, hidden_world) -> None:
    """一条谁都拿不到的证据，会让学生永远卡在「证据还不够」。"""
    broken = hidden_world.model_copy(deep=True)
    broken.observations = [o for o in broken.observations if o.action != "inspect:data.explain"]
    report = validate_question(public_scenario, broken)
    assert not report.ok
    assert report.layer == "completability"
    assert any("E_EXPLAIN_FULLSCAN" in error for error in report.errors)


def test_rejects_distractor_that_cannot_be_ruled_out(public_scenario, hidden_world) -> None:
    """干扰假设必须能被学生自己的观察排除，不能靠系统替他划掉。"""
    broken = hidden_world.model_copy(deep=True)
    broken.observations = [
        Observation(
            action=o.action,
            result=o.result,
            is_negative=o.is_negative,
            yields_evidence=o.yields_evidence,
            rules_out=[] if "H_CPU_BOUND" in o.rules_out else o.rules_out,
            unmet_prerequisite_result=o.unmet_prerequisite_result,
        )
        for o in broken.observations
    ]
    report = validate_question(public_scenario, broken)
    assert not report.ok
    assert any("H_CPU_BOUND" in error for error in report.errors)


def test_rejects_duplicate_action(public_scenario, hidden_world) -> None:
    broken = hidden_world.model_copy(deep=True)
    broken.observations.append(broken.observations[0].model_copy(deep=True))
    report = validate_question(public_scenario, broken)
    assert not report.ok
    assert any("重复" in error for error in report.errors)


def test_validation_error_carries_structured_report(public_scenario, hidden_world) -> None:
    """生成接口失败时要能返回结构化 validation_errors，而不是一句「生成失败」。"""
    broken = hidden_world.model_copy(deep=True)
    broken.root_cause.sufficient_evidence_sets = [["E_DOES_NOT_EXIST"]]
    report = validate_question(public_scenario, broken)
    error = ValidationError(report)
    assert error.report.errors
    assert error.report.layer == "graph"
