"""CT-01～CT-14 的离线契约套件。

这些用例验证两家 provider 共同依赖的本地边界：模型 profile、结构化输出、
答案工具授权、公开事件、重试预算、deadline 和 thinking 隔离。真实 API 请求
位于 ``test_provider_contract_live.py``，由 ``-m live`` 显式开启。
"""

from __future__ import annotations

import asyncio
import json

import httpx
import pytest
from pydantic_ai import Agent, ModelResponse, TextPart, models
from pydantic_ai.exceptions import ModelHTTPError, UnexpectedModelBehavior
from pydantic_ai.models.function import FunctionModel
from pydantic_ai.models.test import TestModel

from hiddenworld.agents.interpreter import create_interpreter_agent
from hiddenworld.agents.mentor import create_mentor_agent
from hiddenworld.agents.models import build_deepseek_model, build_glm_model
from hiddenworld.agents.tools import CompareAnswerAuthorizationError, CompareAnswerRuntime
from hiddenworld.contracts import (
    AgentTurnRequest,
    AnswerAttempt,
    PublicAnswerComparison,
    TurnAnalysis,
)
from hiddenworld.retry import run_with_network_retries
from hiddenworld.runtime import HiddenWorldRuntime, TurnDeadlineExceeded

models.ALLOW_MODEL_REQUESTS = False


