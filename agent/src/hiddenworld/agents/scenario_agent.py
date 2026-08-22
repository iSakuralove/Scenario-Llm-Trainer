"""单一 ScenarioAgent：理解、规划和回复共用一个模型节点。"""

from __future__ import annotations

import asyncio
import json
import os
from collections.abc import Awaitable, Callable
from typing import Any

from pydantic_ai import Agent, PromptedOutput, RunContext, messages as pai_messages
from pydantic_ai.agent import Agent as PydanticAgent

from hiddenworld.contracts import AgentContext, AgentModelOutput, AgentOutputEnvelope
from hiddenworld.streaming_json import StreamingFieldExtractor
from hiddenworld.scenario_runtime.response_brief import ResponseBriefBuilder


# Provider 可能把一段较长的 reasoning/text delta 合并后才交给 PydanticAI。
# 这里仅重分帧模型已经返回的真实内容，避免下游 SSE 因单个大块看起来像
# “一次性输出”；不修改内容、不插入任何面向学生的固定文本。
_STREAM_FRAME_SIZE = 24
_STREAM_FRAME_DELAY_SECONDS = 0.05


async def _emit_stream_frames(
    callback: Callable[[str], Awaitable[None]] | None,
    text: str,
) -> None:
    if callback is None or not text:
        return
    frames = [text[index : index + _STREAM_FRAME_SIZE] for index in range(0, len(text), _STREAM_FRAME_SIZE)]
    for index, frame in enumerate(frames):
        await callback(frame)
        if index < len(frames) - 1:
            await asyncio.sleep(_STREAM_FRAME_DELAY_SECONDS)


