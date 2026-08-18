import asyncio
import json

import pytest
from pydantic_ai.models.test import TestModel

from hiddenworld.agents.interpreter import create_interpreter_agent
from hiddenworld.agents.mentor import create_mentor_agent
from hiddenworld.contracts import AgentTurnRequest
from hiddenworld.runtime import HiddenWorldRuntime, TurnDeadlineExceeded


@pytest.mark.asyncio
async def test_runtime_turn_returns_typed_proposals_without_mutating_input_state(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    interpreter = create_interpreter_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "actions": ["inspect:metrics.cpu"],
                    "hypothesis_id": "H_CPU_BOUND",
                    "hypothesis_raw": "",
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
    )
    mentor = create_mentor_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "reply": "CPU 和内存都在正常区间，这条观察排除了什么方向？",
                    "rationale": "让学生使用刚获得的阴性观察继续推理。",
                    "requested_releases": [],
                    "confirms_hypothesis": False,
                    "expected_effort": "quick",
                },
                ensure_ascii=False,
            )
        )
    )
    request = AgentTurnRequest(
        request_id="request-1",
        session_id="session-1",
        state_revision=7,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="先看看数据库 CPU",
    )

    result = await HiddenWorldRuntime(interpreter=interpreter, mentor=mentor).run_turn(request)

    assert learner_state.collected_evidence == []
    assert result.request_id == "request-1"
    assert result.expected_revision == 7
    assert result.turn_analysis.actions == ["inspect:metrics.cpu"]
    assert result.internal_verification.relation == "ruled_out"
    assert result.internal_verification.ruled_out_this_turn == ["H_CPU_BOUND"]
    proposal_pairs = {
        (item.kind, item.evidence_id or item.hypothesis_id or item.action)
        for item in result.proposals
    }
    assert ("release_evidence", "E_CPU_NORMAL") in proposal_pairs
    assert ("rule_out_hypothesis", "H_CPU_BOUND") in proposal_pairs
    assert ("record_action", "inspect:metrics.cpu") in proposal_pairs
    assert result.reply.startswith("CPU 和内存")
    assert all(item.tool_name != "compare_answer" for item in result.public_trace)


@pytest.mark.asyncio
async def test_runtime_executes_compare_answer_only_for_answer_attempt(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    learner_state.collected_evidence = ["E_RELEASE_LOG", "E_DDL_DIFF"]
    answer = "根因是发布脚本重建 orders 表时漏掉了 idx_user_created 索引。"
    interpreter = create_interpreter_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "actions": [],
                    "hypothesis_id": "H_INDEX",
                    "hypothesis_raw": "",
                    "made_claim": True,
                    "contains_answer_attempt": True,
                    "answer_attempt_text": answer,
                    "established_facts": [],
                    "is_stuck": False,
                    "is_noise": False,
                    "student_affect": "engaged",
                    "confidence": 0.98,
                },
                ensure_ascii=False,
            )
        )
    )
    mentor = create_mentor_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "reply": "证据链已经闭合，可以进入复盘。",
                    "rationale": "学生给出的结论已有完整观察支持。",
                    "requested_releases": [],
                    "confirms_hypothesis": True,
                    "expected_effort": "quick",
                },
                ensure_ascii=False,
            )
        )
    )
    request = AgentTurnRequest(
        request_id="request-answer",
        session_id="session-1",
        state_revision=8,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message=answer,
    )

    result = await HiddenWorldRuntime(interpreter=interpreter, mentor=mentor).run_turn(request)

    comparison = result.internal_verification.answer_comparison
    assert comparison is not None
    assert comparison.answer_attempt_id == "request-answer:answer"
    assert comparison.completion_allowed is True
    assert [item.tool_name for item in result.public_trace if item.tool_name] == ["compare_answer"]


@pytest.mark.asyncio
async def test_runtime_enforces_total_turn_deadline(hidden_world, learner_state, public_scenario) -> None:
    class SlowInterpreter:
        async def run(self, *_args, **_kwargs):
            await asyncio.sleep(0.05)

    request = AgentTurnRequest(
        request_id="request-timeout",
        session_id="session-1",
        state_revision=9,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="先看看 CPU",
        budget={"deadline_ms": 1, "max_releases": 3},
    )

    with pytest.raises(TurnDeadlineExceeded):
        await HiddenWorldRuntime(interpreter=SlowInterpreter(), mentor=object()).run_turn(request)


@pytest.mark.asyncio
async def test_low_confidence_answer_signal_does_not_call_tool_or_write_hypothesis(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    answer = "我猜可能是索引问题。"
    interpreter = create_interpreter_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "actions": ["inspect:data.explain"],
                    "hypothesis_id": "H_INDEX",
                    "hypothesis_raw": "",
                    "made_claim": True,
                    "contains_answer_attempt": True,
                    "answer_attempt_text": answer,
                    "established_facts": [],
                    "is_stuck": False,
                    "is_noise": False,
                    "student_affect": "confused",
                    "confidence": 0.2,
                },
                ensure_ascii=False,
            )
        )
    )
    mentor = create_mentor_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "reply": "你可以先说明想验证哪条可观察现象。",
                    "rationale": "解析置信度较低，只做澄清。",
                    "requested_releases": [],
                    "confirms_hypothesis": False,
                    "expected_effort": "quick",
                },
                ensure_ascii=False,
            )
        )
    )
    request = AgentTurnRequest(
        request_id="request-low-confidence",
        session_id="session-1",
        state_revision=10,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message=answer,
    )

    result = await HiddenWorldRuntime(interpreter=interpreter, mentor=mentor).run_turn(request)

    assert result.internal_verification.answer_comparison is None
    assert all(item.tool_name != "compare_answer" for item in result.public_trace)
    assert all(item.kind != "set_current_hypothesis" for item in result.proposals)
    assert all(item.kind != "record_action" for item in result.proposals)
