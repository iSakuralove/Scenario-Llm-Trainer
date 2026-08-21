"""确定性回合状态归约。

本模块把 kernel 中的状态推进、关系判定、防猜、线索审批和教学约束编排为
一个无副作用入口。它不读取或生成任何模型隐藏事实；TeachingDecision 仅用于
教学状态投影，回合控制由 Runtime 的权威状态与本轮内部比较结果共同归约。
"""

from __future__ import annotations

from collections.abc import Sequence

from pydantic import BaseModel, ConfigDict, Field

from hiddenworld.contracts import (
    GuidanceState,
    HiddenWorld,
    InternalAnswerComparison,
    LearnerState,
    TeachingConstraints,
    TeachingDecision,
    TurnAnalysis,
    TurnControl,
    HypothesisRelation,
    Observation,
    TeachingDimensionRef,
)
from hiddenworld.kernel.antiguess import AntiGuessDecision, AntiGuess
from hiddenworld.kernel.cluegate import ClueGate
from hiddenworld.kernel.evidence import EvidenceEngine
from hiddenworld.kernel.policy import TeachingPolicy
from hiddenworld.kernel.verifier import RootCauseVerifier


class StateReduction(BaseModel):
    """一轮归约后的确定性结果。"""

    model_config = ConfigDict(extra="forbid")

    projected_state: LearnerState
    guidance_state: GuidanceState
    turn_control: TurnControl
    relation: HypothesisRelation
    anti_guess: AntiGuessDecision
    approved_releases: list[str] = Field(default_factory=list)
    constraints: TeachingConstraints
    answer_comparison: InternalAnswerComparison | None = None


class StateReducer:
    """组合 kernel 组件，保持每次调用输入不可变。"""

    def reduce(
        self,
        request,
        *,
        analysis: TurnAnalysis,
        observations: Sequence[Observation] = (),
        answer_comparison: InternalAnswerComparison | None = None,
        teaching_decision: TeachingDecision | None = None,
        progress_assessment: str | None = None,
        max_releases: int | None = None,
        advance_state: bool = True,
        prior_guidance_state: GuidanceState | None = None,
        prior_turn_control: TurnControl | None = None,
    ) -> StateReduction:
        world: HiddenWorld = request.hidden_world
        prior: LearnerState = request.learner_state
        budget = request.budget.max_releases if max_releases is None else max_releases
        approved = ClueGate().approve(
            world,
            actions=analysis.actions,
            collected_evidence=prior.collected_evidence,
            max_releases=budget,
        )
        projected = (
            EvidenceEngine().advance(
                prior,
                analysis=analysis,
                observations=observations,
                valid_hypothesis_ids=world.hypothesis_ids(),
            )
            if advance_state
            else prior.model_copy(deep=True)
        )
        relation = RootCauseVerifier().relation(
            world, hypothesis_id=analysis.hypothesis_id, learner_state=projected
        )
        anti_guess = AntiGuess().evaluate(
            world, collected_evidence=projected.collected_evidence, relation=relation
        )
        contradictions = answer_comparison.contradictions if answer_comparison else []
        # AnswerComparison 是本轮唯一允许改变答案完成权限的内部结果。一旦
        # 比较已经执行，不能再用较早的 relation/coverage 结果覆盖它，否则
        # ``compare_answer`` 的结论会在第三次归约时被静默丢掉。
        completion_allowed = (
            answer_comparison.completion_allowed
            if answer_comparison is not None
            else anti_guess.completion_allowed
        )
        completion_ready = bool(answer_comparison is not None and completion_allowed)
        label = _coverage_label(anti_guess)
        allowed_category = None
        if approved:
            node = world.evidence_by_id(approved[0])
            allowed_category = node.category if node else None
        constraints = TeachingPolicy().compile(
            projected,
            analysis=analysis,
            completion_allowed=completion_allowed,
            evidence_coverage=label,
            may_release=approved,
            allowed_category=allowed_category,
            contradictions=contradictions,
        )
        prior_guidance = _normalize_guidance(
            _resolve_prior_guidance(request, prior_guidance_state, prior=prior)
        )
        prior_control = _normalize_turn_control(_resolve_prior_control(request, prior_turn_control))
        teaching_state = _legal_teaching_state(
            previous=prior_guidance.teaching_state,
            requested=(teaching_decision.teaching_state if teaching_decision is not None else None),
            analysis=analysis,
            answer_comparison=answer_comparison,
            completion_allowed=completion_allowed,
            terminal=prior_control.terminal,
        )
        navigation = _build_navigation(
            world,
            projected,
            allowed_category=allowed_category,
            terminal=prior_control.terminal,
        )
        if prior_control.terminal:
            # 终止会话是不可逆生命周期状态；迟到/重放请求不能用当前模型输出
            # 改写上一轮教学导航。
            guidance = prior_guidance.model_copy(deep=True)
        else:
            guidance = GuidanceState(
                teaching_state=teaching_state,
                progress_assessment=progress_assessment or getattr(analysis, "progress_assessment", "unknown"),
                navigation=navigation,
                stalled_turns=projected.stalled_turns,
                current_focus=(
                    (teaching_decision.guidance_direction if teaching_decision else "").strip()
                    or projected.current_focus
                    or prior_guidance.current_focus
                ),
            )
        # terminal 表示会话生命周期已经结束，只能由上一轮权威会话状态回注；
        # 证据充足/答案可提交并不自动终止会话。三者故意保持独立。
        control = TurnControl(
            terminal=prior_control.terminal,
            completion_allowed=(
                prior_control.completion_allowed
                if prior_control.terminal
                else completion_allowed
            ),
            completion_ready=(
                prior_control.completion_ready
                if prior_control.terminal
                else completion_ready
            ),
            allowed_action_ids=(
                []
                if prior_control.terminal
                else _allowed_action_ids(world, projected, observations=observations)
            ),
        )
        return StateReduction(
            projected_state=projected,
            guidance_state=guidance,
            turn_control=control,
            relation=relation,
            anti_guess=anti_guess,
            approved_releases=approved,
            constraints=constraints,
            answer_comparison=answer_comparison,
        )


