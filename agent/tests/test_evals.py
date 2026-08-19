"""阶段 5 评测资产与离线运行器测试。"""

import asyncio
import json

import pytest
from pydantic_ai import ModelResponse, TextPart
from pydantic_ai.models.function import FunctionModel
from pydantic_ai.models.test import TestModel

from hiddenworld.agents.interpreter import create_interpreter_agent
from hiddenworld.agents.mentor import create_mentor_agent
from hiddenworld.bank.loader import load_fixed_question
from hiddenworld.contracts import LearnerState
from hiddenworld.evals import (
    ADAPTIVE_TRAJECTORIES,
    ALL_TRAJECTORIES,
    FIXED_TRAJECTORIES,
    BehaviorMetrics,
    GoldenCase,
    MatrixReport,
    TrajectoryReport,
    apply_proposals_for_eval,
    build_interpreter_dataset,
    compare_provider_matrices,
    load_golden_cases,
    run_interpreter_goldens,
    run_trajectory,
    run_transcript_judge,
)
from hiddenworld.runtime import HiddenWorldRuntime


def test_stage_five_matrix_covers_required_fixed_and_adaptive_tracks() -> None:
    assert len(FIXED_TRAJECTORIES) == 8
    assert len(ADAPTIVE_TRAJECTORIES) == 4
    assert {item.case_id for item in FIXED_TRAJECTORIES} == {
        "normal-investigation",
        "casual-chat",
        "noise",
        "experienced-misdirection",
        "short-root-cause",
        "long-root-cause",
        "direct-answer-request",
        "contradictory-answer",
    }
    assert {item.case_id for item in ADAPTIVE_TRAJECTORIES} == {
        "novice",
        "experienced-wrong",
        "expert-fast-lane",
        "frustrated-stalled",
    }
    assert len({item.case_id for item in ALL_TRAJECTORIES}) == len(ALL_TRAJECTORIES)


def test_interpreter_seed_goldens_are_jsonl_and_unique() -> None:
    cases = load_golden_cases()
    assert 50 <= len(cases) <= 120
    assert len({case.case_id for case in cases}) == len(cases)
    assert all(case.input.strip() for case in cases)


def test_eval_proposals_are_typed_and_idempotent() -> None:
    state = LearnerState()
    from hiddenworld.contracts import Proposal

    updated = apply_proposals_for_eval(
        state,
        [
            Proposal(kind="release_evidence", evidence_id="E1"),
            Proposal(kind="release_evidence", evidence_id="E1"),
            Proposal(kind="record_action", action="inspect:logs"),
            Proposal(kind="record_action", action="inspect:logs"),
            Proposal(kind="set_stalled_turns", value=2),
        ],
    )
    assert updated.collected_evidence == ["E1"]
    assert updated.actions_taken == ["inspect:logs"]
    assert updated.stalled_turns == 2
    assert state.collected_evidence == []


@pytest.mark.asyncio
async def test_interpreter_golden_runner_returns_public_summary() -> None:
    model = TestModel(
        custom_output_text=json.dumps(
            {
                "public_summary": "你说你完全不知道从哪下手，希望先拿到一点方向。",
                "actions": ["inspect:metrics.cpu"],
                "hypothesis_id": "H_CPU_BOUND",
                "hypothesis_raw": "CPU 打满",
                "made_claim": False,
                "contains_answer_attempt": False,
                "answer_attempt_text": "",
                "established_facts": [],
                "is_stuck": False,
                "is_noise": False,
                "student_affect": "engaged",
                "confidence": 0.95,
            },
            ensure_ascii=False,
        )
    )
    report = await run_interpreter_goldens(
        model,
        cases=[
            GoldenCase(
                case_id="cpu",
                input="检查 CPU",
                expected_actions=("inspect:metrics.cpu",),
                hypothesis_id="H_CPU_BOUND",
                contains_answer_attempt=False,
            )
        ],
    )
    assert report.public_dict() == {
        "total": 1,
        "completed": 1,
        "passed": 1,
        "completion_rate": 1.0,
        "pass_rate": 1.0,
        "failures": [],
        "error_code": "",
    }


