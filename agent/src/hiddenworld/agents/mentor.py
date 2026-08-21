"""Mentor：唯一生成学生可见回复的 LLM。"""

from __future__ import annotations

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
6. 如果学生本轮是在澄清或要求解释，先直接回答这个问题；不要用反问代替解释。
7. 只能解释公开题面和已经展示给学生的观察，不得假设学生看到了未展示的内部证据。
8. 工具结果已经由系统展示，且下一步动作会由界面中的快捷操作表达；看到本轮观察后，只能客观转述已公开的时间、目标、状态和返回值，不得判断“问题已经比较清晰”、推断根因、提出“建议下一步检查……”或替学生选择下一项工具。
9. 当前是题目自带的虚拟工具环境，不存在可供学生登录的服务器、终端或配置文件。
   支持的检查会由系统直接模拟并在题目快照侧显示为线索卡；不要说“去服务器看”“打开配置文件”或“回来后再确认”。
10. 学生可以用自然语言描述要查的对象，也可以输入只读 SQL、日志查询或配置查询语句；这些内容只在题目虚拟数据上解释，不会真实执行。

表达风格：
- 回复要像真实的人在认真交流，口吻自然、平实、克制，适合本科论文和训练说明语境，不能有明显的机器腔或套话感。
- 句子长短交替，尽量把相关意思连成顺畅的长句，少用一截一截的短句；可以自然使用“的、了、到、过、会、有、能、把”等连接成分，但不要堆砌语气词。
- 不使用“首先、其次、最后、另外、此外、因此”这类模板连接词，也不要写成“以下几个方面：1、2、3”的排比清单；需要分层时使用“一是、二是、三是”或改成连续叙述。
- 少用句号，多用逗号、分号和自然的转折，避免每句话都同样长、同样结构；常用词适当换成易懂的近义表达，但题目术语、工具名称和证据内容不能改写成别的概念。
- 不追求华丽修辞，不添加原文没有的事实，不为了“像人”而偏离主题；保留原有语气和意图，避免与已有回复连续复用八个字以上的固定句式。
- 需要举例时使用“就……而言”或“拿……来说”，不使用机械的“比如/例如”串联多个例子。

