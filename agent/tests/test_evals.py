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
    GoldenCase,
    apply_proposals_for_eval,
    load_golden_cases,
    run_interpreter_goldens,
    run_trajectory,
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
    assert len(cases) >= 12
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
