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
    TurnAssessment,
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
        turn_assessment: TurnAssessment | None = None,
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
        projected = _apply_session_learning_state(
            world,
            before=prior,
            after=projected,
            analysis=analysis,
            assessment=turn_assessment,
            allow_progress_signals=advance_state,
        )
        if answer_comparison is not None:
            projected.repair_status = _repair_status(answer_comparison.solution_coverage)
        if advance_state:
            if teaching_decision is not None:
                focus = teaching_decision.guidance_direction.strip()
                if focus in _VALID_FOCUS_VALUES:
                    projected.current_focus = focus
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
                current_focus=projected.current_focus or prior_guidance.current_focus,
                repair_status=projected.repair_status,
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


def _repair_status(solution_coverage: float) -> str:
    """把内部修复覆盖率收窄为 Agent 可见的三值状态。"""

    if solution_coverage <= 0.0:
        return "none"
    if solution_coverage >= 1.0:
        return "sufficient"
    return "partial"


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
    return GuidanceState(
        stalled_turns=prior.stalled_turns,
        current_focus=prior.current_focus,
        repair_status=prior.repair_status,
    )


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


_ALLOWED_SKILL_IDS = frozenset({"log_reading", "causal_reasoning", "cross_layer_debugging"})
_VALID_FOCUS_VALUES = frozenset({"logs", "metrics", "config", "change", "dependency", "data", "resource"})
_PREFERENCE_VALUES: dict[str, frozenset[str]] = {
    "detail": frozenset({"brief", "balanced", "detailed"}),
    "analogy": frozenset({"low", "medium", "high"}),
    "directness": frozenset({"low", "medium", "high"}),
}


def _apply_session_learning_state(
    world: HiddenWorld,
    *,
    before: LearnerState,
    after: LearnerState,
    analysis: TurnAnalysis,
    assessment: TurnAssessment | None,
    allow_progress_signals: bool,
) -> LearnerState:
    """归约当前会话画像与提示；不把模型信号直接当权威状态。"""

    updated = after.model_copy(deep=True)
    assessment = assessment or TurnAssessment()
    concept_ids = {
        item.concept_id
        for item in world.teaching_model.concepts
        if item.concept_id.strip()
    }
    demonstrated_understanding = not analysis.is_low_confidence() and bool(
        assessment.established_facts
        or assessment.made_claim
        or assessment.contains_answer_attempt
        or assessment.claim_type in {"observation", "hypothesis", "answer"}
        or analysis.actions
    )
    mastery_progress = False
    if allow_progress_signals and demonstrated_understanding:
        for concept_id, signal in assessment.concept_mastery_signals.items():
            current = before.concept_mastery.get(concept_id, 0)
            if concept_id not in concept_ids or signal <= current:
                continue
            updated.concept_mastery[concept_id] = min(
                4,
                current + 1,
            )
            mastery_progress = True
        for skill_id, signal in assessment.skill_mastery_signals.items():
            current = before.skill_mastery.get(skill_id, 0)
            if skill_id not in _ALLOWED_SKILL_IDS or signal <= current:
                continue
            updated.skill_mastery[skill_id] = min(
                4,
                current + 1,
            )
            mastery_progress = True

    if mastery_progress and updated.effective_turns == before.effective_turns:
        updated.effective_turns += 1
        updated.stalled_turns = 0

    preferences = updated.explanation_preferences.model_copy(deep=True)
    for key, value in assessment.preference_signals.items():
        allowed = _PREFERENCE_VALUES.get(str(key))
        if allowed is None or value not in allowed:
            continue
        setattr(preferences, str(key), value)
    updated.explanation_preferences = preferences

    if allow_progress_signals:
        updated.hint_level, updated.last_hint = _next_hint_state(
            world,
            before=before,
            after=updated,
            analysis=analysis,
            assessment=assessment,
        )
    return updated


