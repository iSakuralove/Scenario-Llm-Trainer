"""Mentor：唯一生成学生可见回复的 LLM。"""

from __future__ import annotations

import json
from typing import Any

from pydantic_ai import Agent, ModelRetry, PromptedOutput, RunContext

from hiddenworld.contracts import MentorAction, MentorDeps
from hiddenworld.kernel.guard import Guard, GuardViolation

MENTOR_INSTRUCTIONS = """
你是排查训练中的导师，只能依据下面给出的公开题面、学生自己的表述、已释放观察和教学约束回复。

角色原则：
1. 这一轮不必推进排查，不必释放证据，也不必提问；最合适时可以让学生先消化。
2. 语气必须跟随 student_affect：顺利时自然，困惑时给支架，受挫时停止游戏化口吻。
3. 不得复用 recent_openings 中已经出现的开场句式。
4. 你是尊重学生推理过程的导师，不按固定检查表逐项盘问。
5. 你不负责替学生排除方向、不负责把他直接带到答案，也不负责判断结论对错。

严格遵守 must_not；只能请求 may_release 中的证据。输出 MentorAction JSON。
""".strip()


def build_mentor_prompt(deps: MentorDeps) -> str:
    """渲染 Mentor 可见的完整 instructions；绝不读取 ``guard_only``。"""

    context = {
        "public_scenario": deps.public_scenario.model_dump(mode="json"),
        "transcript": [item.model_dump(mode="json") for item in deps.transcript],
        "learner_state": deps.learner_state.model_dump(mode="json"),
        "constraints": deps.constraints.model_dump(mode="json"),
        "released_evidence": list(deps.released_evidence),
        "answer_comparison": (
            deps.answer_comparison.model_dump(mode="json")
            if deps.answer_comparison is not None
            else None
        ),
    }
    return f"{MENTOR_INSTRUCTIONS}\n\n本轮公开上下文：\n{json.dumps(context, ensure_ascii=False)}"


def _mentor_instructions(ctx: RunContext[MentorDeps]) -> str:
    return build_mentor_prompt(ctx.deps)


def create_mentor_agent(model: Any = None) -> Agent[MentorDeps, MentorAction]:
    """创建 Mentor，并把确定性 Guard 注册为终值 output validator。"""

    agent = Agent(
        model,
        deps_type=MentorDeps,
        output_type=PromptedOutput(
            MentorAction,
            name="mentor_action",
            description="生成一个符合教学约束的 MentorAction JSON 对象。",
        ),
        instructions=_mentor_instructions,
        retries=1,
        defer_model_check=model is None,
    )

    @agent.output_validator
    def validate_mentor_action(
        ctx: RunContext[MentorDeps],
        action: MentorAction,
    ) -> MentorAction:
        if ctx.partial_output:
            return action
        try:
            return Guard().validate(
                action,
                constraints=ctx.deps.constraints,
                context=ctx.deps.guard_only,
            )
        except GuardViolation as exc:
            raise ModelRetry(str(exc)) from exc

    return agent
