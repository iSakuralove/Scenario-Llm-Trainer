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
你是排查训练中的唯一对话代理，负责理解学生当前消息、做出教学决策、决定是否请求公开工具、并生成最终回复。
你只能依据 AgentContext 中的公开题面、对话记录、学生状态、工具目录、已授权动作和工具结果工作，
不能猜测、补写或确认任何未展示的答案与内部判断。

输出必须严格二选一：
1. kind=tool_calls：先给一句极短的 public_summary，再给本轮要请求的工具调用；工具只能从 action_catalog 选择，
   观察类工具必须与 authorized_actions 中的 action_ref 对应，不能凭规划获得新权限。
   如果用户明确要求查看 action_catalog 中的公开对象，Runtime 会先把唯一匹配的
   用户授权放入 authorized_actions；此时应输出 tool_calls，不要只返回“已记录”。
2. kind=final_reply：给出一段面向学生的自然中文回复，不泄露内部实现、裁判过程或隐藏事实。

无论选择哪一分支，都必须填写结构化的 turn_assessment 与 teaching_decision。turn_assessment 描述学生当前
在做什么（intent、user_goal、requested_action、hypothesis、claim_type、progress_assessment、stuck、off_topic、
answer_attempt、confidence），teaching_decision 描述教学状态和回复策略（teaching_state、strategy、reply_policy、
guidance_direction）。旧的扁平语义字段也要保持兼容，但不得用它们替代结构化对象。它们是 Runtime 的安全归约信号，
不是思维链；不能填写答案原文、标准答案匹配关系或隐藏证据。
AgentContext.hypothesis_catalog 是一份未标注的候选表，只含 hypothesis_id 与学生可理解的 label，
不包含哪个候选正确、证据如何支持或隐藏答案。学生明确提出某个候选方向时，使用候选表中的精确
hypothesis_id；如果是候选表之外的自由方向，且表中存在 H_OTHER，则使用 H_OTHER 并保留 hypothesis_raw；
没有形成假设时两个字段都留空。不得根据候选顺序猜测正确性。
假设候选表必须按“理解后连接”的方式使用：先判断学生这轮是否真的提出了方向，再连接到表中
语义相符的候选 ID；不能因为字段有默认值就把已提出的方向留成空字符串。若无法安全连接到
某个候选，明确使用 H_OTHER，并把学生自己的说法放入 hypothesis_raw。候选表只用于连接，
不能据此推断哪一个方向正确。
final_reply 还必须填写 reply_mode：正常承接公开观察用 observation，工具没有形成观察用 no_observation，
澄清问题用 clarification，需要反思时用 reflection，闲聊用 casual，普通确认用 acknowledgement；
reply_mode 只是结构化行为声明，最终是否允许由 Runtime 按工具状态复核。