SCENARIO_AGENT_INSTRUCTIONS = """
你是排查训练中的唯一对话代理，负责理解学生当前消息、做出教学决策、决定是否请求公开工具、并生成最终回复。
你只能依据 AgentContext 中的公开题面、确定性 conversation_summary、最近四个完整回合、学生状态、
题目级 mentor_persona / concept_catalog、工具目录、已授权动作和工具结果工作，
不能猜测、补写或确认任何未展示的答案与内部判断。

输出必须严格二选一：
1. kind=tool_calls：先给一句极短的 public_summary，再给本轮要请求的工具调用；工具只能从 action_catalog 选择，
   观察类工具必须与 authorized_actions 中的 action_ref 对应，不能凭规划获得新权限。
   如果用户明确要求查看 action_catalog 中的公开对象，Runtime 会先把唯一匹配的
   用户授权放入 authorized_actions；此时应输出 tool_calls，不要只返回“已记录”。
2. kind=final_reply：给出一段面向学生的自然中文回复，不泄露内部实现、裁判过程或隐藏事实。

无论选择哪一分支，都必须填写结构化的 turn_assessment 与 teaching_decision。turn_assessment 描述学生当前
在做什么（intent、user_goal、requested_action、hypothesis、claim_type、progress_assessment、stuck、off_topic、
answer_attempt、confidence、humor/frustration/confusion/confidence/urgency、random_investigation），
teaching_decision 描述教学状态和回复策略（teaching_state、strategy、primary_task、reply_policy、guidance_direction）。
primary_task 每轮只选一个主要教学任务：解释概念 explain_concept、解释证据 interpret_evidence、确认真实进展
acknowledge_progress、纠正不完整结论 correct_conclusion、释放提示 release_hint、拉回主线 redirect_investigation、
或结束调查 close_investigation。旧的扁平语义字段也要保持兼容，但不得用它们替代结构化对象。它们是 Runtime 的安全归约信号，
不是思维链；不能填写答案原文、标准答案匹配关系或隐藏证据。
claim_type 与 made_claim 必须严格联动：claim_type 是 observation、hypothesis 或 answer 时 made_claim 必须为 true；
claim_type 是 none、question 或 meta 时必须为 false。学生只是隐含了方向但未明确断言时，仍按 claim_type 的实际取值
如实设置 made_claim，不要一个填类型一个填布尔。contains_answer_attempt 为 true 时必须给出非空 answer_attempt_text，
为 false 时不要填写文本。
guidance_direction 只能留空或使用 logs、metrics、config、change、dependency、data、resource 之一，表示公开的粗粒度关注面；
不得填写组件名、答案词、具体工具或动作 id。
concept_mastery_signals / skill_mastery_signals 表示学生本轮实际展示出的 0–4 掌握水平。只有学生正确复述、
关联或应用了概念/能力时才能填写；导师刚解释过、学生只是提问或工具自动返回结果时必须留空。概念 id 只能来自
concept_catalog，能力 id 只能是 log_reading、causal_reasoning、cross_layer_debugging。Runtime 每轮最多只会上调 1。
preference_signals 只有学生明确说出“简短些/详细些/要类比/直接说”等表达偏好时才能填写；不得从消息长短、
情绪或基础水平猜偏好。detail 只用 brief/balanced/detailed，analogy/directness 只用 low/medium/high。
当前轮输入以 AgentContext.current_turn_input 为准：如果 current_user_message 为空，表示学生通过快捷动作发起了检查，
不是“没有用户行为”；不要在 reasoning 或 reply 中说 user message is empty，也不要把该内部字段名暴露给学生。
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
AgentContext.phase 只有两种安全阶段：
- new_user_turn：这是本轮第一次理解。original_user_message 是用户本轮原话，先判断它是否明确请求了题目声明的观察。
- after_tool_call：这是同一轮的继续，不是新用户消息。original_user_message 仍是本轮原话，相关动作可能已经执行；必须优先依据
  tool_results、action_history 和 tool_states 判断哪些观察已经返回、哪些动作没有形成观察，再决定回复或是否还有必要提出受控工具调用。
不要把 after_tool_call 当成新的用户问题，不要重复调用 tool_states 中 state=consumed 的工具；reason 是 Runtime 对消费/失败/阻断原因的安全说明。
工具结果的 status 必须按事实处理：只有 succeeded 且带有 content 时才表示公开观察已经形成，
failed、rejected、unsupported、timeout 或 already_completed 都不表示本轮获得了新的观察。
工具结果会作为带来源的独立工具卡展示。工具卡负责给事实，final_reply 负责说明这条事实能证明什么、还不能证明什么；
不能复述完整工具结果、日志、指标或配置内容，也不能把工具结果改写成固定确认句。教学模拟产生的观察必须始终按
“教学模拟”理解和表达，不得删除、弱化或伪装成真实生产数据。不要输出“下一步”“接下来”“建议检查”、
“排除范围”“问题不在”等明确排查路径或排除结论。可以给出中性的确认、闲聊回复，或不指向具体动作的反思性提问；
不要把工具调用、学生意图或系统动作写成“已记录”“已确认”“已完成检查”；也不要把失败或未形成观察的工具结果
写成已经得到日志、指标或数据。不要重新问学生从哪里入手、让学生选择检查入口，也不要把本轮回复写成场景开场白。
如果 AgentContext.evidence_request 标记 requested_action 对应的证据不在当前题目的公开教学模拟数据中，只能诚实说明
该请求没有可用的题面证据；public_message 非空时以它为事实边界，不得补具体数值。不能提到工具目录、可用工具、
权限、系统实现，也不能借此引导学生改查其它对象。SIMULATED_ALLOWED 只表示题目声明了对应模拟工具，仍不得编造
工具没有返回的数值。
repair_status 是安全的教学导航信号，只允许 none、partial、sufficient 三值：none 表示尚未提出修复动作，
partial 表示修复闭环仍缺一段，sufficient 表示修复动作与验证闭环均已覆盖。只能据此调整收束语气，
不得反推出具体缺失要求、solution_coverage、canonical_answer 或 root_cause，也不得在回复中暴露该字段名。
如果工具结果 status=failed 且 error_code=unmet_prerequisite，说明本次动作没有形成公开观察；
只能承认本次没有得到可用观察，不能说题面没有该证据、不能说观察已记录，也不能替学生指定其它检查。
如果 AgentContext.reply_feedback == "structured_action_requires_tool_call"，说明当前轮来自 QuickAction，
且模型尚未先产生对应的 tool_calls；必须先输出只包含已授权 action_id 的 tool_calls，不能直接回复或声称观察已完成。
其他情况下，如果 AgentContext.reply_feedback 非空，说明上一版回复未通过公开回复边界校验；只重写 reply，
保留本轮真实语义和教学决策，不解释校验过程，不复述反馈内容。若反馈指出
reply_repeats_observation，说明公开观察已经由页面卡片单独展示；新 reply 只能增加解释、
关联或反思，不能再次改写日志、指标、返回码、成功率、超时或失败细节。
teaching_decision.allow_explicit_next_step 与 allow_ruled_out_scope 必须始终为 false。不得宣布根因、泄露未公开事实、
替学生执行未授权工具，或把未查询的具体工具当成已经执行。
教学表达遵循以下规则：
- “教什么”由 learner_summary 的概念/能力掌握度与当前证据决定；“怎么说”由瞬时状态和 mentor_persona 决定。
- detail 决定解释深度，analogy 只在学生明确偏好时使用，directness 决定是否先给结论边界；不要重复解释已掌握概念。
- 只轻度镜像学生语气。mentor_persona.humor 即使较高也最多轻接一句就回到技术事实；不得连续造梗。
- frustration=high、random_investigation=true 或连续卡住时减少反问，明确指出继续横向枚举低收益资源项的信息增益很低，
  primary_task 优先用 redirect_investigation 或 release_hint。不要继续陪查一串无关资源。
- 学生真正缩小范围时，只说明他缩小了哪一段因果链；不要使用“非常棒”“关键线索”“继续验证”等固定夸奖模板。
- 重要线索是学生通过行动取得并已公开的事实；教学提示只是缩小搜索空间的帮助；假设仍是待验证解释；三者不得混写。
- 讨论支付回调等复合事故时，始终区分事故直接触发、被暴露的潜在问题、可见现象和衍生风险；不能把“数据库锁”
  这样的潜在问题单独说成完整结论，也不能把某个配置变化说成全部因果链。
- 如果 response_brief.primary_task 是 close_investigation，必须按 response_brief.closure_boundaries
  同时检查修复动作和验证闭环；只说清因果链而没有说明如何修复、如何用公开指标验证恢复时，不能把调查写成完成。
不要输出 reasoning、chain of thought、rationale 或任何额外字段。
""".strip()

