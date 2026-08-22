"""题面证据可用性的确定性解析。"""

from __future__ import annotations

from hiddenworld.contracts import AgentTurnRequest, EvidenceRequest
from hiddenworld.contracts.version import EvidenceAvailability


_MISSING_SIMULATION_MESSAGE = "当前题目的教学模拟数据不包含这项证据，不能据此判断。"


def resolve_evidence_request(
    request: AgentTurnRequest,
    requested_text: str,
    *,
    fallback_availability: EvidenceAvailability | None = None,
) -> EvidenceRequest | None:
    """按题目声明匹配证据边界；未声明时只使用调用方给定的保守回退。"""

    requested = requested_text.strip()
    if not requested:
        return None
    normalized = requested.casefold()
    declared_actions = {
        item.observation_action
        for item in request.hidden_world.virtual_tools
        if item.observation_action.strip()
    }
    for rule in request.hidden_world.teaching_model.evidence_availability_rules:
        patterns = [item.casefold().strip() for item in rule.request_patterns if item.strip()]
        if not patterns or not any(pattern in normalized for pattern in patterns):
            continue
        availability = rule.availability
        public_message = rule.public_message.strip()
        if availability == "SIMULATED_ALLOWED" and not declared_actions.intersection(rule.action_ids):
            availability = "UNAVAILABLE"
            public_message = _MISSING_SIMULATION_MESSAGE
        return EvidenceRequest(
            requested_text=requested,
            availability=availability,
            public_message=public_message,
        )
    if fallback_availability is None:
        return None
    return EvidenceRequest(
        requested_text=requested,
        availability=fallback_availability,
    )


__all__ = ["resolve_evidence_request"]
