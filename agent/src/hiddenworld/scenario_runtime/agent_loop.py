"""ScenarioAgent 单节点循环：模型只规划，Runtime 决定是否执行。"""

from __future__ import annotations

import asyncio
from dataclasses import dataclass
from collections.abc import Awaitable, Callable
from typing import Literal, Protocol

from hiddenworld.contracts import (
    ActionHistoryEntry,
    AgentContext,
    AgentModelOutput,
    AgentToolResult,
    FinalReplyOutput,
    ToolCall,
    ToolCallsOutput,
    ToolStateView,
)
from hiddenworld.contracts.assessment import direction_status_for_assessment

from .batch_scheduler import BatchScheduler, _fingerprint


class ScenarioAgentRunner(Protocol):
    async def run(self, context: AgentContext) -> AgentModelOutput: ...


class ToolExecutor(Protocol):
    async def execute(self, call: ToolCall, context: AgentContext) -> AgentToolResult: ...


class AgentLoopBudgetExceeded(RuntimeError):
    """模型在规定轮次内没有收束。"""


ScenarioRunFrameKind = Literal[
    "understanding",
    "tool_batch_started",
    "tool_rejected",
    "tool_deferred",
    "tool_result",
    "final_reply",
]


@dataclass(frozen=True)
class ScenarioRunFrame:
    """Python 内部运行帧。

    它只描述 Runtime 内部事实，不携带正式公开序号，也不承担 SSE/历史投影。
    Go 侧仍是正式事件的唯一序号来源；这样工具生命周期和公开事件不会共享
    一个容易回退的计数器。
    """

    kind: ScenarioRunFrameKind
    payload: object


# 兼容既有调用方；新代码和文档使用更明确的 ScenarioRunFrame 名称。
AgentLoopEvent = ScenarioRunFrame