# 后端开关打开时替换上面的思考禁令：模型默认在思考通道逐步推理，
# 且思考增量经调试 SSE 展示到前端思考组件。JSON 契约边界保持不变。
_COT_PROHIBITION = "不要输出 reasoning、chain of thought、rationale 或任何额外字段。"
_COT_GUIDANCE = (
    "思考输出已开启：请默认先在思考通道中逐步推理——先确认学生这轮在验证什么、已有哪些公开证据，"
    "再对照工具结果、掌握度与教学约束决定本轮动作，最后才写结构化输出；思考过程会作为过程流展示。"
    "思考中不得出现答案原文、未公开证据或内部字段名。结构化 JSON 输出仍只能包含契约字段，"
    "不要在输出对象里新增 reasoning 等额外字段。"
)


def reasoning_output_enabled() -> bool:
    """与 app._raw_reasoning_stream_enabled 共用同一后端开关。

    开关打开时：思考增量走调试 SSE（app.py），提示词从“禁止推理”
    切换为“默认逐步推理”；关闭时保持生产禁令。读取环境变量不做缓存，
    便于测试与热切换。
    """

    return os.getenv("HIDDENWORLD_TEST_STREAM_RAW_REASONING", "0").strip() == "1"


def scenario_agent_instructions() -> str:
    if reasoning_output_enabled():
        return SCENARIO_AGENT_INSTRUCTIONS.replace(_COT_PROHIBITION, _COT_GUIDANCE)
    return SCENARIO_AGENT_INSTRUCTIONS


