"""ScenarioAgent 单节点循环：模型只规划，Runtime 决定是否执行。"""

from __future__ import annotations

import asyncio
from dataclasses import dataclass
from collections.abc import Awaitable, Callable
from typing import Protocol

from hiddenworld.contracts import AgentContext, AgentModelOutput, AgentToolResult, FinalReplyOutput, ToolCall

from .batch_scheduler import BatchScheduler, _fingerprint


class ScenarioAgentRunner(Protocol):
    async def run(self, context: AgentContext) -> AgentModelOutput: ...


class ToolExecutor(Protocol):
    async def execute(self, call: ToolCall, context: AgentContext) -> AgentToolResult: ...


class AgentLoopBudgetExceeded(RuntimeError):
    """模型在规定轮次内没有收束。"""


@dataclass(frozen=True)
class AgentLoopEvent:
    kind: str
    payload: object


class AgentLoop:
    """最多 11 次模型轮次、最多 10 次逻辑工具调用。"""

    def __init__(
        self,
        agent: ScenarioAgentRunner,
        executor: ToolExecutor,
        *,
        scheduler: BatchScheduler | None = None,
        max_model_rounds: int = 11,
        max_tool_calls: int = 10,
        on_reply_delta: Callable[[str], Awaitable[None]] | None = None,
    ) -> None:
        if max_model_rounds < 1 or max_tool_calls < 0:
            raise ValueError("invalid agent loop budget")
        self.agent = agent
        self.executor = executor
        self.scheduler = scheduler or BatchScheduler()
        self.max_model_rounds = max_model_rounds
        self.max_tool_calls = max_tool_calls
        self.on_reply_delta = on_reply_delta

    async def run(self, context: AgentContext) -> tuple[FinalReplyOutput, list[AgentLoopEvent]]:
        events: list[AgentLoopEvent] = []
        current = context.model_copy(deep=True)
        tool_calls_used = 0
        completed_fingerprints: set[str] = set()

        for round_index in range(self.max_model_rounds):
            current = current.model_copy(
                update={
                    "budget": current.budget.model_copy(
                        update={
                            "remaining_model_rounds": self.max_model_rounds - round_index,
                            "remaining_tool_calls": self.max_tool_calls - tool_calls_used,
                        }
                    )
                }
            )
            run_stream = getattr(self.agent, "run_stream", None)
            if self.on_reply_delta is not None and callable(run_stream):
                output = await run_stream(current, on_reply_delta=self.on_reply_delta)
            else:
                output = await self.agent.run(current)
            if isinstance(output, FinalReplyOutput):
                events.append(AgentLoopEvent("final_reply", output))
                return output, events

            events.append(AgentLoopEvent("understanding", output.public_summary))
            plan = self.scheduler.plan(
                output.calls,
                action_catalog=current.action_catalog,
                authorized_actions=current.authorized_actions,
                remaining_tool_calls=self.max_tool_calls - tool_calls_used,
                completed_fingerprints=completed_fingerprints,
            )
            events.extend(AgentLoopEvent("tool_rejected", item) for item in plan.rejected)
            events.extend(AgentLoopEvent("tool_deferred", item) for item in plan.deferred)

            results = list(plan.rejected)
            if plan.accepted:
                tool_calls_used += len(plan.accepted)
                completed_fingerprints.update(_fingerprint(call) for call in plan.accepted)
                execution_results = await asyncio.gather(
                    *(self.executor.execute(call, current) for call in plan.accepted)
                )
                results.extend(execution_results)
                events.extend(AgentLoopEvent("tool_result", item) for item in execution_results)

            if plan.deferred:
                results.extend(
                    AgentToolResult(
                        call_id=call.call_id,
                        tool_id=call.tool_id,
                        tool_kind="",
                        status="rejected",
                        error_code="dependency_deferred",
                    )
                    for call in plan.deferred
                )
            current = current.model_copy(
                update={
                    "tool_results": [*current.tool_results, *results],
                    "budget": current.budget.model_copy(
                        update={
                            "remaining_model_rounds": self.max_model_rounds - round_index - 1,
                            "remaining_tool_calls": self.max_tool_calls - tool_calls_used,
                        }
                    ),
                }
            )

        raise AgentLoopBudgetExceeded(
            f"ScenarioAgent did not return final_reply within {self.max_model_rounds} model rounds"
        )
