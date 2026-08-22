"""把 AgentTurnRequest 投影为 ScenarioAgent 可见的 AgentContext。"""

from __future__ import annotations

from collections.abc import Mapping

from hiddenworld.action_resolver import (
    legacy_action_aliases,
    resolve_declared_items,
    resolve_legacy_observation_action,
    resolve_user_requested_actions,
)
from hiddenworld.contracts import (
    ActionCatalogEntry,
    AgentBudgetView,
    AgentContext,
    AgentTurnControlView,
    AuthorizedActionRef,
    EvidenceRequestView,
    GuidanceState,
    HypothesisCatalogEntry,
    LearnerStateView,
    TurnControl,
    ToolStateView,
)
from hiddenworld.contracts.transport import AgentTurnRequest
from hiddenworld.evidence_availability import resolve_evidence_request


_ALLOWED_SKILL_IDS = frozenset({"log_reading", "causal_reasoning", "cross_layer_debugging"})
_RECENT_COMPLETE_TURNS = 4


def project_agent_context(
    request: AgentTurnRequest,
    *,
    prior_guidance_state: GuidanceState | None = None,
    prior_turn_control: TurnControl | AgentTurnControlView | None = None,
) -> AgentContext:
    """逐字段白名单投影，禁止把完整 request 或 HiddenWorld 传给模型。

    ``AgentTurnRequest`` 在旧部署中还没有携带上一轮导航字段，因此这里同时
    支持显式参数和按约定名称读取请求快照。新 Runtime 应显式传入
    ``prior_guidance_state`` / ``prior_turn_control``；按属性读取只是为了让
    旧的 HTTP 适配器在契约扩展期间平滑工作。绝不把完整 ``TurnControl`` 投影
    给模型：completion 相关字段仍是 Runtime 私有裁判结果。
    """

    prior_guidance = _normalize_guidance(_resolve_prior_guidance(request, prior_guidance_state))
    prior_control = _resolve_prior_control(request, prior_turn_control)

    public_tools = [item for item in request.hidden_world.virtual_tools if _is_public_observation_tool(item)]
    catalog = [
        ActionCatalogEntry(
            tool_id=item.observation_action,
            kind=item.kind,
            target=item.target,
            parameter_names=list(item.redacted_parameters),
            aliases=list(item.aliases),
        )
        for item in public_tools
    ]
    # 旧题目没有 virtual_tools 时，仍暴露观察入口，但不暴露结果/evidence 映射。
    if not catalog:
        catalog = [
            ActionCatalogEntry(
                tool_id=item.action,
                kind=item.action.partition(":")[2].partition(".")[0] or "observation",
                target=item.action,
                aliases=[],
            )
            for item in request.hidden_world.observations
            if _is_public_observation_action_id(item.action)
        ]

    hypothesis_catalog = [
        HypothesisCatalogEntry(hypothesis_id=item.hypothesis_id, label=item.label)
        for item in request.hidden_world.hypotheses
        if item.hypothesis_id.strip() and item.label.strip()
    ]

    authorized = _project_authorized_actions(request)
    evidence_request = _project_evidence_request(request)
    labels = {item.hypothesis_id: item.label for item in request.hidden_world.hypotheses}
    learner = request.learner_state
    concept_ids = {
        item.concept_id
        for item in request.hidden_world.teaching_model.concepts
        if item.concept_id.strip()
    }
    learner_summary = LearnerStateView(
        established_facts=list(learner.established_facts),
        actions_taken=list(learner.actions_taken),
        current_focus=learner.current_focus,
        current_hypothesis_label=labels.get(learner.current_hypothesis or ""),
        ruled_out_labels=[labels[item] for item in learner.ruled_out_hypotheses if item in labels],
        effective_turns=learner.effective_turns,
        stalled_turns=learner.stalled_turns,
        concept_mastery={
            key: value
            for key, value in learner.concept_mastery.items()
            if key in concept_ids
        },
        skill_mastery={
            key: value
            for key, value in learner.skill_mastery.items()
            if key in _ALLOWED_SKILL_IDS
        },
        explanation_preferences=learner.explanation_preferences.model_copy(deep=True),
        hint_level=learner.hint_level,
        last_hint=learner.last_hint,
        repair_status=learner.repair_status,
        recent_openings=list(learner.recent_openings),
    )
    consumed_actions = {item.strip() for item in learner.actions_taken if item.strip()}
    default_tool_states = {
        item.tool_id: ToolStateView(
            state="consumed" if item.tool_id in consumed_actions else "available",
            reason=(
                "本会话已使用，不可重复调用"
                if item.tool_id in consumed_actions
                else "等待本轮明确请求"
            ),
        )
        for item in catalog
        if item.tool_id.strip()
    }
    provided_tool_states = getattr(request, "tool_states", {}) or {}
    tool_states = {
        item.tool_id: (
            provided_tool_states[item.tool_id].model_copy(deep=True)
            if item.tool_id in provided_tool_states
            else default_tool_states[item.tool_id]
        )
        for item in catalog
        if item.tool_id in default_tool_states
    }
    # 上一轮 GuidanceState 是跨轮教学状态的唯一安全切片。没有它时才从
    # LearnerState 的公开字段构造最小兼容值；不能每轮无条件重置为默认状态。
    current_focus = prior_guidance.current_focus or learner.current_focus
    if (
        not prior_control.terminal
        and (
            prior_guidance.current_focus != current_focus
            or prior_guidance.stalled_turns < learner.stalled_turns
        )
    ):
        prior_guidance = prior_guidance.model_copy(
            update={
                "current_focus": current_focus,
                "stalled_turns": max(prior_guidance.stalled_turns, learner.stalled_turns),
            }
        )
    if prior_guidance.repair_status != learner.repair_status:
        prior_guidance = prior_guidance.model_copy(
            update={"repair_status": learner.repair_status}
        )

    return AgentContext(
        public_scenario=request.public_scenario,
        conversation_summary=request.conversation_summary.strip(),
        transcript=_recent_transcript(request.transcript),
        current_user_message=request.user_message,
        phase=getattr(request, "phase", "new_user_turn"),
        turn_id=getattr(request, "turn_id", "") or request.request_id,
        original_user_message=(
            getattr(request, "original_user_message", "") or request.user_message
        ),
        evidence_request=evidence_request,
        learner_summary=learner_summary,
        mentor_persona=request.hidden_world.teaching_model.mentor_persona.model_copy(deep=True),
        concept_catalog=[
            item.model_copy(deep=True)
            for item in request.hidden_world.teaching_model.concepts
            if item.concept_id.strip() and item.label.strip() and item.summary.strip()
        ],
        action_catalog=catalog,
        hypothesis_catalog=hypothesis_catalog,
        authorized_actions=authorized,
        action_history=[item.model_copy(deep=True) for item in getattr(request, "action_history", [])],
        tool_states=tool_states,
        budget=AgentBudgetView(
            remaining_model_rounds=11,
            remaining_tool_calls=10,
        ),
        teaching_navigation=list(prior_guidance.navigation),
        guidance_state=prior_guidance,
        turn_control=AgentTurnControlView(terminal=prior_control.terminal),
    )


