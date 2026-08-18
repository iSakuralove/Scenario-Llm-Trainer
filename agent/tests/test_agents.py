import json

import pytest
from pydantic_ai import ModelResponse, TextPart, models
from pydantic_ai.models.function import FunctionModel
from pydantic_ai.models.test import TestModel

from hiddenworld.agents.interpreter import build_interpreter_prompt, create_interpreter_agent
from hiddenworld.agents.mentor import build_mentor_prompt, create_mentor_agent
from hiddenworld.contracts import (
    GuardContext,
    InterpreterDeps,
    LearnerStateView,
    MentorDeps,
    PublicAnswerComparison,
)

models.ALLOW_MODEL_REQUESTS = False


def test_interpreter_prompt_contains_only_unlabelled_candidates_and_public_context(
    hidden_world,
    public_scenario,
) -> None:
    deps = InterpreterDeps(
        public_scenario=public_scenario,
        hypotheses=hidden_world.hypotheses,
        known_actions=[item.action for item in hidden_world.observations],
    )

    prompt = build_interpreter_prompt(deps)

    assert public_scenario.title in prompt
    assert "H_INDEX" in prompt
    assert "索引问题" in prompt
    assert "inspect:data.explain" in prompt
    assert hidden_world.root_cause.description not in prompt
    assert "accepted_hypotheses" not in prompt
    assert "is_correct" not in prompt


@pytest.mark.asyncio
async def test_interpreter_agent_returns_typed_turn_analysis(hidden_world, public_scenario) -> None:
    deps = InterpreterDeps(
        public_scenario=public_scenario,
        hypotheses=hidden_world.hypotheses,
        known_actions=[item.action for item in hidden_world.observations],
    )
    model = TestModel(
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
                "confidence": 0.92,
            },
            ensure_ascii=False,
        )
    )
    agent = create_interpreter_agent()

    with agent.override(model=model):
        result = await agent.run("先看看数据库 CPU", deps=deps)

    assert result.output.actions == ["inspect:metrics.cpu"]
    assert result.output.hypothesis_id == "H_CPU_BOUND"
    assert result.output.confidence == 0.92


def test_mentor_prompt_excludes_guard_only_and_unreleased_content(
    public_scenario,
    teaching_constraints,
    hidden_world,
) -> None:
    released = hidden_world.evidence_by_id("E_CPU_NORMAL")
    hidden = hidden_world.evidence_by_id("E_DDL_DIFF")
    assert released is not None and hidden is not None
    deps = MentorDeps(
        public_scenario=public_scenario,
        transcript=[],
        learner_state=LearnerStateView(
            established_facts=["CPU 使用率正常"],
            actions_taken=["inspect:metrics.cpu"],
            ruled_out_labels=["CPU 打满"],
        ),
        constraints=teaching_constraints,
        released_evidence=[released.content],
        answer_comparison=PublicAnswerComparison(
            user_points=["我怀疑是索引问题"],
            support_status="needs_more_evidence",
            next_action="继续补充能支撑这个结论的直接观察。",
        ),
        guard_only=GuardContext(
            forbidden_entities=[hidden.content, hidden_world.root_cause.description],
            completion_allowed=False,
        ),
    )

    prompt = build_mentor_prompt(deps)

    assert public_scenario.title in prompt
    assert released.content in prompt
    assert "我怀疑是索引问题" in prompt
    assert hidden.content not in prompt
    assert hidden_world.root_cause.description not in prompt
    assert "forbidden_entities" not in prompt
    assert "guard_only" not in prompt


@pytest.mark.asyncio
async def test_mentor_agent_returns_typed_validated_action(
    public_scenario,
    teaching_constraints,
) -> None:
    deps = MentorDeps(
        public_scenario=public_scenario,
        transcript=[],
        learner_state=LearnerStateView(),
        constraints=teaching_constraints,
        guard_only=GuardContext(completion_allowed=False),
    )
    model = TestModel(
        custom_output_text=json.dumps(
            {
                "reply": "先从你最确定的一条现象开始，说说它排除了什么。",
                "rationale": "学生还没有形成可验证的证据链，先降低认知负担。",
                "requested_releases": [],
                "confirms_hypothesis": False,
                "expected_effort": "quick",
            },
            ensure_ascii=False,
        )
    )
    agent = create_mentor_agent()

    with agent.override(model=model):
        result = await agent.run("请生成本轮导师回复", deps=deps)

    assert result.output.reply.startswith("先从你最确定")
    assert result.output.confirms_hypothesis is False
    assert result.output.expected_effort == "quick"


@pytest.mark.asyncio
async def test_mentor_guard_retries_once_after_premature_confirmation(
    public_scenario,
    teaching_constraints,
) -> None:
    calls = 0

    def model_function(_messages, _info) -> ModelResponse:
        nonlocal calls
        calls += 1
        payload = {
            "reply": "这个结论已经确定了。" if calls == 1 else "先补一条直接观察再判断。",
            "rationale": "测试 Guard 的有界重试。",
            "requested_releases": [],
            "confirms_hypothesis": calls == 1,
            "expected_effort": "quick",
        }
        return ModelResponse(parts=[TextPart(json.dumps(payload, ensure_ascii=False))])

    deps = MentorDeps(
        public_scenario=public_scenario,
        transcript=[],
        learner_state=LearnerStateView(),
        constraints=teaching_constraints,
        guard_only=GuardContext(completion_allowed=False),
    )
    agent = create_mentor_agent(FunctionModel(model_function))

    result = await agent.run("请生成本轮导师回复", deps=deps)

    assert calls == 2
    assert result.output.confirms_hypothesis is False
    assert result.output.reply == "先补一条直接观察再判断。"