class AgentLoop:
    """最多 11 次模型轮次、最多 5 次逻辑工具调用。

    一个 Turn 可以包含多个受控工具子循环，但默认预算必须足够小，避免
    模型把“继续看看”误当成无限枚举。证据已经足够时由模型返回 final_reply
    提前收束，5 只是安全上限而不是必须消耗的数量。
    """

    def __init__(
        self,
        agent: ScenarioAgentRunner,
        executor: ToolExecutor,
        *,
        scheduler: BatchScheduler | None = None,
        max_model_rounds: int = 11,
        max_tool_calls: int = 5,
        on_reply_delta: Callable[[str], Awaitable[None]] | None = None,
        on_reasoning_delta: Callable[[str], Awaitable[None]] | None = None,
        on_loop_event: Callable[["AgentLoopEvent"], Awaitable[None]] | None = None,
    ) -> None:
        if max_model_rounds < 1 or max_tool_calls < 0:
            raise ValueError("invalid agent loop budget")
        self.agent = agent
        self.executor = executor
        self.scheduler = scheduler or BatchScheduler()
        self.max_model_rounds = max_model_rounds
        self.max_tool_calls = max_tool_calls
        self.on_reply_delta = on_reply_delta
        self.on_reasoning_delta = on_reasoning_delta
        self.on_loop_event = on_loop_event

    async def run(self, context: AgentContext) -> tuple[FinalReplyOutput, list[AgentLoopEvent]]:
        events: list[AgentLoopEvent] = []
        current = context.model_copy(deep=True)
        tool_calls_used = 0
        completed_fingerprints: set[str] = set()

        async def notify(event: AgentLoopEvent) -> None:
            # 实时旁路：只转发循环事实（工具开始/结束），不携带隐藏状态。
            if self.on_loop_event is not None:
                await self.on_loop_event(event)

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
            if (self.on_reply_delta is not None or self.on_reasoning_delta is not None) and callable(run_stream):
                stream_kwargs = {}
                if self.on_reply_delta is not None:
                    stream_kwargs["on_reply_delta"] = self.on_reply_delta
                if self.on_reasoning_delta is not None:
                    stream_kwargs["on_reasoning_delta"] = self.on_reasoning_delta
                output = await run_stream(current, **stream_kwargs)
            else:
                output = await self.agent.run(current)
            if isinstance(output, FinalReplyOutput):
                events.append(AgentLoopEvent("final_reply", output))
                return output, events

            # 保留完整的安全语义投影供 Runtime 归约；不包含隐藏答案或 CoT。
            events.append(AgentLoopEvent("understanding", output))
            plan = self.scheduler.plan(
                output.calls,
                action_catalog=current.action_catalog,
                authorized_actions=current.authorized_actions,
                remaining_tool_calls=self.max_tool_calls - tool_calls_used,
                completed_fingerprints=completed_fingerprints,
                tool_states=current.tool_states,
            )
            events.extend(AgentLoopEvent("tool_rejected", item) for item in plan.rejected)
            events.extend(AgentLoopEvent("tool_deferred", item) for item in plan.deferred)

            results = list(plan.rejected)
            if plan.accepted:
                tool_calls_used += len(plan.accepted)
                completed_fingerprints.update(_fingerprint(call) for call in plan.accepted)
                await notify(AgentLoopEvent("tool_batch_started", list(plan.accepted)))
                raw_results = await asyncio.gather(
                    *(self.executor.execute(call, current) for call in plan.accepted),
                    return_exceptions=True,
                )
                execution_results = [
                    _normalize_execution_result(call, result, current)
                    for call, result in zip(plan.accepted, raw_results, strict=True)
                ]
                results.extend(execution_results)
                events.extend(AgentLoopEvent("tool_result", item) for item in execution_results)
                for item in execution_results:
                    await notify(AgentLoopEvent("tool_result", item))

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
            current = context_after_tool_results(
                current,
                output=output,
                results=results,
                round_index=round_index + 1,
            )
            guidance_state = current.guidance_state
            if output.teaching_decision is not None:
                guidance_state = guidance_state.model_copy(
                    update={
                        "teaching_state": output.teaching_decision.teaching_state,
                        "current_focus": output.teaching_decision.guidance_direction,
                    }
                )
            if output.turn_assessment is not None:
                guidance_state = guidance_state.model_copy(
                    update={
                        "progress_assessment": output.turn_assessment.progress_assessment,
                        "direction_status": direction_status_for_assessment(output.turn_assessment),
                    }
                )
            current = current.model_copy(
                update={
                    "guidance_state": guidance_state,
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


def _append_action_history(
    existing: list[ActionHistoryEntry],
    output: AgentModelOutput | None,
    results: list[AgentToolResult],
    *,
    round_index: int = 0,
) -> list[ActionHistoryEntry]:
    """把本轮动作和结果归纳为模型下一轮可读的短历史。"""

    history = list(existing)
    if isinstance(output, ToolCallsOutput):
        summary = _compact_action_summary(output.public_summary)
        history.extend(
            ActionHistoryEntry(
                action="tool_call",
                tool_name=call.tool_id,
                round=round_index,
                call_id=call.call_id,
                decision_summary=summary,
            )
            for call in output.calls
        )
    history.extend(
        ActionHistoryEntry(
            action="tool_result",
            tool_name=result.tool_id,
            round=round_index,
            call_id=result.call_id,
            decision_summary=_result_decision_summary(result),
            status=result.status,
        )
        for result in results
    )
    return history


def context_after_tool_results(
    context: AgentContext,
    *,
    output: AgentModelOutput | None,
    results: list[AgentToolResult],
    round_index: int | None = None,
) -> AgentContext:
    """把工具动作和终态结果回注到同一轮安全上下文。"""

    if output is None and not results:
        return context
    effective_round = round_index if round_index is not None else context.turn_context.round + 1
    action_history = _append_action_history(
        context.action_history,
        output,
        results,
        round_index=effective_round,
    )
    tool_states = _update_tool_states(context.tool_states, results)
    has_error = any(item.status != "succeeded" for item in results)
    next_phase = "after_tool_error" if has_error else "after_tool_call"
    successful_results = [
        item for item in results if item.status == "succeeded" and item.content.strip()
    ]
    last_result = results[-1] if results else None
    envelope = context.turn_context.model_copy(
        update={
            "round": effective_round,
            "phase": next_phase,
            "continuation": True,
            "continuation_note": (
                "这是同一用户轮次的继续；上一动作未形成公开观察，请基于失败状态决定是否收束。"
                if has_error
                else "这是同一用户轮次的继续；上一动作已经返回公开观察，请基于 Observation 决策。"
            ),
            "last_action_id": last_result.call_id if last_result is not None else "",
            "last_action_status": last_result.status if last_result is not None else None,
        }
    )
    return context.model_copy(
        update={
            "tool_results": [*context.tool_results, *results],
            "current_turn_observations": [
                *context.current_turn_observations,
                *successful_results,
            ],
            "action_history": action_history,
            "tool_states": tool_states,
            "authorized_actions": _remove_consumed_authorizations(
                context.authorized_actions,
                tool_states,
            ),
            "phase": next_phase if results else context.phase,
            "turn_context": envelope,
        },
        deep=True,
    )


def _compact_action_summary(value: str) -> str:
    return " ".join(str(value or "").split())[:240]


def _result_decision_summary(result: AgentToolResult) -> str:
    if result.status == "succeeded":
        return "工具已返回公开观察"
    if result.status == "already_completed":
        return "本会话已使用该工具，不重复调用"
    if result.status == "rejected":
        return "Runtime 未批准本次动作，未形成公开观察"
    if result.status == "unsupported":
        return "题目当前没有可用的该工具"
    if result.status == "timeout":
        return "本次调用超时，未形成公开观察"
    return "本次动作未形成公开观察"


def _update_tool_states(
    current: dict[str, ToolStateView],
    results: list[AgentToolResult],
) -> dict[str, ToolStateView]:
    states = {key: value.model_copy(deep=True) for key, value in current.items()}
    for result in results:
        if not result.tool_id:
            continue
        previous = states.get(result.tool_id, ToolStateView(state="available"))
        call_count = previous.call_count + 1
        common = {
            "call_count": call_count,
            "last_call_id": result.call_id,
        }
        if result.status in {"succeeded", "already_completed"}:
            states[result.tool_id] = previous.model_copy(
                update={
                    **common,
                    "state": "consumed",
                    "reason": "本会话已使用，不可重复调用",
                    "blocked_reason": "本会话已使用，不可重复调用",
                    "can_call": False,
                }
            )
        elif result.status == "unsupported":
            states[result.tool_id] = previous.model_copy(
                update={
                    **common,
                    "state": "unavailable",
                    "reason": "题目当前没有声明该工具",
                    "blocked_reason": "题目当前没有声明该工具",
                    "can_call": False,
                }
            )
        elif result.status == "rejected":
            states[result.tool_id] = previous.model_copy(
                update={
                    **common,
                    "state": "blocked",
                    "reason": "本轮动作未获 Runtime 批准，不重复尝试",
                    "blocked_reason": "本轮动作未获 Runtime 批准，不重复尝试",
                    "can_call": False,
                }
            )
        elif result.status in {"timeout", "failed"}:
            states[result.tool_id] = previous.model_copy(
                update={
                    **common,
                    "state": "failed_retryable",
                    "reason": "本次动作未形成公开观察",
                    "blocked_reason": "本次动作未形成公开观察",
                    "can_call": False,
                }
            )
        else:
            states[result.tool_id] = previous.model_copy(
                update={
                    **common,
                    "state": "attempted",
                    "reason": "本次动作未形成公开观察，前置条件或执行状态仍需由 Runtime 判断",
                    "can_call": False,
                }
            )
    return states


def _remove_consumed_authorizations(
    authorized_actions,
    tool_states: dict[str, ToolStateView],
):
    return [
        item
        for item in authorized_actions
        if tool_states.get(item.action_ref, ToolStateView(state="available")).state != "consumed"
    ]


def _normalize_execution_result(call: ToolCall, result: object, context: AgentContext) -> AgentToolResult:
    """把单个执行器异常收束为终态结果，不丢弃同批其它工具结果。

    异常正文属于内部实现细节，不能回灌模型或公开事件；学生只能知道本次
    观察没有形成。其它动作仍按正常结果继续进入下一轮。
    """

    if isinstance(result, AgentToolResult):
        return result
    entry = next((item for item in context.action_catalog if item.tool_id == call.tool_id), None)
    return AgentToolResult(
        call_id=call.call_id,
        tool_id=call.tool_id,
        tool_kind=entry.kind if entry is not None else "unknown",
        status="failed",
        error_code="tool_execution_failed",
    )