def _recent_transcript(transcript):
    """只投影最近四个完整用户/导师回合；更早内容由确定性摘要承接。"""

    if not transcript:
        return []
    limit = _RECENT_COMPLETE_TURNS * 2
    return [item.model_copy(deep=True) for item in transcript[-limit:]]


def _project_evidence_request(request: AgentTurnRequest) -> EvidenceRequestView | None:
    requested_text = request.user_message
    if request.structured_user_action is not None:
        requested_text = (
            request.structured_user_action.normalized_scope.strip()
            or request.structured_user_action.action_id.strip()
        )
    resolved = resolve_evidence_request(request, requested_text)
    if resolved is None:
        return None
    return EvidenceRequestView(
        requested_text=resolved.requested_text,
        availability=resolved.availability,
        public_message=resolved.public_message,
    )


def _resolve_prior_guidance(
    request: AgentTurnRequest,
    explicit: GuidanceState | None,
) -> GuidanceState:
    """从显式参数或请求快照取得上一轮安全教学状态。"""

    if explicit is not None:
        if isinstance(explicit, GuidanceState):
            return explicit.model_copy(deep=True)
        return GuidanceState.model_validate(explicit)
    for name in ("prior_guidance_state", "previous_guidance_state", "guidance_state"):
        value = getattr(request, name, None)
        if value is None:
            continue
        if isinstance(value, GuidanceState):
            return value.model_copy(deep=True)
        try:
            return GuidanceState.model_validate(value)
        except (TypeError, ValueError):
            # 请求对象来自旧版本时忽略未知形状，继续使用安全默认值。
            continue
    learner = request.learner_state
    return GuidanceState(
        stalled_turns=learner.stalled_turns,
        current_focus=learner.current_focus,
    )


def _resolve_prior_control(
    request: AgentTurnRequest,
    explicit: TurnControl | AgentTurnControlView | None,
) -> AgentTurnControlView:
    """只投影上一轮 terminal；拒绝把 completion 判定泄露给 Agent。"""

    if explicit is not None:
        if isinstance(explicit, Mapping):
            return AgentTurnControlView(terminal=bool(explicit.get("terminal", False)))
        return AgentTurnControlView(terminal=bool(explicit.terminal))
    for name in ("prior_turn_control", "previous_turn_control", "turn_control"):
        value = getattr(request, name, None)
        if value is None:
            continue
        if isinstance(value, AgentTurnControlView):
            return value.model_copy(deep=True)
        if isinstance(value, TurnControl):
            return AgentTurnControlView(terminal=value.terminal)
        if isinstance(value, Mapping):
            return AgentTurnControlView(terminal=bool(value.get("terminal", False)))
        try:
            # 只读取 terminal，Pydantic extra=ignore 的兼容行为不应扩大安全面。
            return AgentTurnControlView(terminal=bool(getattr(value, "terminal", False)))
        except (TypeError, ValueError):
            continue
    return AgentTurnControlView(terminal=False)