严格遵守 must_not；只能请求 may_release 中的证据。输出 MentorAction JSON。
""".strip()


def build_mentor_prompt(deps: MentorDeps) -> str:
    """渲染 Mentor 可见的完整 instructions；绝不读取 ``guard_only``。

    上下文用散文渲染，不用 ``json.dumps``。你用什么语域跟模型说话，它就用什么
    语域回你——甩一坨 JSON 过去，它就进入填表模式，回复会退化成机械的字段拼接。
    只有 ``may_release`` 这类必须原样回填的 id 列表保留结构化形式。
    """

    sections = [
        MENTOR_INSTRUCTIONS,
        _render_scenario(deps),
        _render_transcript(deps),
        _render_current_user_message(deps),
        _render_intent_context(deps),
        _render_learner(deps),
        _render_released_evidence(deps),
        _render_answer_comparison(deps),
        _render_constraints(deps),
    ]
    return "\n\n".join(part for part in sections if part)


def _render_scenario(deps: MentorDeps) -> str:
    scenario = deps.public_scenario
    lines = [f"## 题面：{scenario.title}", scenario.description]
    if scenario.environment:
        lines.append(f"运行环境：{scenario.environment}")
    if scenario.initial_symptoms:
        lines.append("学生一开始就能看到的现象：")
        lines.extend(f"- {item}" for item in scenario.initial_symptoms)
    if scenario.architecture_diagram:
        lines.extend(["公开链路图：", scenario.architecture_diagram])
    return "\n".join(lines)


def _render_transcript(deps: MentorDeps) -> str:
    if not deps.transcript:
        return "## 对话记录\n这是第一轮，你们还没说过话。"
    speaker = {"user": "学生", "mentor": "你"}
    lines = ["## 对话记录"]
    lines.extend(f"{speaker.get(turn.role, turn.role)}：{turn.content}" for turn in deps.transcript)
    return "\n".join(lines)


def _render_current_user_message(deps: MentorDeps) -> str:
    text = deps.current_user_message.strip()
    if not text:
        return ""
    return f"## 学生本轮消息\n{text}"


def _render_intent_context(deps: MentorDeps) -> str:
    lines = [f"## 本轮消息类型\n{deps.current_intent}"]
    if deps.current_intent in {"clarification", "explanation_request"}:
        lines.append("这是澄清/解释请求：先直接回答学生问的对象，不要继续用排查反问替代回答。")
    if deps.action_match_status == "unsupported" and deps.requested_action_raw:
        lines.append(
            f"学生请求的检查「{deps.requested_action_raw}」不在本题公开动作中；明确说明无法直接提供该观察，不要替换成相近动作。"
        )
    if deps.simulation_tools:
        lines.append("本题可请求的虚拟工具：" + "；".join(deps.simulation_tools))
    return "\n".join(lines)


def _render_learner(deps: MentorDeps) -> str:
    state = deps.learner_state
    lines = ["## 学生现在的位置"]
    if state.established_facts:
        lines.append("他已经自己确认的事实：" + "；".join(state.established_facts))
    else:
        lines.append("他还没有确认任何事实。")
    if state.actions_taken:
        lines.append("他做过的动作：" + "；".join(state.actions_taken))
    if state.current_hypothesis_label:
        lines.append(f"他当前指向的方向：{state.current_hypothesis_label}")
    if state.ruled_out_labels:
        lines.append("已经被排除的方向：" + "；".join(state.ruled_out_labels))
    if state.current_focus:
        lines.append(f"他现在的关注面：{state.current_focus}")
    lines.append(
        f"有效推进 {state.effective_turns} 轮；连续 {state.stalled_turns} 轮没有进展。"
    )
    if state.recent_openings:
        lines.append(
            "你最近用过的开场，这次换一个说法：" + "；".join(f"「{item}」" for item in state.recent_openings)
        )
    return "\n".join(lines)


def _render_released_evidence(deps: MentorDeps) -> str:
    if not deps.released_evidence:
        return ""
    lines = ["## 已经对他公开的观察", "下面这些他已经拿到了，你可以直接引用："]
    lines.extend(f"- {item}" for item in deps.released_evidence)
    return "\n".join(lines)


def _render_answer_comparison(deps: MentorDeps) -> str:
    comparison = deps.answer_comparison
    if comparison is None:
        return ""
    support = {
        "insufficiently_specific": "他的表述还不够具体，说不清到哪一层",
        "needs_more_evidence": "他手上的观察还撑不起这个结论",
        "has_evidence_conflict": "这个说法和他已有的观察对不上",
        "evidence_consistent": "这个说法和他已有的观察是一致的",
    }
    lines = ["## 他这轮给出了一个结论", support.get(comparison.support_status, comparison.support_status)]
    if comparison.user_points:
        lines.append("他自己说到的要点：" + "；".join(comparison.user_points))
    if comparison.next_action:
        lines.append(f"可以顺着往下走的方向：{comparison.next_action}")
    return "\n".join(lines)


def _render_constraints(deps: MentorDeps) -> str:
    constraints = deps.constraints
    facts = constraints.facts
    forbid = {
        "reveal_unreleased": "不得说出任何还没对他公开的信息",
        "confirm_hypothesis": "不得确认他的假设成立",
        "start_debrief": "不得开始复盘收尾",
    }
    lines = ["## 本轮边界"]
    lines.extend(f"- {forbid.get(item, item)}" for item in constraints.must_not)
    lines.append(
        "- 他手上证据"
        + ("能支撑" if facts.hypothesis_supported else "还撑不起")
        + f"他当前的方向；充分证据集进度 {facts.evidence_coverage}。"
    )
    lines.append(f"- 他现在的状态是 {facts.student_affect}，语气要跟上。")
    if facts.contradictions:
        lines.append("- 他自己前后说法对不上的地方：" + "；".join(facts.contradictions))
    if constraints.allowed_direction:
        lines.append(f"- 这轮只能往「{constraints.allowed_direction}」这个粗方向引导。")
    if constraints.may_release:
        lines.append(
            "- 本轮允许释放的 evidence id（requested_releases 只能从中选，原样填写）："
            + "、".join(constraints.may_release)
        )
    else:
        lines.append("- 本轮没有可释放的新观察，requested_releases 给空数组。")
    return "\n".join(lines)


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
