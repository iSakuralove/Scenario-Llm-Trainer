"""单一 ScenarioAgent：理解、规划和回复共用一个模型节点。"""

from __future__ import annotations

import json
from collections.abc import Awaitable, Callable
from typing import Any

from pydantic_ai import Agent, PromptedOutput, RunContext, messages as pai_messages
from pydantic_ai.agent import Agent as PydanticAgent

from hiddenworld.contracts import AgentContext, AgentModelOutput, AgentOutputEnvelope
from hiddenworld.streaming_json import StreamingFieldExtractor


SCENARIO_AGENT_INSTRUCTIONS = """
你是排查训练中的唯一对话代理，负责理解学生当前消息、决定是否请求公开工具、并生成最终回复。
你只能依据 AgentContext 中的公开题面、对话记录、学生状态、工具目录、已授权动作和工具结果工作，
不能猜测、补写或确认任何未展示的答案与内部判断。

输出必须严格二选一：
1. kind=tool_calls：先给一句极短的 public_summary，再给本轮要请求的工具调用；工具只能从 action_catalog 选择，
   观察类工具必须与 authorized_actions 中的 action_ref 对应，不能凭规划获得新权限；
2. kind=final_reply：给出一段面向学生的自然中文回复，不泄露内部实现、裁判过程或隐藏事实。

工具结果回注后，再决定是否继续请求工具或直接回复；注意预算，避免重复调用。
工具结果已经由 Runtime 展示，QuickActions 会表达下一步动作；如果本轮已有工具结果，最终回复只能客观转述已公开观察，
不得判断根因、说“问题已经比较清晰”、提出“建议下一步检查……”或替学生选择未查询的工具。
不要输出 reasoning、chain of thought、rationale 或任何额外字段。
""".strip()


def build_scenario_agent_prompt(context: AgentContext) -> str:
    """以安全上下文构造模型输入，当前用户消息单独保留。"""

    payload = context.model_dump(mode="json")
    return f"{SCENARIO_AGENT_INSTRUCTIONS}\n\nAgentContext：\n{json.dumps(payload, ensure_ascii=False)}"


def _scenario_agent_instructions(ctx: RunContext[AgentContext]) -> str:
    return build_scenario_agent_prompt(ctx.deps)


class PydanticScenarioAgentRunner:
    """把 PydanticAI Agent 适配成 ScenarioAgentLoop 所需的安全接口。"""

    def __init__(self, agent: Agent[AgentContext, AgentOutputEnvelope]) -> None:
        self.agent = agent

    async def run(self, context: AgentContext) -> AgentModelOutput:
        result = await self.agent.run(_model_prompt(context), deps=context)
        return result.output.to_contract()

    async def run_stream(
        self,
        context: AgentContext,
        *,
        on_reply_delta: Callable[[str], Awaitable[None]] | None = None,
    ) -> AgentModelOutput:
        """流式消费最终 reply 字段；工具规划轮不伪造正文增量。"""

        if on_reply_delta is None:
            return await self.run(context)
        extractor = StreamingFieldExtractor("reply")
        async with self.agent.iter(_model_prompt(context), deps=context) as run:
            async for node in run:
                if not PydanticAgent.is_model_request_node(node):
                    continue
                async with node.stream(run.ctx) as stream:
                    async for event in stream:
                        if not isinstance(event, pai_messages.PartDeltaEvent):
                            continue
                        delta = event.delta
                        if not isinstance(delta, pai_messages.TextPartDelta):
                            continue
                        piece = extractor.feed(delta.content_delta)
                        if piece:
                            await on_reply_delta(piece)
            return run.result.output.to_contract()


def _model_prompt(context: AgentContext) -> str:
    """保证结构化动作轮也有非空 provider prompt。

    QuickAction 的用户原文必须继续保持空字符串，不能伪造历史消息；这里只给模型
    一个固定的内部启动语句，真正的动作授权和动作 ID 仍来自 AgentContext。
    """

    if context.current_user_message.strip():
        return context.current_user_message
    if context.authorized_actions:
        return "请根据当前已授权的检查和公开上下文继续本轮处理。"
    return "请根据当前公开上下文生成本轮回复。"


def create_scenario_agent(model: Any = None) -> Agent[AgentContext, AgentOutputEnvelope]:
    """创建单 Agent；不提供 model 时仍允许测试侧延迟注入。"""

    return Agent(
        model,
        deps_type=AgentContext,
        output_type=PromptedOutput(
            AgentOutputEnvelope,
            name="scenario_agent_output",
            description="严格输出 tool_calls 或 final_reply 二选一的 JSON 对象。",
        ),
        instructions=_scenario_agent_instructions,
        retries=1,
        defer_model_check=model is None,
    )


def create_scenario_agent_runner(model: Any = None) -> PydanticScenarioAgentRunner:
    """创建可直接交给 ``scenario_runtime.AgentLoop`` 的模型适配器。"""

    return PydanticScenarioAgentRunner(create_scenario_agent(model))
