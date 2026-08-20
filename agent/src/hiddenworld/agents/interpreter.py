"""TurnInterpreter：把学生自由文本转换为类型化 TurnAnalysis。"""

from __future__ import annotations

import json
from typing import Any

from pydantic_ai import Agent, PromptedOutput, RunContext

from hiddenworld.contracts import InterpreterDeps, TurnAnalysis

INTERPRETER_INSTRUCTIONS = """
你是排查训练中的消息解释器，不与学生对话，也不生成教学回复。
只根据学生当前消息和公开上下文，输出 TurnAnalysis 所要求的 JSON 对象。
候选假设没有正确性标记；不得猜测或补写标准答案。
动作只能从 known_actions 中选择。候选表外的想法使用 H_OTHER 并保留原话。
「不知道该从哪看起」是卡住，不是噪声。没有足够把握时降低 confidence。
如果学生是在追问上一句的含义或要求解释公开信息，intent 使用 clarification 或 explanation_request，
不要把这类消息伪装成新的检查动作。
只有在学生请求与 known_actions 明确对应时才填 actions；不要因为主题相近就替换成最近动作。
无法匹配或存在多个可能动作时，actions 给空数组，保留 requested_action_raw，并将 action_match_status 设为 unsupported 或 ambiguous。
""".strip()


def build_interpreter_prompt(deps: InterpreterDeps) -> str:
    """渲染模型可见的完整 instructions；刻意不接收 HiddenWorld。"""

    context = {
        "public_scenario": deps.public_scenario.model_dump(mode="json"),
        "hypotheses": [item.model_dump(mode="json") for item in deps.hypotheses],
        "known_actions": list(deps.known_actions),
        "virtual_tools": [
            {
                "tool_id": item.tool_id,
                "kind": item.kind,
                "target": item.target,
                "aliases": item.aliases,
                "query_patterns": item.query_patterns,
                "observation_action": item.observation_action,
            }
            for item in deps.virtual_tools
        ],
        "transcript": [item.model_dump(mode="json") for item in deps.transcript],
    }
    return f"{INTERPRETER_INSTRUCTIONS}\n\n公开上下文：\n{json.dumps(context, ensure_ascii=False)}"


def _interpreter_instructions(ctx: RunContext[InterpreterDeps]) -> str:
    return build_interpreter_prompt(ctx.deps)


def create_interpreter_agent(model: Any = None) -> Agent[InterpreterDeps, TurnAnalysis]:
    """创建无默认网络副作用的 Interpreter；生产和测试都显式注入模型。"""

    return Agent(
        model,
        deps_type=InterpreterDeps,
        output_type=PromptedOutput(
            TurnAnalysis,
            name="turn_analysis",
            description="把学生当前消息解释为一个 TurnAnalysis JSON 对象。",
        ),
        instructions=_interpreter_instructions,
        retries=1,
        defer_model_check=model is None,
    )
