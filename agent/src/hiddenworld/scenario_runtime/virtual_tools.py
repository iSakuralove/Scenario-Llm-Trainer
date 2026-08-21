"""授权虚拟观察工具执行器。"""

from __future__ import annotations

from hiddenworld.contracts import AgentContext, AgentToolResult, ToolCall
from hiddenworld.contracts.transport import AgentTurnRequest
from hiddenworld.kernel import HiddenWorldEngine


class VirtualObservationExecutor:
    """只在 AgentContext 已有授权引用时执行题目声明的只读观察。"""

    def __init__(self, request: AgentTurnRequest) -> None:
        self.request = request
        self.executed_observations: dict[str, object] = {}

    async def execute(self, call: ToolCall, context: AgentContext) -> AgentToolResult:
        entry = next((item for item in context.action_catalog if item.tool_id == call.tool_id), None)
        if entry is None:
            return AgentToolResult(
                call_id=call.call_id,
                tool_id=call.tool_id,
                tool_kind="unknown",
                status="unsupported",
                error_code="unsupported_tool",
            )
        if entry.kind != "compare_answer" and not any(
            item.action_ref == call.tool_id for item in context.authorized_actions
        ):
            return AgentToolResult(
                call_id=call.call_id,
                tool_id=call.tool_id,
                tool_kind=entry.kind,
                status="rejected",
                error_code="user_action_required",
            )

        observation = next(
            (item for item in self.request.hidden_world.observations if item.action == call.tool_id),
            None,
        )
        if observation is None:
            return AgentToolResult(
                call_id=call.call_id,
                tool_id=call.tool_id,
                tool_kind=entry.kind,
                status="unsupported",
                error_code="observation_not_declared",
            )
        result = HiddenWorldEngine().observe(
            self.request.hidden_world,
            action=call.tool_id,
            collected_evidence=self.request.learner_state.collected_evidence,
        )
        self.executed_observations[call.tool_id] = result
        return AgentToolResult(
            call_id=call.call_id,
            tool_id=call.tool_id,
            tool_kind=entry.kind,
            status="succeeded",
            content=result.result,
        )