def _normalize_guidance(value: GuidanceState) -> GuidanceState:
    """稳定化跨轮导航，避免重复 dimension_id 让 Agent 看到矛盾状态。"""

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


def _project_authorized_actions(request: AgentTurnRequest) -> list[AuthorizedActionRef]:
    result: list[AuthorizedActionRef] = []
    consumed_actions = _consumed_action_ids(request)
    action = request.structured_user_action
    if action is not None and action.state_revision == request.state_revision:
        if _has_action(request, action.action_id) and action.action_id not in consumed_actions:
            result.append(
                AuthorizedActionRef(
                    authorization_id=f"{request.request_id}:structured:{action.action_id}",
                    action_ref=action.action_id,
                    tool_kind=_tool_kind(request, action.action_id),
                    normalized_scope=action.normalized_scope,
                )
            )

    text = request.user_message.casefold().strip()
    if not text:
        return result
    candidates = resolve_user_requested_actions(
        request.user_message,
        [
            *[item for item in request.hidden_world.virtual_tools if _is_public_observation_tool(item)],
            *_legacy_virtual_tools(request),
        ],
        action_attr="observation_action",
    )
    if not candidates:
        # 与旧 Runtime 的兼容动作标识匹配保持确定性，不做模糊近邻替换。
        candidates = resolve_legacy_observation_action(request.user_message, request.hidden_world.observations)
    # 一条学生消息可以明确请求一组相互关联的观察（例如按 request_id
    # 对比 Gateway 与 Nginx）。解析器已经只返回题目声明的精确动作，
    # 因此逐项签发授权；“候选为空/同一对象多次命中”仍保持拒绝，避免
    # 把泛泛的“看看日志”扩展成工具枚举。
    for action_ref in dict.fromkeys(candidates):
        if (
            any(item.action_ref == action_ref for item in result)
            or action_ref in consumed_actions
            or not _has_action(request, action_ref)
        ):
            continue
        result.append(
            AuthorizedActionRef(
                authorization_id=f"{request.request_id}:message:{action_ref}",
                action_ref=action_ref,
                tool_kind=_tool_kind(request, action_ref),
            )
        )
    return result


def _consumed_action_ids(request: AgentTurnRequest) -> set[str]:
    consumed = {item.strip() for item in request.learner_state.actions_taken if item.strip()}
    for action_id, state in (getattr(request, "tool_states", {}) or {}).items():
        if getattr(state, "state", "") == "consumed":
            consumed.add(action_id)
    return consumed


def _legacy_virtual_tool_aliases(tool) -> tuple[str, ...]:
    """为已创建的旧会话补充已批准的明确别名，不做模糊关键词猜测。"""

    return legacy_action_aliases(tool.observation_action)


def _legacy_virtual_tools(request: AgentTurnRequest):
    """把旧会话的兼容别名放入同一解析器，不复制另一套匹配逻辑。"""

    tools = []
    for tool in request.hidden_world.virtual_tools:
        if not _is_public_observation_tool(tool):
            continue
        aliases = [*tool.aliases, *_legacy_virtual_tool_aliases(tool)]
        if aliases != list(tool.aliases):
            tools.append(tool.model_copy(update={"aliases": aliases}))
    return tools


def _is_public_observation_tool(tool) -> bool:
    """过滤内部答案比较等非观察能力，防止它们进入 Agent 工具目录。"""

    action = str(getattr(tool, "observation_action", "") or "").casefold()
    kind = str(getattr(tool, "kind", "") or "").casefold()
    return _is_public_observation_action_id(action) and kind not in {
        "internal",
        "answer",
        "answer_comparison",
    }


def _is_public_observation_action_id(action: str) -> bool:
    action = str(action or "").casefold()
    return bool(action) and "compare_answer" not in action


def _has_action(request: AgentTurnRequest, action_ref: str) -> bool:
    return any(
        _is_public_observation_action_id(item.action) and item.action == action_ref
        for item in request.hidden_world.observations
    ) or any(
        _is_public_observation_tool(item) and item.observation_action == action_ref
        for item in request.hidden_world.virtual_tools
    )


def _tool_kind(request: AgentTurnRequest, action_ref: str) -> str:
    for item in request.hidden_world.virtual_tools:
        if _is_public_observation_tool(item) and item.observation_action == action_ref:
            return item.kind
    return action_ref.partition(":")[2].partition(".")[0] or "observation"