def _coverage_label(decision: AntiGuessDecision) -> str:
    total = len(decision.best_evidence_set)
    found = total - len(decision.missing_evidence)
    return f"{found}/{total}"


# 文档定义的教学状态图。表内只允许显式、可解释的迁移；模型若请求跳跃到
# 不相邻状态，Reducer 会保留当前状态或落到与本轮意图相符的安全状态。
_TEACHING_TRANSITIONS: dict[str, frozenset[str]] = {
    "normal_diagnosis": frozenset(
        {
            "normal_diagnosis",
            "guided_inquiry",
            "unsupported_hypothesis",
            "anti_guess_detected",
            "premature_conclusion",
            "conclusion_grilling",
            "evidence_reconstruction",
            "debrief",
            "casual_chat",
            "clarification",
            "off_topic",
            "garbage",
        }
    ),
    "guided_inquiry": frozenset(
        {
            "guided_inquiry",
            "unsupported_hypothesis",
            "anti_guess_detected",
            "premature_conclusion",
            "conclusion_grilling",
            "evidence_reconstruction",
            "normal_diagnosis",
            "debrief",
            "casual_chat",
            "clarification",
            "off_topic",
            "garbage",
        }
    ),
    "unsupported_hypothesis": frozenset(
        {
            "unsupported_hypothesis",
            "guided_inquiry",
            "conclusion_grilling",
            "evidence_reconstruction",
            "normal_diagnosis",
            "casual_chat",
            "clarification",
            "off_topic",
            "garbage",
        }
    ),
    "anti_guess_detected": frozenset(
        {
            "anti_guess_detected",
            "premature_conclusion",
            "conclusion_grilling",
            "guided_inquiry",
        }
    ),
    "premature_conclusion": frozenset(
        {
            "premature_conclusion",
            "conclusion_grilling",
            "evidence_reconstruction",
            "guided_inquiry",
            "normal_diagnosis",
        }
    ),
    "conclusion_grilling": frozenset(
        {
            "conclusion_grilling",
            "evidence_reconstruction",
            "normal_diagnosis",
            "guided_inquiry",
            "debrief",
        }
    ),
    "evidence_reconstruction": frozenset(
        {
            "evidence_reconstruction",
            "conclusion_grilling",
            "normal_diagnosis",
            "guided_inquiry",
            "debrief",
        }
    ),
    "debrief": frozenset({"debrief"}),
    "casual_chat": frozenset(
        {"casual_chat", "normal_diagnosis", "guided_inquiry", "clarification", "off_topic", "garbage"}
    ),
    "clarification": frozenset(
        {"clarification", "normal_diagnosis", "guided_inquiry", "casual_chat", "off_topic", "garbage"}
    ),
    "off_topic": frozenset({"off_topic", "casual_chat", "normal_diagnosis", "guided_inquiry", "garbage"}),
    "garbage": frozenset({"garbage", "casual_chat", "normal_diagnosis", "guided_inquiry"}),
}


