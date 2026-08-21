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
    GuidanceState,
    LearnerStateView,
    TurnControl,
)
from hiddenworld.contracts.transport import AgentTurnRequest


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

    authorized = _project_authorized_actions(request)
    labels = {item.hypothesis_id: item.label for item in request.hidden_world.hypotheses}
    learner = request.learner_state
    learner_summary = LearnerStateView(
        established_facts=list(learner.established_facts),
        actions_taken=list(learner.actions_taken),
        current_focus=learner.current_focus,
        current_hypothesis_label=labels.get(learner.current_hypothesis or ""),
        ruled_out_labels=[labels[item] for item in learner.ruled_out_hypotheses if item in labels],
        effective_turns=learner.effective_turns,
        stalled_turns=learner.stalled_turns,
        recent_openings=list(learner.recent_openings),
    )
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

    return AgentContext(
        public_scenario=request.public_scenario,
        transcript=list(request.transcript),
        current_user_message=request.user_message,
        learner_summary=learner_summary,
        action_catalog=catalog,
        authorized_actions=authorized,
        budget=AgentBudgetView(
            remaining_model_rounds=11,
            remaining_tool_calls=10,
        ),
        teaching_navigation=list(prior_guidance.navigation),
        guidance_state=prior_guidance,
        turn_control=AgentTurnControlView(terminal=prior_control.terminal),
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
    action = request.structured_user_action
    if action is not None and action.state_revision == request.state_revision:
        if _has_action(request, action.action_id):
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
    if len(set(candidates)) == 1:
        action_ref = candidates[0]
        if not any(item.action_ref == action_ref for item in result) and _has_action(request, action_ref):
            result.append(
                AuthorizedActionRef(
                    authorization_id=f"{request.request_id}:message:{action_ref}",
                    action_ref=action_ref,
                    tool_kind=_tool_kind(request, action_ref),
                )
            )
    return result


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
