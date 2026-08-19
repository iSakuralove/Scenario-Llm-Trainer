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
    return "\n".join(lines)


def _render_transcript(deps: MentorDeps) -> str:
    if not deps.transcript:
        return "## 对话记录\n这是第一轮，你们还没说过话。"
    speaker = {"user": "学生", "mentor": "你"}
    lines = ["## 对话记录"]
    lines.extend(f"{speaker.get(turn.role, turn.role)}：{turn.content}" for turn in deps.transcript)
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
