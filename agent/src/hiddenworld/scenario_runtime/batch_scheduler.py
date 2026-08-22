"""单 Agent 工具批次的确定性校验与拆分。"""

from __future__ import annotations

import json
from dataclasses import dataclass, field

from hiddenworld.contracts import (
    ActionCatalogEntry,
    AgentToolResult,
    AuthorizedActionRef,
    ToolCall,
    ToolStateView,
)


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
        tool_states: dict[str, ToolStateView] | None = None,
    ) -> BatchPlan:
        catalog = {item.tool_id: item for item in action_catalog}
        authorized = {item.action_ref for item in authorized_actions}
        seen: set[str] = set()
        completed = completed_fingerprints or set()
        states = tool_states or {}
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
            if states.get(call.tool_id, ToolStateView(state="available")).state == "consumed":
                rejected.append(_rejected(call, "already_completed", entry.kind))
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

    def authorize_action(
        self,
        action_id: str,
        *,
        action_catalog: list[ActionCatalogEntry],
        authorized_actions: list[AuthorizedActionRef],
        call_id: str = "",
        tool_states: dict[str, ToolStateView] | None = None,
    ) -> AgentToolResult | None:
        """校验结构化动作，与普通 tool call 共用同一准入规则。

        QuickAction 是用户点击产生的动作，但它仍必须同时存在于题目目录和
        Runtime 签发的授权集合中；不能因为前端传了一个 action_id 就直接读取
        虚拟世界。返回 ``None`` 表示允许执行，返回终态结果表示拒绝原因。
        """

        catalog = {item.tool_id: item for item in action_catalog}
        entry = catalog.get(action_id)
        if entry is None:
            return AgentToolResult(
                call_id=call_id or f"quick:{action_id}",
                tool_id=action_id,
                tool_kind="unknown",
                status="unsupported",
                error_code="unsupported_tool",
            )
        if (tool_states or {}).get(action_id, ToolStateView(state="available")).state == "consumed":
            return AgentToolResult(
                call_id=call_id or f"quick:{action_id}",
                tool_id=action_id,
                tool_kind=entry.kind,
                status="already_completed",
                error_code="already_completed",
            )
        if not any(item.action_ref == action_id for item in authorized_actions):
            return AgentToolResult(
                call_id=call_id or f"quick:{action_id}",
                tool_id=action_id,
                tool_kind=entry.kind,
                status="rejected",
                error_code="user_action_required",
            )
        return None


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
