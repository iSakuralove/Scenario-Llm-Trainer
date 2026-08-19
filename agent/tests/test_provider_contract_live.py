"""真实 DeepSeek / GLM 契约验收。

默认不运行；需要在当前 shell 显式提供 ``DEEPSEEK_API_KEY`` 和/或
``ZAI_API_KEY`` / ``GLM_API_KEY``，再执行 ``pytest -m live``。测试只断言跨 provider 的硬契约，
不比较自然语言文案、步骤数或模型排名。
"""

from __future__ import annotations

import os

import pytest
from pydantic_ai import models

from hiddenworld.agents.interpreter import create_interpreter_agent
from hiddenworld.agents.mentor import create_mentor_agent
from hiddenworld.agents.models import build_deepseek_model, build_glm_model
from hiddenworld.bank.loader import load_fixed_question
from hiddenworld.contracts import AgentTurnRequest, LearnerState
from hiddenworld.runtime import HiddenWorldRuntime

pytestmark = pytest.mark.live


def _provider_cases() -> list[tuple[str, str]]:
    cases: list[tuple[str, str]] = []
    if os.getenv("DEEPSEEK_API_KEY"):
        cases.append(("deepseek", os.environ["DEEPSEEK_API_KEY"]))
    glm_key = os.getenv("ZAI_API_KEY") or os.getenv("GLM_API_KEY")
    if glm_key:
        cases.append(("glm", glm_key))
    return cases


@pytest.fixture(params=("deepseek", "glm"))
def live_provider(request: pytest.FixtureRequest):
    keys = dict(_provider_cases())
    provider = str(request.param)
    if provider not in keys:
        variable = "DEEPSEEK_API_KEY" if provider == "deepseek" else "ZAI_API_KEY or GLM_API_KEY"
        pytest.skip(f"未提供 {variable}，跳过真实 provider 契约测试")
    models.ALLOW_MODEL_REQUESTS = True
    if provider == "deepseek":
        return provider, build_deepseek_model(api_key=keys[provider])
    return provider, build_glm_model(api_key=keys[provider])


@pytest.mark.asyncio
async def test_live_fixed_and_adaptive_trajectories_keep_hard_contract(live_provider) -> None:
    provider, model = live_provider
    question = load_fixed_question("hw-db-index-001")
    runtime = HiddenWorldRuntime(
        interpreter=create_interpreter_agent(model),
        mentor=create_mentor_agent(model),
    )

    fixed = await runtime.run_turn(
        AgentTurnRequest(
            request_id=f"live-{provider}-fixed",
            session_id=f"live-{provider}",
            state_revision=0,
            public_scenario=question.public_scenario,
            hidden_world=question.hidden_world,
            learner_state=LearnerState(),
            user_message="先检查慢查询，再根据公开结果决定下一步。",
        )
    )
    adaptive = await runtime.run_turn(
        AgentTurnRequest(
            request_id=f"live-{provider}-adaptive",
            session_id=f"live-{provider}",
            state_revision=1,
            public_scenario=question.public_scenario,
            hidden_world=question.hidden_world,
            learner_state=LearnerState(),
            user_message="我有点不知道从哪里开始，能先整理一下现象吗？",
        )
    )

    for result in (fixed, adaptive):
        assert result.contract_version == "hiddenworld.v1"
        assert result.expected_revision in (0, 1)
        assert result.reply
        assert (
            result.internal_verification.answer_comparison is None
            or result.internal_verification.answer_comparison.answer_attempt_id
        )
        sequences = [event.sequence for event in result.public_trace]
        assert sequences == sorted(sequences)
        assert len(sequences) == len(set(sequences))
        public_json = " ".join(event.model_dump_json() for event in result.public_trace)
        for forbidden in ("correct", "target", "claim_alignment", "root_cause", "reasoning_content"):
            assert forbidden not in public_json

    assert fixed.request_id != adaptive.request_id