@pytest.mark.asyncio
async def test_interpreter_golden_runner_enforces_per_case_deadline() -> None:
    async def slow_model(_messages, _info) -> ModelResponse:
        await asyncio.sleep(0.05)
        return ModelResponse(parts=[TextPart("{}")])

    report = await run_interpreter_goldens(
        FunctionModel(slow_model),
        cases=[GoldenCase(case_id="slow", input="检查 CPU")],
        deadline_seconds=0.001,
    )
    assert report.completed == 0
    assert report.error_code == "provider_timeout"


@pytest.mark.asyncio
async def test_pydantic_evals_dataset_executes_hiddenworld_golden_evaluator() -> None:
    from hiddenworld.contracts import TurnAnalysis

    case = GoldenCase(
        case_id="dataset-cpu",
        input="检查 CPU",
        expected_actions=("inspect:metrics.cpu",),
    )
    dataset = build_interpreter_dataset([case])

    async def task(_case: GoldenCase) -> TurnAnalysis:
        return TurnAnalysis(
            public_summary="你说你完全不知道从哪下手，希望先拿到一点方向。",
            actions=["inspect:metrics.cpu"],
            hypothesis_id="H_CPU_BOUND",
            hypothesis_raw="CPU 打满",
            made_claim=False,
            contains_answer_attempt=False,
            answer_attempt_text="",
            established_facts=[],
            is_stuck=False,
            is_noise=False,
            student_affect="engaged",
            confidence=0.95,
        )

    report = await dataset.evaluate(task, progress=False, max_concurrency=1)
    assert len(report.cases) == 1


def test_trajectory_answer_tool_expectations_are_per_turn() -> None:
    case = next(item for item in ADAPTIVE_TRAJECTORIES if item.case_id == "experienced-wrong")
    assert [case.expects_answer_tool(index) for index in range(1, 4)] == [True, True, False]


@pytest.mark.asyncio
async def test_eval_runner_supports_multi_turn_without_exposing_hidden_world() -> None:
    question = load_fixed_question("hw-db-index-001")
    interpreter = create_interpreter_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "public_summary": "你说你完全不知道从哪下手，希望先拿到一点方向。",
                    "actions": [],
                    "hypothesis_id": "H_OTHER",
                    "hypothesis_raw": "",
                    "made_claim": False,
                    "contains_answer_attempt": False,
                    "answer_attempt_text": "",
                    "established_facts": [],
                    "is_stuck": True,
                    "is_noise": False,
                    "student_affect": "confused",
                    "confidence": 0.9,
                }
            )
        )
    )
    mentor = create_mentor_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "reply": "先说明你想核对哪条公开现象。",
                    "rationale": "只根据公开上下文给出小步引导。",
                    "requested_releases": [],
                    "confirms_hypothesis": False,
                    "expected_effort": "quick",
                },
                ensure_ascii=False,
            )
        )
    )
    report = await run_trajectory(
        HiddenWorldRuntime(interpreter=interpreter, mentor=mentor),
        question,
        next(item for item in ADAPTIVE_TRAJECTORIES if item.case_id == "novice"),
        provider="test",
        request_prefix="offline-eval",
    )
    assert report.turns == 3
    assert report.passed
    assert report.behavior.sentence_repetition_rate == 1.0
    assert report.behavior.question_type_entropy == 0.0
    assert report.behavior.max_stalled_turns == 2
    assert report.behavior.leak_rate == 0.0


@pytest.mark.asyncio
async def test_trajectory_runner_enforces_per_turn_deadline() -> None:
    async def slow_model(_messages, _info) -> ModelResponse:
        await asyncio.sleep(0.05)
        return ModelResponse(parts=[TextPart("{}")])

    question = load_fixed_question("hw-db-index-001")
    runtime = HiddenWorldRuntime(
        interpreter=create_interpreter_agent(FunctionModel(slow_model)),
        mentor=create_mentor_agent(FunctionModel(slow_model)),
    )
    report = await run_trajectory(
        runtime,
        question,
        FIXED_TRAJECTORIES[0],
        provider="slow",
        request_prefix="deadline",
        deadline_seconds=0.001,
    )
    assert report.turns == 0
    assert report.error_code == "provider_timeout"