def _resolve_prior_guidance(request, explicit: GuidanceState | None, *, prior: LearnerState) -> GuidanceState:
    if explicit is not None:
        return explicit.model_copy(deep=True)
    for name in ("prior_guidance_state", "previous_guidance_state", "guidance_state"):
        value = getattr(request, name, None)
        if value is None:
            continue
        if isinstance(value, GuidanceState):
            return value.model_copy(deep=True)
        try:
            return GuidanceState.model_validate(value)
        except (TypeError, ValueError):
            continue
    return GuidanceState(stalled_turns=prior.stalled_turns, current_focus=prior.current_focus)


def _resolve_prior_control(request, explicit: TurnControl | None) -> TurnControl:
    if explicit is not None:
        return explicit.model_copy(deep=True)
    for name in ("prior_turn_control", "previous_turn_control", "turn_control"):
        value = getattr(request, name, None)
        if value is None:
            continue
        if isinstance(value, TurnControl):
            return value.model_copy(deep=True)
        try:
            return TurnControl.model_validate(value)
        except (TypeError, ValueError):
            continue
    return TurnControl()


def _normalize_guidance(value: GuidanceState) -> GuidanceState:
    seen: set[str] = set()
    navigation = []
    for item in value.navigation:
        if item.dimension_id in seen:
            continue
        seen.add(item.dimension_id)
        navigation.append(item)
    if navigation == value.navigation:
        return value
    return value.model_copy(update={"navigation": navigation}, deep=True)


def _normalize_turn_control(value: TurnControl) -> TurnControl:
    """执行 TurnControl 的跨字段不变式。"""

    unique_actions: list[str] = []
    seen: set[str] = set()
    for action_id in value.allowed_action_ids:
        action_id = str(action_id).strip()
        if not action_id or action_id in seen:
            continue
        seen.add(action_id)
        unique_actions.append(action_id)
    if value.terminal:
        unique_actions = []
    return value.model_copy(
        update={
            "completion_ready": bool(value.completion_ready and value.completion_allowed),
            "allowed_action_ids": unique_actions,
        },
        deep=True,
    )


def _legal_teaching_state(
    *,
    previous: str,
    requested: str | None,
    analysis: TurnAnalysis,
    answer_comparison: InternalAnswerComparison | None,
    completion_allowed: bool,
    terminal: bool,
) -> str:
    """把 Agent 的策略提议收敛到显式状态图，不让模型任意跳态。"""

    if terminal:
        return previous
    inferred = _infer_teaching_state(
        analysis,
        answer_comparison=answer_comparison,
        completion_allowed=completion_allowed,
    )
    candidate = requested or inferred or previous
    if candidate == "debrief" and not completion_allowed:
        candidate = inferred or "normal_diagnosis"
    allowed = _TEACHING_TRANSITIONS.get(previous, _TEACHING_TRANSITIONS["normal_diagnosis"])
    if candidate in allowed:
        return candidate
    if inferred in allowed:
        return inferred
    return previous if previous in _TEACHING_TRANSITIONS else "normal_diagnosis"