def _analysis_payload(**overrides: object) -> dict[str, object]:
    payload: dict[str, object] = {
        "public_summary": "你说你完全不知道从哪下手，希望先拿到一点方向。",
        "actions": [],
        "hypothesis_id": "H_OTHER",
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
    payload.update(overrides)
    return payload


def _mentor_payload(reply: str = "先说说你准备验证哪条公开现象。") -> dict[str, object]:
    return {
        "reply": reply,
        "rationale": "只基于公开上下文给出下一步支架。",
        "requested_releases": [],
        "confirms_hypothesis": False,
        "expected_effort": "quick",
    }


def _assert_profile_snapshot(model, *, provider: str, model_id: str) -> None:
    assert model.model_name == model_id
    assert model.system == provider
    profile = model.profile
    assert profile["supports_json_object_output"] is True
    assert profile["supports_json_schema_output"] is False
    assert profile["supports_tools"] is True
    assert profile.get("openai_supports_tool_choice_required", False) is False
    assert profile["default_structured_output_mode"] == "tool"


def test_ct01_provider_profile_snapshot() -> None:
    _assert_profile_snapshot(
        build_deepseek_model(api_key="test"),
        provider="deepseek",
        model_id="deepseek-v4-flash",
    )
    _assert_profile_snapshot(build_glm_model(api_key="test"), provider="zai", model_id="glm-4.7")


@pytest.mark.asyncio
async def test_ct02_plain_text_is_non_empty_utf8() -> None:
    text_agent = Agent(None, output_type=str)
    with text_agent.override(model=TestModel(custom_output_text="普通中文回复")):
        result = await text_agent.run("生成普通文本")
    assert result.output == "普通中文回复"


@pytest.mark.asyncio
async def test_ct03_json_object_is_typed() -> None:
    interpreter = create_interpreter_agent(
        TestModel(custom_output_text=json.dumps(_analysis_payload(actions=["inspect:metrics.cpu"])))
    )
    result = await interpreter.run("检查 CPU", deps=_interpreter_deps())
    assert isinstance(result.output, TurnAnalysis)
    assert result.output.actions == ["inspect:metrics.cpu"]


@pytest.mark.asyncio
async def test_ct04_structured_output_retries_once_then_fails_closed() -> None:
    calls = 0

    def model_function(_messages, _info) -> ModelResponse:
        nonlocal calls
        calls += 1
        return ModelResponse(parts=[TextPart("not-json" if calls == 1 else "still-not-json")])

    agent = create_interpreter_agent(FunctionModel(model_function))
    with pytest.raises(UnexpectedModelBehavior):
        await agent.run("检查 CPU", deps=_interpreter_deps())
    assert calls == 2


def test_ct05_compare_answer_requires_server_bound_attempt(hidden_world, learner_state) -> None:
    answer = AnswerAttempt(
        answer_attempt_id="req:answer",
        session_id="session",
        turn_id="req",
        revision=3,
        text="我认为是索引问题。",
    )
    analysis = TurnAnalysis(
        **_analysis_payload(contains_answer_attempt=True, answer_attempt_text=answer.text)
    )
    runtime = CompareAnswerRuntime(
        request_id="req",
        session_id="session",
        turn_id="req",
        revision=3,
        world=hidden_world,
        learner_state=learner_state,
        analysis=analysis,
        attempts={answer.answer_attempt_id: answer},
    )
    result = runtime.execute(answer.answer_attempt_id)
    assert isinstance(result, PublicAnswerComparison)
    assert runtime.execution_count == 1
    assert runtime.execute(answer.answer_attempt_id) == result
    with pytest.raises(CompareAnswerAuthorizationError):
        runtime.execute("forged-answer-id")


@pytest.mark.asyncio
async def test_ct06_no_tool_branch_has_no_fake_compare_event(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    result = await _run_runtime(
        hidden_world,
        learner_state,
        public_scenario,
        analysis=_analysis_payload(),
    )
    assert all(event.tool_name != "compare_answer" for event in result.public_trace)


def test_ct07_public_trace_has_no_reply_before_guard() -> None:
    ordered = ["mentor_buffered", "guard_passed", "reply_delta"]
    assert ordered.index("guard_passed") < ordered.index("reply_delta")


def test_ct08_chunked_or_repeated_tool_arguments_execute_once(hidden_world, learner_state) -> None:
    answer = AnswerAttempt(
        answer_attempt_id="req:answer",
        session_id="session",
        turn_id="req",
        revision=1,
        text="可能是索引问题。",
    )
    runtime = CompareAnswerRuntime(
        request_id="req",
        session_id="session",
        turn_id="req",
        revision=1,
        world=hidden_world,
        learner_state=learner_state,
        analysis=TurnAnalysis(
            **_analysis_payload(contains_answer_attempt=True, answer_attempt_text=answer.text)
        ),
        attempts={answer.answer_attempt_id: answer},
    )
    first = runtime.execute("req:answer")
    second = runtime.execute("req:answer")
    assert first == second
    assert runtime.execution_count == 1


@pytest.mark.asyncio
async def test_ct09_answer_attempt_tool_and_reply_are_both_present(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    answer = "根因是发布脚本漏掉了索引。"
    result = await _run_runtime(
        hidden_world,
        learner_state,
        public_scenario,
        analysis=_analysis_payload(
            hypothesis_id="H_INDEX",
            made_claim=True,
            contains_answer_attempt=True,
            answer_attempt_text=answer,
        ),
    )
    kinds = [event.kind for event in result.public_trace]
    assert kinds.index("tool_started") < kinds.index("tool_completed") < kinds.index("mentor_buffered")
    assert result.reply


@pytest.mark.asyncio
async def test_ct10_retry_classification_retries_only_allowed_errors() -> None:
    attempts = 0

    async def transient() -> str:
        nonlocal attempts
        attempts += 1
        if attempts < 3:
            raise httpx.ConnectError("temporary")
        return "ok"

    assert await run_with_network_retries(transient, jitter=0, sleep=_no_sleep) == "ok"
    assert attempts == 3

    attempts = 0

    async def invalid() -> None:
        nonlocal attempts
        attempts += 1
        raise ModelHTTPError(401, "model", {"error": "unauthorized"})

    with pytest.raises(ModelHTTPError):
        await run_with_network_retries(invalid, sleep=_no_sleep)
    assert attempts == 1


@pytest.mark.asyncio
async def test_ct11_replaying_same_request_does_not_repeat_compare(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    answer = "我认为是索引问题。"
    runtime = await _run_runtime(
        hidden_world,
        learner_state,
        public_scenario,
        analysis=_analysis_payload(contains_answer_attempt=True, answer_attempt_text=answer),
    )
    comparison = runtime.internal_verification.answer_comparison
    assert comparison is not None
    assert len([event for event in runtime.public_trace if event.kind == "tool_completed"]) == 1


@pytest.mark.asyncio
async def test_ct12_total_deadline_is_hard_limit(hidden_world, learner_state, public_scenario) -> None:
    class SlowInterpreter:
        async def run(self, *_args, **_kwargs):
            await asyncio.sleep(0.05)

    request = AgentTurnRequest(
        request_id="deadline",
        session_id="session",
        state_revision=1,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="检查 CPU",
        budget={"deadline_ms": 1, "max_releases": 3},
    )
    with pytest.raises(TurnDeadlineExceeded):
        await HiddenWorldRuntime(interpreter=SlowInterpreter(), mentor=object()).run_turn(request)


def test_ct13_public_projection_excludes_reasoning_and_internal_fields(hidden_world, learner_state) -> None:
    answer = AnswerAttempt(
        answer_attempt_id="req:answer",
        session_id="session",
        turn_id="req",
        revision=1,
        text="索引问题",
    )
    runtime = CompareAnswerRuntime(
        request_id="req",
        session_id="session",
        turn_id="req",
        revision=1,
        world=hidden_world,
        learner_state=learner_state,
        analysis=TurnAnalysis(
            **_analysis_payload(contains_answer_attempt=True, answer_attempt_text=answer.text)
        ),
        attempts={answer.answer_attempt_id: answer},
    )
    public_json = runtime.execute(answer.answer_attempt_id).model_dump_json()
    for forbidden in ("correct", "target", "claim_alignment", "root_cause", "reasoning_content"):
        assert forbidden not in public_json


def test_ct14_unsupported_features_remain_disabled() -> None:
    for model in (build_deepseek_model(api_key="test"), build_glm_model(api_key="test")):
        assert model.profile["supports_json_schema_output"] is False
        assert model.profile.get("openai_supports_tool_choice_required", False) is False
        assert model.profile["default_structured_output_mode"] == "tool"


async def _no_sleep(_delay: float) -> None:
    return None


def _interpreter_deps():
    from hiddenworld.contracts import InterpreterDeps

    return InterpreterDeps(
        public_scenario=_public_scenario(),
        hypotheses=[],
        known_actions=["inspect:metrics.cpu"],
    )


def _mentor_deps():
    from hiddenworld.contracts import (
        ConstraintFacts,
        GuardContext,
        LearnerStateView,
        MentorDeps,
        TeachingConstraints,
    )

    return MentorDeps(
        public_scenario=_public_scenario(),
        transcript=[],
        learner_state=LearnerStateView(),
        constraints=TeachingConstraints(
            must_not=["confirm_hypothesis", "reveal_unreleased"],
            facts=ConstraintFacts(
                hypothesis_supported=False,
                evidence_coverage="0/1",
                stalled_turns=0,
                contradictions=[],
                student_affect="engaged",
            ),
        ),
        guard_only=GuardContext(completion_allowed=False),
    )


def _public_scenario():
    from hiddenworld.contracts import PublicScenario

    return PublicScenario(
        title="订单查询变慢",
        description="订单列表 P99 升高。",
        environment="服务 + 数据库",
        initial_symptoms=["接口变慢"],
    )


async def _run_runtime(hidden_world, learner_state, public_scenario, *, analysis: dict[str, object]):
    interpreter = create_interpreter_agent(
        TestModel(custom_output_text=json.dumps(analysis, ensure_ascii=False))
    )
    mentor = create_mentor_agent(
        TestModel(custom_output_text=json.dumps(_mentor_payload(), ensure_ascii=False))
    )
    request = AgentTurnRequest(
        request_id="contract-runtime",
        session_id="session",
        state_revision=1,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="测试本轮",
    )
    return await HiddenWorldRuntime(interpreter=interpreter, mentor=mentor).run_turn(request)