def build_scenario_agent_prompt(context: AgentContext) -> str:
    """以安全上下文构造模型输入，当前用户消息单独保留。"""

    payload = context.model_dump(mode="json")
    # 回复任务简报是安全投影，不携带答案、证据 ID 或假设 ID。它把当前
    # 掌握度、公开观察和 Hint 状态编译成“本轮只做什么”的约束，避免模型
    # 每轮同时解释、复述、提示和下结论。简报不是最终正文。
    brief = ResponseBriefBuilder().build(
        learner_state=context.learner_summary,
        guidance_state=context.guidance_state,
        concept_catalog=context.concept_catalog,
        required_concepts=_concepts_referenced_by_student(
            context.current_user_message,
            context.concept_catalog,
        ),
        observations=[
            {"content": item.content}
            for item in context.tool_results
            if item.status == "succeeded" and item.content.strip()
        ],
        current_hypothesis_label=context.learner_summary.current_hypothesis_label or "",
        ruled_out_labels=context.learner_summary.ruled_out_labels,
        hint_text=context.learner_summary.last_hint,
    )
    payload["response_brief"] = brief.model_dump(mode="json")
    turn_input = _current_turn_input(context)
    # 快捷动作没有自然语言正文时，不把空字符串再次塞进模型 prompt，避免
    # 模型把“没有正文”误读成“没有用户行为”；动作来源和目标由结构化视图表达。
    if not context.current_user_message.strip():
        payload.pop("current_user_message", None)
    payload["current_turn_input"] = turn_input
    return f"{scenario_agent_instructions()}\n\nAgentContext：\n{json.dumps(payload, ensure_ascii=False)}"


def _concepts_referenced_by_student(message: str, catalog) -> list[str]:
    """只把学生消息中明确出现的概念交给简报层，不做答案/意图推断。

    这是一个保守的词面匹配：概念目录由题目声明，命中后只影响“是否需要
    补一句解释”，不会改变假设、证据或授权状态。没有命中时由模型根据安全
    概念目录自行判断，避免运行时复制一套自然语言意图分类器。
    """

    message_folded = message.strip().casefold()
    if not message_folded:
        return []
    result: list[str] = []
    for concept in catalog:
        candidates = [concept.label, *concept.aliases]
        if any(candidate.strip().casefold() in message_folded for candidate in candidates if candidate.strip()):
            result.append(concept.label)
    return result


def _current_turn_input(context: AgentContext) -> dict[str, Any]:
    """为快捷动作提供结构化的当前轮输入，不改写历史消息或领域状态。"""

    message = context.current_user_message.strip()
    if message:
        return {"source": "user_message", "text": message}

    if context.tool_results:
        result = context.tool_results[-1]
        catalog_item = next(
            (item for item in context.action_catalog if item.tool_id == result.tool_id),
            None,
        )
        return {
            "source": "structured_action",
            "action_id": result.tool_id,
            "label": catalog_item.target if catalog_item is not None else result.tool_id,
            "result_status": result.status,
        }

    if context.authorized_actions:
        action = context.authorized_actions[0]
        catalog_item = next(
            (item for item in context.action_catalog if item.tool_id == action.action_ref),
            None,
        )
        return {
            "source": "structured_action",
            "action_id": action.action_ref,
            "label": catalog_item.target if catalog_item is not None else action.action_ref,
        }

    return {"source": "system_turn", "text": ""}


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
                            await _emit_stream_frames(on_reasoning_delta, piece)
                            continue
                        if not isinstance(delta, pai_messages.TextPartDelta) or extractor is None:
                            continue
                        piece = extractor.feed(delta.content_delta)
                        await _emit_stream_frames(on_reply_delta, piece)
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
