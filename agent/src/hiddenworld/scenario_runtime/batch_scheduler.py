"""单 Agent 工具批次的确定性校验与拆分。"""

from __future__ import annotations

import json
from dataclasses import dataclass, field

from hiddenworld.contracts import ActionCatalogEntry, AgentToolResult, AuthorizedActionRef, ToolCall


@dataclass
class BatchPlan:
    accepted: list[ToolCall] = field(default_factory=list)
    deferred: list[ToolCall] = field(default_factory=list)
    rejected: list[AgentToolResult] = field(default_factory=list)


class BatchScheduler:
    """按 Runtime 掌握的目录、授权与依赖图决定本轮可执行批次。"""

    def __init__(self, *, dependency_map: dict[str, set[str]] | None = None) -> None:
        self._dependency_map = dependency_map or {}

    def plan(
        self,
        calls: list[ToolCall],
        *,
        action_catalog: list[ActionCatalogEntry],
        authorized_actions: list[AuthorizedActionRef],
        remaining_tool_calls: int,
        completed_fingerprints: set[str] | None = None,
    ) -> BatchPlan:
        catalog = {item.tool_id: item for item in action_catalog}
        authorized = {item.action_ref for item in authorized_actions}
        seen: set[str] = set()
        completed = completed_fingerprints or set()
        candidates: list[ToolCall] = []
        rejected: list[AgentToolResult] = []

        for call in calls:
            fingerprint = _fingerprint(call)
            if fingerprint in seen or fingerprint in completed:
                rejected.append(
                    AgentToolResult(
                        call_id=call.call_id,
                        tool_id=call.tool_id,
                        tool_kind=catalog.get(call.tool_id).kind if call.tool_id in catalog else "unknown",
                        status="already_completed",
                        error_code="already_completed" if fingerprint in completed else "duplicate_call",
                    )
                )
                continue
            seen.add(fingerprint)

            entry = catalog.get(call.tool_id)
            if entry is None:
                rejected.append(_rejected(call, "unsupported_tool"))
                continue
            if entry.kind != "compare_answer" and call.tool_id not in authorized:
                rejected.append(_rejected(call, "user_action_required", entry.kind))
                continue
            candidates.append(call)

        if remaining_tool_calls <= 0:
            rejected.extend(
                _rejected(call, "tool_budget_exhausted", catalog[call.tool_id].kind) for call in candidates
            )
            return BatchPlan(rejected=rejected)

        selected = candidates[:remaining_tool_calls]
        rejected.extend(
            _rejected(call, "tool_budget_exhausted", catalog[call.tool_id].kind)
            for call in candidates[remaining_tool_calls:]
        )

        selected_ids = {call.tool_id for call in selected}
        accepted: list[ToolCall] = []
        deferred: list[ToolCall] = []
        for call in selected:
            dependencies = self._dependency_map.get(call.tool_id, set())
            if dependencies.intersection(selected_ids):
                deferred.append(call)
            else:
                accepted.append(call)
        return BatchPlan(accepted=accepted, deferred=deferred, rejected=rejected)


def _fingerprint(call: ToolCall) -> str:
    return json.dumps(
        {"tool_id": call.tool_id, "arguments": call.arguments},
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    )


def _rejected(call: ToolCall, error_code: str, tool_kind: str = "unknown") -> AgentToolResult:
    return AgentToolResult(
        call_id=call.call_id,
        tool_id=call.tool_id,
        tool_kind=tool_kind,
        status="rejected" if error_code != "unsupported_tool" else "unsupported",
        error_code=error_code,
    )
