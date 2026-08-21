"""把 AgentTurnRequest 投影为 ScenarioAgent 可见的 AgentContext。"""

from __future__ import annotations

from hiddenworld.contracts import (
    ActionCatalogEntry,
    AgentBudgetView,
    AgentContext,
    AgentTurnControlView,
    AuthorizedActionRef,
    LearnerStateView,
)
from hiddenworld.contracts.transport import AgentTurnRequest


def project_agent_context(request: AgentTurnRequest) -> AgentContext:
    """逐字段白名单投影，禁止把完整 request 或 HiddenWorld 传给模型。"""

    catalog = [
        ActionCatalogEntry(
            tool_id=item.observation_action,
            kind=item.kind,
            target=item.target,
            parameter_names=list(item.redacted_parameters),
        )
        for item in request.hidden_world.virtual_tools
    ]
    # 旧题目没有 virtual_tools 时，仍暴露观察入口，但不暴露结果/evidence 映射。
    if not catalog:
        catalog = [
            ActionCatalogEntry(
                tool_id=item.action,
                kind=item.action.partition(":")[2].partition(".")[0] or "observation",
                target=item.action,
            )
            for item in request.hidden_world.observations
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
        turn_control=AgentTurnControlView(terminal=False),
    )


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
    candidates = []
    for tool in request.hidden_world.virtual_tools:
        signals = [*tool.aliases, *tool.query_patterns, *_legacy_virtual_tool_aliases(tool)]
        if any(signal.strip().casefold() in text for signal in signals if signal.strip()):
            candidates.append(tool.observation_action)
    if not candidates:
        # 与旧 Runtime 的兼容动作标识匹配保持确定性，不做模糊近邻替换。
        for observation in request.hidden_world.observations:
            tokens = [part for part in observation.action.casefold().replace(":", ".").split(".") if part]
            if any(token != "inspect" and token in text for token in tokens):
                candidates.append(observation.action)
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

    compatibility_aliases = {
        "inspect:database.order_write": (
            "订单库日志",
            "订单库写入日志",
            "看订单库日志",
        ),
    }
    return compatibility_aliases.get(tool.observation_action, ())


def _has_action(request: AgentTurnRequest, action_ref: str) -> bool:
    return any(item.action == action_ref for item in request.hidden_world.observations) or any(
        item.observation_action == action_ref for item in request.hidden_world.virtual_tools
    )


def _tool_kind(request: AgentTurnRequest, action_ref: str) -> str:
    for item in request.hidden_world.virtual_tools:
        if item.observation_action == action_ref:
            return item.kind
    return action_ref.partition(":")[2].partition(".")[0] or "observation"