def _next_hint_state(
    world: HiddenWorld,
    *,
    before: LearnerState,
    after: LearnerState,
    analysis: TurnAnalysis,
    assessment: TurnAssessment,
) -> tuple[int, str]:
    """提示随卡住/随机排查升级，形成进展后逐级回落。"""

    explicit_help = assessment.is_stuck or analysis.is_stuck
    random_investigation = assessment.random_investigation
    progressed = assessment.progress_assessment in {"progress", "partial"}

    if explicit_help or random_investigation:
        target = min(4, before.hint_level + 1)
    elif progressed:
        target = max(0, before.hint_level - 1)
    else:
        target = before.hint_level

    last_hint = before.last_hint
    if target > before.hint_level:
        step = next(
            (item for item in world.teaching_model.hint_ladder if item.level == target),
            None,
        )
        if step is None or not step.public_hint.strip():
            return before.hint_level, before.last_hint
        last_hint = step.public_hint.strip()
    elif target < before.hint_level:
        last_hint = ""
    return target, last_hint


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
    hinted_dimensions = _hint_focus_dimensions(world, state.hint_level)
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
        if not terminal and category in hinted_dimensions:
            hint_level = "direct" if state.hint_level >= 3 else "light"
        navigation.append(
            TeachingDimensionRef(
                dimension_id=f"dimension:{category}",
                category=category,
                status=status,
                hint_level=hint_level,
            )
        )
    return navigation


def _hint_focus_dimensions(world: HiddenWorld, hint_level: int) -> set[str]:
    actions = _hint_focus_actions(world, hint_level)
    if not actions:
        return set()
    categories: set[str] = set()
    for observation in world.observations:
        if observation.action not in actions:
            continue
        for evidence_id in observation.yields_evidence:
            node = world.evidence_by_id(evidence_id)
            if node is not None:
                dimension = _CATEGORY_TO_DIMENSION.get(str(node.category))
                if dimension:
                    categories.add(dimension)
    return categories


def _allowed_action_ids(
    world: HiddenWorld,
    state: LearnerState,
    *,
    observations: Sequence[Observation] = (),
) -> list[str]:
    """返回 2–3 个当前可执行候选，不把整份工具目录伪装成推荐。"""

    # 低置信度/QuickAction 轮可能不会把动作写入 actions_taken，但本轮已经
    # 执行的观察同样不能马上重新出现在 allowed_action_ids 中。
    taken = set(state.actions_taken).union(item.action for item in observations)
    collected = set(state.collected_evidence)
    candidates: list[tuple[int, int, str]] = []
    seen: set[str] = set()
    focus = state.current_focus.casefold().strip()
    hint_actions = _hint_focus_actions(world, state.hint_level)
    if world.virtual_tools:
        for index, tool in enumerate(world.virtual_tools):
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
            if not _action_prerequisites_met(world, evidence_ids, collected):
                continue
            score = _action_priority(
                action=action,
                focus=focus,
                hint_actions=hint_actions,
                searchable=" ".join([action, tool.kind, tool.target, *tool.aliases]).casefold(),
            )
            candidates.append((score, -index, action))
            seen.add(action)
        return _top_actions(candidates)
    for index, observation in enumerate(world.observations):
        action = str(observation.action or "").strip()
        if not _is_public_observation_action_id(action) or action in seen or action in taken:
            continue
        evidence_ids = set(observation.yields_evidence)
        if evidence_ids and evidence_ids.issubset(collected):
            continue
        if not _action_prerequisites_met(world, evidence_ids, collected):
            continue
        score = _action_priority(
            action=action,
            focus=focus,
            hint_actions=hint_actions,
            searchable=action.casefold(),
        )
        candidates.append((score, -index, action))
        seen.add(action)
    return _top_actions(candidates)


def _hint_focus_actions(world: HiddenWorld, hint_level: int) -> set[str]:
    step = next((item for item in world.teaching_model.hint_ladder if item.level == hint_level), None)
    return set(step.focus_action_ids) if step is not None else set()


def _action_prerequisites_met(
    world: HiddenWorld,
    evidence_ids: set[str],
    collected: set[str],
) -> bool:
    """动作至少要能形成一条当前可公开观察。"""

    if not evidence_ids:
        return True
    for evidence_id in evidence_ids:
        if evidence_id in collected:
            continue
        node = world.evidence_by_id(evidence_id)
        if node is not None and set(node.prerequisites).issubset(collected):
            return True
    return False


def _action_priority(
    *,
    action: str,
    focus: str,
    hint_actions: set[str],
    searchable: str,
) -> int:
    score = 0
    if action in hint_actions:
        score += 100
    if focus and (focus in searchable or any(token and token in searchable for token in focus.split())):
        score += 40
    return score


def _top_actions(candidates: list[tuple[int, int, str]]) -> list[str]:
    candidates.sort(reverse=True)
    return [action for _, _, action in candidates[:3]]


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