def _infer_teaching_state(
    analysis: TurnAnalysis,
    *,
    answer_comparison: InternalAnswerComparison | None,
    completion_allowed: bool,
) -> str:
    intent = str(getattr(analysis, "intent", "") or "")
    if getattr(analysis, "is_noise", False) or intent == "garbage":
        return "garbage"
    if intent == "chat":
        return "casual_chat"
    if getattr(analysis, "is_off_topic", False) or intent == "off_topic":
        return "off_topic"
    if intent in {"clarification", "explanation_request"}:
        return "clarification"
    if answer_comparison is not None and getattr(analysis, "contains_answer_attempt", False):
        if completion_allowed:
            return "evidence_reconstruction"
        if answer_comparison.claim_alignment >= 0.8:
            return "conclusion_grilling"
        return "premature_conclusion"
    if getattr(analysis, "contains_answer_attempt", False):
        return "unsupported_hypothesis"
    if getattr(analysis, "is_stuck", False) or intent in {"stuck", "help_request", "request_hint"}:
        return "guided_inquiry"
    return "normal_diagnosis"


_CATEGORY_TO_DIMENSION: dict[str, str] = {
    "logs": "evidence",
    "metrics": "capacity",
    "config": "configuration",
    "change": "temporal",
    "dependency": "dependency",
    "data": "data",
    "resource": "resource",
}


def _build_navigation(
    world: HiddenWorld,
    state: LearnerState,
    *,
    allowed_category: str | None,
    terminal: bool,
) -> list[TeachingDimensionRef]:
    """把证据图聚合成不含答案关键词的粗粒度导航。"""

    grouped: dict[str, list[str]] = {}
    for node in world.evidence_graph:
        dimension = _CATEGORY_TO_DIMENSION.get(str(node.category))
        if dimension is None:
            continue
        grouped.setdefault(dimension, []).append(node.evidence_id)
    collected = set(state.collected_evidence)
    navigation: list[TeachingDimensionRef] = []
    for category, evidence_ids in grouped.items():
        found = len(collected.intersection(evidence_ids))
        if found == 0:
            status = "unexplored"
        elif found >= len(evidence_ids):
            status = "covered"
        else:
            status = "in_progress"
        hint_level = "none"
        if not terminal and allowed_category is not None and _CATEGORY_TO_DIMENSION.get(allowed_category) == category:
            hint_level = "light"
        navigation.append(
            TeachingDimensionRef(
                dimension_id=f"dimension:{category}",
                category=category,
                status=status,
                hint_level=hint_level,
            )
        )
    return navigation


def _allowed_action_ids(
    world: HiddenWorld,
    state: LearnerState,
    *,
    observations: Sequence[Observation] = (),
) -> list[str]:
    """返回仍可供学生选择的题目声明动作，不把隐藏证据内容带出。"""

    # 低置信度/QuickAction 轮可能不会把动作写入 actions_taken，但本轮已经
    # 执行的观察同样不能马上重新出现在 allowed_action_ids 中。
    taken = set(state.actions_taken).union(item.action for item in observations)
    collected = set(state.collected_evidence)
    result: list[str] = []
    seen: set[str] = set()
    if world.virtual_tools:
        for tool in world.virtual_tools:
            action = str(tool.observation_action or "").strip()
            if not _is_public_observation_action(tool) or action in seen or action in taken:
                continue
            evidence_ids = set(tool.evidence_ids)
            if not evidence_ids:
                evidence_ids = {
                    evidence_id
                    for observation in world.observations
                    if observation.action == action
                    for evidence_id in observation.yields_evidence
                }
            if evidence_ids and evidence_ids.issubset(collected):
                continue
            result.append(action)
            seen.add(action)
        return result
    for observation in world.observations:
        action = str(observation.action or "").strip()
        if (
            _is_public_observation_action_id(action)
            and action not in seen
            and action not in taken
        ):
            result.append(action)
            seen.add(action)
    return result


def _is_public_observation_action(tool) -> bool:
    action = str(getattr(tool, "observation_action", "") or "").casefold()
    kind = str(getattr(tool, "kind", "") or "").casefold()
    return (
        _is_public_observation_action_id(action)
        and kind not in {"internal", "answer", "answer_comparison"}
    )


def _is_public_observation_action_id(action: str) -> bool:
    action = str(action or "").casefold()
    return bool(action) and "compare_answer" not in action


__all__ = ["StateReducer", "StateReduction"]