工具结果回注后，再决定是否继续请求工具或直接回复；注意预算，避免重复调用。
工具结果的 status 必须按事实处理：只有 succeeded 且带有 content 时才表示公开观察已经形成，
failed、rejected、unsupported、timeout 或 already_completed 都不表示本轮获得了新的观察。
工具结果会作为公开观察事件展示。final_reply 可以用自然语言承接学生刚看到的公开观察，
但不能复述完整工具结果、日志、指标或配置内容，也不能把工具结果改写成固定确认句。不要输出“下一步”“接下来”“建议检查”、
“排除范围”“问题不在”等明确排查路径或排除结论。可以给出中性的确认、闲聊回复，或不指向具体动作的反思性提问；
不要把工具调用、学生意图或系统动作写成“已记录”“已确认”“已完成检查”；也不要把失败或未形成观察的工具结果
写成已经得到日志、指标或数据。不要重新问学生从哪里入手、让学生选择检查入口，也不要把本轮回复写成场景开场白。
如果 AgentContext.evidence_request 标记 requested_action 对应的证据不在当前题目的公开模拟数据中，只能诚实说明该请求没有可用的题面证据，
不能提到工具目录、可用工具、权限、系统实现，也不能借此引导学生改查其它对象。
如果工具结果 status=failed 且 error_code=unmet_prerequisite，说明本次动作没有形成公开观察；
只能承认本次没有得到可用观察，不能说题面没有该证据、不能说观察已记录，也不能替学生指定其它检查。
如果 AgentContext.reply_feedback 非空，说明上一版回复未通过公开回复边界校验；只重写 reply，
保留本轮真实语义和教学决策，不解释校验过程，不复述反馈内容。
teaching_decision.allow_explicit_next_step 与 allow_ruled_out_scope 必须始终为 false。不得宣布根因、泄露未公开事实、
替学生执行未授权工具，或把未查询的具体工具当成已经执行。
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

    def __init__(
        self,
        agent: Agent[AgentContext, AgentOutputEnvelope],
        *,
        fallback_agent: Agent[AgentContext, AgentOutputEnvelope] | None = None,
    ) -> None:
        self.agent = agent
        self.fallback_agent = fallback_agent

    async def run(self, context: AgentContext) -> AgentModelOutput:
        try:
            result = await self.agent.run(_model_prompt(context), deps=context)
        except Exception:
            if self.fallback_agent is None:
                raise
            result = await self.fallback_agent.run(_model_prompt(context), deps=context)
        return result.output.to_contract()

    async def run_stream(
        self,
        context: AgentContext,
        *,
        on_reply_delta: Callable[[str], Awaitable[None]] | None = None,
        on_reasoning_delta: Callable[[str], Awaitable[None]] | None = None,
    ) -> AgentModelOutput:
        """流式消费 reply；测试开关打开时可额外转发原始 thinking 增量。

        ``on_reasoning_delta`` 只由测试调试入口显式传入。生产 Runtime 不传这个
        回调，因此模型的 ThinkingPartDelta 不会进入正式事件、历史或公开回复。
        """

        if on_reply_delta is None and on_reasoning_delta is None:
            return await self.run(context)
        try:
            return await self._run_stream_once(
                self.agent,
                context,
                on_reply_delta=on_reply_delta,
                on_reasoning_delta=on_reasoning_delta,
            )
        except Exception:
            if self.fallback_agent is None:
                raise
            # SingleAgentRuntime 会先把正文增量放入私有缓冲，故主模型失败后
            # 回退不会把半截不安全正文发送给学生；测试调试流可见的原始 thinking
            # 则按用户明确允许的测试语义保留。
            return await self._run_stream_once(
                self.fallback_agent,
                context,
                on_reply_delta=on_reply_delta,
                on_reasoning_delta=on_reasoning_delta,
            )

    async def _run_stream_once(
        self,
        agent: Agent[AgentContext, AgentOutputEnvelope],
        context: AgentContext,
        *,
        on_reply_delta: Callable[[str], Awaitable[None]] | None,
        on_reasoning_delta: Callable[[str], Awaitable[None]] | None,
    ) -> AgentModelOutput:
        extractor = StreamingFieldExtractor("reply") if on_reply_delta is not None else None
        async with agent.iter(_model_prompt(context), deps=context) as run:
            async for node in run:
                if not PydanticAgent.is_model_request_node(node):
                    continue
                async with node.stream(run.ctx) as stream:
                    async for event in stream:
                        if not isinstance(event, pai_messages.PartDeltaEvent):
                            continue
                        delta = event.delta
                        if isinstance(delta, pai_messages.ThinkingPartDelta):
                            piece = delta.content_delta or ""
                            if piece and on_reasoning_delta is not None:
                                await on_reasoning_delta(piece)
                            continue
                        if not isinstance(delta, pai_messages.TextPartDelta) or extractor is None:
                            continue
                        piece = extractor.feed(delta.content_delta)
                        if piece and on_reply_delta is not None:
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


def create_scenario_agent(
    model: Any = None,
    *,
    fallback_model: Any = None,
) -> Agent[AgentContext, AgentOutputEnvelope]:
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


def create_scenario_agent_runner(
    model: Any = None,
    *,
    fallback_model: Any = None,
) -> PydanticScenarioAgentRunner:
    """创建可直接交给 ``scenario_runtime.AgentLoop`` 的模型适配器。"""

    return PydanticScenarioAgentRunner(
        create_scenario_agent(model),
        fallback_agent=create_scenario_agent(fallback_model) if fallback_model is not None else None,
    )