@pytest.mark.asyncio
async def test_llm_judge_reports_verdict_without_transcript_or_reason() -> None:
    case = ADAPTIVE_TRAJECTORIES[0]
    trajectory = TrajectoryReport(
        provider="glm",
        question_id="hw-db-index-001",
        case_id=case.case_id,
        kind=case.kind,
        turns=3,
    )
    trajectory._judge_transcript = [
        {"role": "student", "content": "我不知道从哪里开始。"},
        {"role": "mentor", "content": "先选一条公开现象，说说你想验证什么。"},
        {"role": "student", "content": "我想先看请求耗时。"},
        {"role": "mentor", "content": "可以，先核对耗时变化发生在哪个范围。"},
        {"role": "student", "content": "只在订单列表接口。"},
        {"role": "mentor", "content": "这个范围已经收窄，再补一条直接观察。"},
    ]
    report = await run_transcript_judge(
        MatrixReport("glm", [trajectory]),
        TestModel(custom_output_args={"reason": "能适应新手并逐轮推进。", "pass": True, "score": 1.0}),
        judge_provider="test",
    )
    public = report.public_dict()
    assert public["total"] == 1
    assert public["passed"] == 1
    assert public["failures"] == []
    serialized = json.dumps(public, ensure_ascii=False)
    assert "transcript" not in serialized
    assert "能适应新手" not in serialized


def test_provider_comparison_accepts_equivalent_behavior_without_ranking() -> None:
    case = ADAPTIVE_TRAJECTORIES[0]
    left = TrajectoryReport(
        provider="deepseek",
        question_id="hw-db-index-001",
        case_id=case.case_id,
        kind=case.kind,
        turns=3,
        behavior=BehaviorMetrics(
            sentence_repetition_rate=0.2,
            question_type_entropy=1.0,
            max_stalled_turns=2,
            action_count=1,
            evidence_count=1,
            ruled_out_count=0,
            compare_answer_calls=0,
        ),
    )
    right = TrajectoryReport(
        provider="glm",
        question_id="hw-db-index-001",
        case_id=case.case_id,
        kind=case.kind,
        turns=3,
        behavior=BehaviorMetrics(
            sentence_repetition_rate=0.4,
            question_type_entropy=1.5,
            max_stalled_turns=1,
            action_count=1,
            evidence_count=1,
            ruled_out_count=0,
            compare_answer_calls=0,
        ),
    )

    report = compare_provider_matrices(
        MatrixReport("deepseek", [left]),
        MatrixReport("glm", [right]),
        question_ids=("hw-db-index-001",),
        trajectories=(case,),
    )

    assert report.status == "passed"
    public = report.public_dict()
    assert public["hard_consistency"] is True
    assert public["behavior_equivalence"] is True
    assert public["providers"] == ["deepseek", "glm"]
    assert "duration_ms" not in public["trajectories"][0]["deltas"]


def test_provider_comparison_marks_missing_and_behavior_differences() -> None:
    case = ADAPTIVE_TRAJECTORIES[0]
    left = TrajectoryReport(
        provider="deepseek",
        question_id="hw-db-index-001",
        case_id=case.case_id,
        kind=case.kind,
        turns=3,
        behavior=BehaviorMetrics(action_count=2, completion_observed=True),
    )
    right = TrajectoryReport(
        provider="glm",
        question_id="hw-db-index-001",
        case_id=case.case_id,
        kind=case.kind,
        turns=3,
        behavior=BehaviorMetrics(action_count=1, completion_observed=False),
    )

    failed = compare_provider_matrices(
        MatrixReport("deepseek", [left]),
        MatrixReport("glm", [right]),
        question_ids=("hw-db-index-001",),
        trajectories=(case,),
    )
    assert failed.status == "failed"
    assert set(failed.comparisons[0].codes) >= {
        "behavior:action_count",
        "behavior:completion_observed",
    }

    incomplete = compare_provider_matrices(
        MatrixReport("deepseek", [left]),
        None,
        question_ids=("hw-db-index-001",),
        trajectories=(case,),
    )
    assert incomplete.status == "insufficient_data"
    assert incomplete.unavailable_providers == ["glm"]
    assert incomplete.missing == ["hw-db-index-001/novice:glm"]
