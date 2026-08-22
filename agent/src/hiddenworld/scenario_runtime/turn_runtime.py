"""旧传输契约上的单 Agent 运行时适配器。

它把新的 ScenarioAgentLoop 接到现有 Go/Python V1 `AgentTurnResult`，让部署可以先
切换模型主链并验证性能，再在后续阶段替换 V2 事件与传输 DTO。确定性教学内核仍由
Runtime 持有，模型只看到 AgentContext。
"""

from __future__ import annotations

import logging
from dataclasses import replace
from time import perf_counter
from typing import Any

from hiddenworld.agents.scenario_agent import PydanticScenarioAgentRunner, _emit_stream_frames
from hiddenworld.contracts import (
    AgentTurnRequest,
    AgentToolResult,
    EvidenceRequest,
    EvidenceRequestView,
    AgentSemanticDecision,
    GuidanceState,
    TeachingDecision,
    AgentTurnResult,
    AuditTrace,
    GuardContext,
    HYPOTHESIS_OTHER,
    MentorAction,
    FinalReplyOutput,
    PublicAnswerComparison,
    PublicObservation,
    ToolCall,
    ToolCallsOutput,
    PublicTraceEvent,
    TurnAnalysis,
    TurnAssessment,
    TurnControl,
    UserActionAuthorization,
    VerificationResult,
    validate_scenario_contract,
)
from .state_reducer import StateReducer
from hiddenworld.kernel.cluegate import ClueGate
from hiddenworld.kernel.guard import Guard
from hiddenworld.contracts.assessment import direction_status_for_assessment
from hiddenworld.runtime import (
    PublicBoundaryRejected,
    TurnDeadlineExceeded,
    _coverage_label,
    _elapsed_ms,
    _first_release_category,
    _fallback_mentor_action,
    _forbidden_entities,
    _public_guard_observation_texts,
    _evidence_request_for_analysis,
    _new_items,
    _observe_actions,
    _public_trace_after_mentor,
    _public_trace_before_mentor,
    _reason_codes,
    _state_proposals,
)
from .agent_loop import (
    AgentLoop,
    AgentLoopBudgetExceeded,
    AgentLoopEvent,
    context_after_tool_results,
)
from .batch_scheduler import BatchScheduler, compile_tool_dependency_map
from .context import project_agent_context
from .virtual_tools import VirtualObservationExecutor


logger = logging.getLogger("hiddenworld.turn_runtime")


class SingleAgentRuntime:
    """以一个 ScenarioAgent 完成本轮理解、工具规划和最终回复。"""

    def __init__(self, agent: Any) -> None:
        self.agent = agent

    async def run_turn(
        self,
        request: AgentTurnRequest,
        *,
        on_turn_analysis=None,
        on_public_trace=None,
        on_reply_delta=None,
        on_reasoning_delta=None,
    ) -> AgentTurnResult:
        request.require_contract_version()
        if request.hidden_world.canonical_answer is not None:
            validate_scenario_contract(request.hidden_world)
        timeout_seconds = max(request.budget.deadline_ms, 1) / 1000
        started_at = perf_counter()
        phase_state = {"value": "deadline_guard"}

        def set_phase(phase: str) -> None:
            phase_state["value"] = phase

        logger.info(
            "[hiddenworld-turn-runtime] request_id=%s session_id=%s state_revision=%s "
            "budget_deadline_ms=%s timeout_seconds=%s structured_action=%s quick_action=%s "
            "phase=deadline_guard",
            request.request_id,
            request.session_id,
            request.state_revision,
            request.budget.deadline_ms,
            timeout_seconds,
            request.structured_user_action.action_id if request.structured_user_action else "",
            request.structured_user_action is not None and not request.user_message.strip(),
        )
        import asyncio

        try:
            return await asyncio.wait_for(
                self._run_turn(
                    request,
                    on_turn_analysis=on_turn_analysis,
                    on_public_trace=on_public_trace,
                    on_reply_delta=on_reply_delta,
                    on_reasoning_delta=on_reasoning_delta,
                    on_phase=set_phase,
                ),
                timeout=timeout_seconds,
            )
        except TurnDeadlineExceeded as exc:
            logger.error(
                "[hiddenworld-turn-runtime] request_id=%s session_id=%s state_revision=%s "
                "budget_deadline_ms=%s elapsed_ms=%s phase=%s "
                "error_type=%s detail=%s",
                request.request_id,
                request.session_id,
                request.state_revision,
                request.budget.deadline_ms,
                int((perf_counter() - started_at) * 1000),
                phase_state["value"],
                type(exc).__name__,
                str(exc),
            )
            raise
        except TimeoutError as exc:
            elapsed_ms = int((perf_counter() - started_at) * 1000)
            outer_deadline_reached = elapsed_ms >= max(1, request.budget.deadline_ms - 1000)
            timeout_origin = "outer_wait_for" if outer_deadline_reached else "inner_timeout_propagated"
            logger.error(
                "[hiddenworld-turn-runtime] request_id=%s session_id=%s state_revision=%s "
                "budget_deadline_ms=%s elapsed_ms=%s phase=%s timeout_origin=%s "
                "error_type=%s detail=%s",
                request.request_id,
                request.session_id,
                request.state_revision,
                request.budget.deadline_ms,
                elapsed_ms,
                phase_state["value"],
                timeout_origin,
                type(exc).__name__,
                str(exc),
            )
            raise TurnDeadlineExceeded("turn deadline exceeded") from exc

    async def _run_turn(
        self,
        request: AgentTurnRequest,
        *,
        on_turn_analysis,
        on_public_trace,
        on_reply_delta,
        on_reasoning_delta,
        on_phase=None,
    ):
        def mark_phase(phase: str) -> None:
            if on_phase is not None:
                on_phase(phase)

        mark_phase("context_projection")
        started = perf_counter()
        context = project_agent_context(request)
        authorization_context = context
        runner = self.agent
        if not isinstance(runner, PydanticScenarioAgentRunner) and hasattr(runner, "run"):
            # 测试和内部调用可直接注入 ScenarioAgentRunner；生产入口传入 Pydantic adapter。
            runner = self.agent
        # 题库当前没有可执行的工具依赖图字段；不把 query_patterns 误当成依赖。
        # 真正的跨轮依赖在下一阶段由 Runtime-owned ToolDependencyGraph 注入。
        scheduler = BatchScheduler(
            dependency_map=compile_tool_dependency_map(request.hidden_world),
        )
        executor = VirtualObservationExecutor(request)
        buffered_reply_deltas: list[str] = []

        async def buffer_reply_delta(text: str) -> None:
            if text:
                buffered_reply_deltas.append(text)

        loop_streamer = _LoopTraceStreamer(request, executor, on_public_trace)

        async def on_loop_event(event: AgentLoopEvent) -> None:
            if event.kind == "tool_batch_started":
                await loop_streamer.emit_tool_started(
                    event.payload
                )
            elif event.kind == "tool_result" and isinstance(event.payload, AgentToolResult):
                await loop_streamer.emit_tool_result(event.payload)

        quick_action_turn = request.structured_user_action is not None and not request.user_message.strip()
        if quick_action_turn:
            # QuickAction 是用户明确授权的动作，但仍必须经过 ScenarioAgent
            # 产生 tool_calls 后才能执行。此前这里先调用 executor，再调用模型，
            # 导致工具卡先于模型阶段出现，页面看起来像“按钮直接查数据”。
            action_id = request.structured_user_action.action_id
            quick_call = ToolCall(call_id=f"quick:{action_id}", tool_id=action_id)
            quick_rejection = scheduler.authorize_action(
                action_id,
                action_catalog=context.action_catalog,
                authorized_actions=context.authorized_actions,
                call_id=quick_call.call_id,
                tool_states=context.tool_states,
            )
            if quick_rejection is not None:
                # 无效 QuickAction 不能触碰虚拟世界；把拒绝结果作为模型输入，
                # 让导师生成安全的失败说明，且不发出任何观察正文。
                mark_phase("quick_action_rejected")
                rejected_context = context_after_tool_results(
                    context,
                    output=None,
                    results=[quick_rejection],
                ).model_copy(update={"authorized_actions": []})
                final_output = await runner.run(rejected_context)
                if isinstance(final_output, ToolCallsOutput):
                    final_output = await _retry_final_reply(
                        runner,
                        rejected_context,
                        feedback="tool_result_received_but_final_reply_missing",
                        on_reasoning_delta=on_reasoning_delta,
                    )
                    if final_output is None:
                        raise PublicBoundaryRejected(
                            "agent did not produce a final reply after the rejected action"
                        )
                events = [
                    AgentLoopEvent("tool_result", quick_rejection),
                    AgentLoopEvent("final_reply", final_output),
                ]
            else:
                # 合法 QuickAction 走与普通消息相同的模型→工具→模型循环，
                # 但把预算收窄到一次授权观察，避免点击动作触发工具枚举。
                mark_phase("quick_action_model")
                quick_loop = AgentLoop(
                    runner,
                    executor,
                    scheduler=scheduler,
                    max_model_rounds=3,
                    max_tool_calls=1,
                    on_reply_delta=buffer_reply_delta if on_reply_delta is not None else None,
                    on_reasoning_delta=on_reasoning_delta,
                    on_loop_event=on_loop_event,
                )
                try:
                    final_output, events = await quick_loop.run(context)
                except AgentLoopBudgetExceeded as exc:
                    raise PublicBoundaryRejected(str(exc)) from exc
                successful_quick_actions = {
                    item.payload.tool_id
                    for item in events
                    if item.kind == "tool_result"
                    and isinstance(item.payload, AgentToolResult)
                    and item.payload.status == "succeeded"
                }
                planned_quick_action = any(
                    item.kind == "tool_result"
                    and isinstance(item.payload, AgentToolResult)
                    and item.payload.tool_id == action_id
                    for item in events
                )
                if not planned_quick_action:
                    # 模型首轮若误返回 final_reply，给它一次明确的结构化约束
                    # 重试；仍不产生 tool_call 就整轮拒绝，绝不由 Runtime
                    # 偷执行来“修正”模型行为。
                    # 首轮可能已经通过流式接口产出了一段无效正文候选；
                    # 该候选没有经过工具观察和最终 Guard，不得混入重试后的
                    # 正文缓冲。提交屏障虽会再次校验，这里仍要先清空内存态，
                    # 避免调试流/回调消费者误把两次模型输出拼成一段正文。
                    buffered_reply_deltas.clear()
                    retry_context = context.model_copy(
                        update={"reply_feedback": "structured_action_requires_tool_call"}
                    )
                    try:
                        retry_output, retry_events = await quick_loop.run(retry_context)
                    except AgentLoopBudgetExceeded as exc:
                        raise PublicBoundaryRejected(str(exc)) from exc
                    final_output = retry_output
                    events.extend(retry_events)
                    successful_quick_actions = {
                        item.payload.tool_id
                        for item in events
                        if item.kind == "tool_result"
                        and isinstance(item.payload, AgentToolResult)
                        and item.payload.status == "succeeded"
                    }
                if action_id not in successful_quick_actions:
                    # 模型没有真正产生并执行点击对应的 tool_call，不能把
                    # 任何直接/猜测结果伪装成 QuickAction 观察。
                    raise PublicBoundaryRejected("agent did not plan the authorized quick action")
        else:
            scope = context.investigation_scope
            loop = AgentLoop(
                runner,
                executor,
                scheduler=scheduler,
                max_model_rounds=(scope.max_tool_calls + 2 if scope is not None else 6),
                max_tool_calls=(scope.max_tool_calls if scope is not None else 3),
                on_reply_delta=buffer_reply_delta if on_reply_delta is not None else None,
                on_reasoning_delta=on_reasoning_delta,
                on_loop_event=on_loop_event,
            )
            try:
                mark_phase("agent_loop")
                final_output, events = await loop.run(context)
            except AgentLoopBudgetExceeded as exc:
                raise PublicBoundaryRejected(str(exc)) from exc

        # AgentLoop 已经拿到 final_reply 或预算/错误终态；从这里开始不再接受
        # 新的 Observation 计划，TurnEnvelope 的生命周期进入 finalizing。
        mark_phase("finalizing")
        mark_phase("assessment")
        successful_actions = [
            item.payload.tool_id
            for item in events
            if item.kind == "tool_result" and getattr(item.payload, "status", "") == "succeeded"
        ]
        assessment = _assessment_from_single_agent(request, final_output, events, successful_actions)
        analysis = _analysis_from_single_agent(
            request,
            final_output,
            events,
            successful_actions,
            assessment=assessment,
        )

        authorizations = _authorizations_from_context(request, authorization_context)
        is_clarification = analysis.intent in {"clarification", "explanation_request"}
        actions = [] if is_clarification else [item for item in successful_actions if item]
        actions = list(dict.fromkeys(actions))
        effective_analysis = analysis.model_copy(update={"actions": actions})
        # QuickAction 的授权动作由模型 tool_call 触发并由 Runtime 执行，模型回注
        # 上下文里保留工具结果；对外传输的结构化
        # TurnAssessment 必须与最终 TurnAnalysis 共用同一份 Runtime 事实，
        # 否则 Go 会把它判定为跨层语义不一致。
        effective_assessment = assessment.model_copy(update={"actions": actions})
        guidance_scope = _guidance_scope_for_turn(request, effective_assessment)
        teaching_decision = _teaching_decision_from_agent(
            final_output,
            events,
            effective_assessment,
            guidance_scope=guidance_scope,
            has_observations=False,
        )
        # 先用同一个 StateReducer 入口取得本轮允许公开的证据，再过滤执行器
        # 返回；最终归约仍会在观察注入后重新计算状态、关系和教学约束。
        mark_phase("pre_reduction")
        pre_reduction = StateReducer().reduce(
            request,
            analysis=effective_analysis,
            observations=(),
            teaching_decision=teaching_decision,
            turn_assessment=effective_assessment,
            progress_assessment=assessment.progress_assessment,
            advance_state=False,
        )
        approved_releases = pre_reduction.approved_releases
        observations = []
        if not is_clarification:
            # 工具执行器已经完成一次确定性观察，兼容层只做授权/ClueGate 投影，
            # 不再重新调用 HiddenWorldEngine.observe。过滤逻辑与循环内实时旁路
            # 共用同一助手，保证两处外发内容一致。
            for action in actions:
                observation = _filtered_observation(
                    executor,
                    action,
                    approved_releases,
                    request.learner_state.collected_evidence,
                )
                if observation is not None:
                    observations.append(observation)
        if observations:
            teaching_decision = _teaching_decision_from_agent(
                final_output,
                events,
                effective_assessment,
                guidance_scope=guidance_scope,
                has_observations=True,
            )
        mark_phase("state_reduction")
        reduction = StateReducer().reduce(
            request,
            analysis=effective_analysis,
            observations=observations,
            teaching_decision=teaching_decision,
            turn_assessment=effective_assessment,
            progress_assessment=assessment.progress_assessment,
            advance_state=not is_clarification,
        )
        projected_state = reduction.projected_state
        answer_internal = None
        answer_public: PublicAnswerComparison | None = None
        answer_attempt_id = ""
        if analysis.contains_answer_attempt:
            mark_phase("compare_answer")
            from hiddenworld.agents.tools import CompareAnswerRuntime

            answer_attempt_id = f"{request.request_id}:answer"
            tool_runtime = CompareAnswerRuntime.bind_user_message(
                request_id=request.request_id,
                session_id=request.session_id,
                turn_id=request.request_id,
                revision=request.state_revision,
                world=request.hidden_world,
                learner_state=projected_state,
                analysis=analysis,
                user_message=request.user_message,
            )
            answer_public = tool_runtime.execute_bound()
            answer_internal = tool_runtime.internal_result

        mark_phase("final_reduction")
        reduction = StateReducer().reduce(
            request,
            analysis=effective_analysis,
            observations=observations,
            answer_comparison=answer_internal,
            teaching_decision=teaching_decision,
            turn_assessment=effective_assessment,
            progress_assessment=assessment.progress_assessment,
            advance_state=not is_clarification,
        )
        projected_state = reduction.projected_state
        relation = reduction.relation
        anti_guess = reduction.anti_guess

        ruled_out_this_turn = _new_items(
            request.learner_state.ruled_out_hypotheses,
            projected_state.ruled_out_hypotheses,
        )
        verification = VerificationResult(
            relation=(answer_internal.relation if answer_internal is not None else relation),
            coverage=(answer_internal.evidence_coverage if answer_internal is not None else anti_guess.coverage),
            completion_allowed=(
                answer_internal.completion_allowed
                if answer_internal is not None
                else anti_guess.completion_allowed
            ),
            ruled_out_this_turn=ruled_out_this_turn,
            answer_comparison=answer_internal,
        )
        constraints = reduction.constraints
        guidance_state = reduction.guidance_state.model_copy(
            update={
                # Go 侧会把公开 GuidanceState 与本轮 TurnAssessment 做同构复核；
                # 这里显式以 Runtime 当前归约的 assessment 覆盖旧导航信号，
                # 避免上一轮方向状态污染本轮正式提议。
                "direction_status": direction_status_for_assessment(effective_assessment),
                "progress_assessment": effective_assessment.progress_assessment,
            }
        )
        turn_control = reduction.turn_control
        normalized_primary_task = teaching_decision.primary_task
        if normalized_primary_task == "close_investigation" and not turn_control.completion_allowed:
            normalized_primary_task = (
                "correct_conclusion" if effective_assessment.contains_answer_attempt else "acknowledge_progress"
            )
        teaching_decision = teaching_decision.model_copy(
            update={
                "teaching_state": guidance_state.teaching_state,
                "primary_task": normalized_primary_task,
                "guidance_scope": guidance_scope,
            }
        )
        evidence_request = _evidence_request_for_analysis(analysis, request)

        guard_context = GuardContext(
            forbidden_entities=_forbidden_entities(request, projected_state),
            completion_allowed=verification.completion_allowed,
            guidance_scope=guidance_scope,
            may_release=reduction.approved_releases,
            current_user_message=request.user_message,
            evidence_request=_evidence_request_after_tool_results(
                analysis,
                events,
                request=request,
                fallback=evidence_request,
            ),
            public_observation_texts=_public_guard_observation_texts(
                request,
                projected_state,
                observations,
            ),
        )
        analysis = analysis.model_copy(
            update={
                "public_summary": _sanitize_public_summary(
                    analysis.public_summary,
                    constraints=constraints,
                    context=guard_context,
                )
            }
        )
        effective_analysis = effective_analysis.model_copy(
            update={"public_summary": analysis.public_summary}
        )
        guard_context = replace(
            guard_context,
            required_reply_mode=_required_reply_mode(events, observations),
        )
        action = _mentor_action_from_reply(
            final_output.reply,
            teaching_decision,
            reply_mode=getattr(final_output, "reply_mode", "acknowledgement"),
            required_reply_mode=guard_context.required_reply_mode,
        )
        mark_phase("reply_guard")
        guard_retry_count = 0
        try:
            action = Guard().validate(action, constraints=constraints, context=guard_context)
        except ValueError as exc:
            logger.warning(
                "[hiddenworld-reply-guard] request_id=%s session_id=%s phase=initial code=%s",
                request.request_id,
                request.session_id,
                getattr(exc, "code", "reply_guard_rejected"),
            )
            last_error = exc
            accepted = False
            for retry_index in range(1, 2):
                guard_retry_count += 1
                mark_phase("reply_guard_retry")
                retry_context = _reply_retry_context(
                    context,
                    events,
                    feedback=getattr(last_error, "code", "reply_guard_rejected"),
                    evidence_request=guard_context.evidence_request,
                )
                retry_output = await _retry_final_reply(
                    runner,
                    retry_context,
                    feedback=retry_context.reply_feedback,
                    on_reasoning_delta=on_reasoning_delta,
                )
                if retry_output is None:
                    break
                action = _mentor_action_from_reply(
                    retry_output.reply,
                    teaching_decision,
                    reply_mode=getattr(retry_output, "reply_mode", "acknowledgement"),
                    required_reply_mode=guard_context.required_reply_mode,
                )
                try:
                    action = Guard().validate(action, constraints=constraints, context=guard_context)
                    accepted = True
                    break
                except ValueError as retry_error:
                    logger.warning(
                        "[hiddenworld-reply-guard] request_id=%s session_id=%s phase=retry "
                        "attempt=%d code=%s",
                        request.request_id,
                        request.session_id,
                        retry_index,
                        getattr(retry_error, "code", "reply_guard_rejected"),
                    )
                    last_error = retry_error
            if not accepted:
                mark_phase("reply_guard_fallback")
                action = _fallback_mentor_action(
                    request=request,
                    analysis=analysis,
                    observations=observations,
                    constraints=constraints,
                    guard_context=guard_context,
                )
                try:
                    action = Guard().validate(action, constraints=constraints, context=guard_context)
                except ValueError as fallback_error:
                    logger.error(
                        "[hiddenworld-reply-guard] request_id=%s session_id=%s phase=fallback code=%s",
                        request.request_id,
                        request.session_id,
                        getattr(fallback_error, "code", "reply_guard_rejected"),
                    )
                    raise PublicBoundaryRejected(
                        "agent did not produce a reply accepted by the public boundary"
                    ) from fallback_error
        # 只有最终正文通过 Guard 后才把本轮结构化理解交给流式消费方；
        # 它包含模型生成的 public_summary，不能在失败/重生成前提前外发。
        mark_phase("public_stream_replay")
        if on_turn_analysis is not None:
            await on_turn_analysis(analysis)
        # 公开理解摘要和观察必须等最终回复通过 Guard 后再发送。模型流式输出
        # 只是私有候选，Guard 拒绝或重生成时不能让旧摘要残留在学生界面。
        # 循环内已经实时外发过的观察不再重复发送；序号接在旁路事件之后连续。
        trace_offset = len(loop_streamer.events)
        public_trace = _public_trace_before_mentor(
            public_summary=analysis.public_summary,
            observations=[
                item for item in observations if item.action not in loop_streamer.streamed_actions
            ],
            analysis_contains_answer=analysis.contains_answer_attempt,
            answer_attempt_id=answer_attempt_id,
            answer_public=answer_public,
            compare_answer_ms=0,
        )
        public_trace = [
            item.model_copy(update={"sequence": trace_offset + item.sequence})
            for item in public_trace
        ]
        await _emit_trace(on_public_trace, public_trace)
        final_trace = _public_trace_after_mentor(
            start_sequence=trace_offset + len(public_trace) + 1
        )
        public_trace.extend(final_trace)
        await _emit_trace(on_public_trace, final_trace)
        # 完整落库序列 = 循环旁路事件 + 收尾事件；非流式调用方从 result 拿到
        # 与流式路径同一份顺序。
        public_trace = [*loop_streamer.events, *public_trace]
        if on_reply_delta is not None:
            buffered_reply = "".join(buffered_reply_deltas)
            if buffered_reply == action.reply and buffered_reply:
                # Guard 通过后才公开候选正文，但不能因此把所有分片在同一
                # 个事件循环 tick 内一次性倾倒给浏览器；对已经通过的真实
                # 内容重新分帧，保留安全边界并恢复可感知的增量输出。
                for piece in buffered_reply_deltas:
                    await _emit_stream_frames(on_reply_delta, piece)
            else:
                # Guard/归一化改变了正文时，丢弃不一致的预览，发送最终安全正文一次。
                await _emit_stream_frames(on_reply_delta, action.reply)

        mark_phase("result_build")
        return AgentTurnResult(
            request_id=request.request_id,
            expected_revision=request.state_revision,
            reply=action.reply,
            turn_analysis=effective_analysis,
            turn_assessment=effective_assessment,
            teaching_decision=teaching_decision,
            guidance_state=guidance_state,
            turn_control=turn_control,
            proposals=_state_proposals(
                request.learner_state,
                projected_state,
                reply=action.reply,
            ),
            public_trace=public_trace,
            internal_verification=verification,
            internal_audit=AuditTrace(
                reason_codes=["single_agent_runtime", *_reason_codes(analysis, observations, answer_public)],
                mentor_rationale="single_agent_runtime",
                guard_retries=guard_retry_count,
                interpreter_ms=0,
                mentor_ms=_elapsed_ms(started),
            ),
        )


async def _emit_trace(callback, events: list[PublicTraceEvent]) -> None:
    if callback is None:
        return
    for event in events:
        await callback(event)


def _filtered_observation(executor, action, approved_releases, collected_evidence):
    """按本轮 ClueGate 批准投影可公开观察；循环内旁路与收尾共用同一规则。

    未获批证据对应的结果整体中性化，部分获批只裁剪 yields，正文保持不变。
    """
    observation = executor.executed_observations.get(action)
    if observation is None:
        return None
    allowed_yields = [
        item
        for item in observation.yields_evidence
        if item in approved_releases or item in collected_evidence
    ]
    if observation.yields_evidence and set(allowed_yields) != set(observation.yields_evidence):
        return observation.model_copy(
            update={
                "result": "本轮暂未形成新的可公开观察。",
                "is_negative": False,
                "yields_evidence": [],
                "rules_out": [],
            },
            deep=True,
        )
    if allowed_yields != observation.yields_evidence:
        return observation.model_copy(update={"yields_evidence": allowed_yields}, deep=True)
    return observation


class _LoopTraceStreamer:
    """循环内实时旁路：工具开始/结束即时外发，学生不再对着静默等待。

    安全边界与收尾一致：观察内容仍经 ClueGate（同一确定性函数，以累计成功
    动作调用，最终与收尾 pre_reduction 的输入完全相同）过滤后才能外发；
    未获批时外发的是中性占位，与收尾口径一字不差。序号从 1 连续分配，
    收尾事件必须接在旁路序号之后续编。
    """

    def __init__(self, request, executor, on_public_trace) -> None:
        self.request = request
        self.executor = executor
        self.on_public_trace = on_public_trace
        self.events: list[PublicTraceEvent] = []
        self.streamed_actions: set[str] = set()
        self._successful_actions: list[str] = []
        self._sequence = 0
        self._round = 0

    def _next_sequence(self) -> int:
        self._sequence += 1
        return self._sequence

    async def _emit(self, event: PublicTraceEvent) -> None:
        sequenced = event.model_copy(update={"sequence": self._next_sequence()})
        self.events.append(sequenced)
        if self.on_public_trace is not None:
            await self.on_public_trace(sequenced)

    async def emit_tool_started(self, calls) -> None:
        self._round += 1
        for call in calls:
            await self._emit(
                PublicTraceEvent(
                    sequence=0,
                    kind="agent_tool_started",
                    round=self._round,
                    call_id=call.call_id,
                    tool_name=call.tool_id,
                    status="started",
                )
            )

    async def emit_tool_result(self, result: AgentToolResult) -> None:
        if result.status != "succeeded":
            await self._emit(
                PublicTraceEvent(
                    sequence=0,
                    kind="agent_tool_result",
                    round=self._round,
                    call_id=result.call_id,
                    tool_name=result.tool_id,
                    status="failed",
                )
            )
            return
        if result.tool_id not in self._successful_actions:
            self._successful_actions.append(result.tool_id)
        approved = ClueGate().approve(
            self.request.hidden_world,
            actions=list(self._successful_actions),
            collected_evidence=self.request.learner_state.collected_evidence,
            max_releases=self.request.budget.max_releases,
        )
        observation = _filtered_observation(
            self.executor,
            result.tool_id,
            approved,
            self.request.learner_state.collected_evidence,
        )
        self.streamed_actions.add(result.tool_id)
        if observation is None:
            await self._emit(
                PublicTraceEvent(
                    sequence=0,
                    kind="agent_tool_result",
                    round=self._round,
                    call_id=result.call_id,
                    tool_name=result.tool_id,
                    status="completed",
                )
            )
            return
        await self._emit(
            PublicTraceEvent(
                sequence=0,
                kind="agent_tool_result",
                round=self._round,
                call_id=result.call_id,
                tool_name=result.tool_id,
                status="completed",
                observation=PublicObservation(
                    action=result.tool_id,
                    result=observation.result,
                    is_negative=observation.is_negative,
                ),
            )
        )


def _authorizations_from_context(request: AgentTurnRequest, context) -> list[UserActionAuthorization]:
    source = "structured_user_action" if request.structured_user_action is not None else "user_message"
    return [
        UserActionAuthorization(
            source=source,
            action_ref=item.action_ref,
            tool_kind=item.tool_kind,
            normalized_scope=item.normalized_scope,
            state_revision=request.state_revision,
            authorization_id=item.authorization_id,
        )
        for item in context.authorized_actions
    ]


def _assessment_from_single_agent(request, final_output, events, successful_actions) -> TurnAssessment:
    semantic = _semantic_from_agent_output(final_output, events)
    if semantic is None:
        # 兼容旧模型/测试替身时只依据 Runtime 已执行的动作降级，
        # 不重新解析用户中文，不猜答案、卡住或教学状态。
        structured_scope = ""
        if request.structured_user_action is not None:
            structured_scope = (
                request.structured_user_action.normalized_scope.strip()
                or request.structured_user_action.action_id.strip()
            )
        return TurnAssessment(
            intent="investigate" if successful_actions or request.structured_user_action is not None else "chat",
            requested_action=structured_scope,
            requested_action_raw=structured_scope,
            action_match_status="matched" if successful_actions else "none",
            actions=list(dict.fromkeys(successful_actions)),
            progress_assessment="progress" if successful_actions else "unknown",
            confidence=0.0,
        )
    assessment = TurnAssessment.model_validate(semantic.model_dump())
    # 模型有时把未规范化的用户动作填到 requested_action，却漏掉
    # requested_action_raw。TurnAnalysis 会用 requested_action 兜底生成原话，
    # 所以这里先补齐 raw，保证 Go 的 assessment/analysis 一致性校验不会把
    # 合法的闲聊或偏题回合误判成跨层语义冲突。
    if assessment.requested_action.strip() and not assessment.requested_action_raw.strip():
        assessment = assessment.model_copy(
            update={"requested_action_raw": assessment.requested_action.strip()}
        )
    runtime_stuck = assessment.is_stuck or assessment.intent in {
        "stuck",
        "help_request",
        "request_hint",
    }
    if request.learner_state.stalled_turns >= 1 and assessment.progress_assessment == "no_progress":
        runtime_stuck = True
    if runtime_stuck != assessment.is_stuck:
        assessment = assessment.model_copy(update={"is_stuck": runtime_stuck})
    assessment = _normalize_hypothesis_for_world(request, assessment)
    assessment = _normalize_claim_semantics(assessment)
    if request.structured_user_action is not None:
        # QuickAction 是后端签发的用户动作，模型不能把它改判成 chat，
        # 也不能覆盖已经签发的动作范围；是否产出观察仍以工具状态为准。
        structured_scope = (
            request.structured_user_action.normalized_scope.strip()
            or request.structured_user_action.action_id.strip()
        )
        return assessment.model_copy(
            update={
                "intent": "investigate",
                "requested_action": structured_scope,
                "requested_action_raw": structured_scope,
                "action_match_status": "matched",
                "actions": list(dict.fromkeys(successful_actions)),
                "progress_assessment": "progress" if successful_actions else "no_progress",
                "is_stuck": False,
                "is_noise": False,
                "made_claim": False,
                "claim_type": "none",
                "contains_answer_attempt": False,
                "answer_attempt_text": "",
                "confidence": max(assessment.confidence, 0.95),
                "concept_mastery_signals": {},
                "skill_mastery_signals": {},
                "preference_signals": {},
            }
        )
    return assessment


def _normalize_hypothesis_for_world(request, assessment: TurnAssessment) -> TurnAssessment:
    """把模型的假设连接收束到题目声明的 ID，不用字符串猜测替代语义判断。

    ``hypothesis_catalog`` 已经作为未标注候选表提供给模型。正常路径是模型直接
    返回精确候选 ID；这里只处理契约边界上的两类安全归约：

    - 自由假设或模型漏填 ID：归入题目声明的 ``H_OTHER``，保留学生自己的说法；
    - 模型自造 ID 且没有形成假设信号：清空，避免污染权威状态。

    绝不把自由文本按关键词强行映射到某个真实候选，更不根据候选顺序推断正确性。
    """

    hypothesis_id = assessment.hypothesis_id.strip()
    hypothesis_raw = assessment.hypothesis_raw.strip()
    declared_ids = request.hidden_world.hypothesis_ids()
    if hypothesis_id in declared_ids:
        if hypothesis_id == HYPOTHESIS_OTHER and not hypothesis_raw:
            hypothesis_raw = request.user_message.strip()
        return assessment.model_copy(
            update={
                "hypothesis_id": hypothesis_id,
                "hypothesis_raw": hypothesis_raw if hypothesis_id == HYPOTHESIS_OTHER else "",
            }
        )

    hypothesis_signal = (
        bool(hypothesis_raw)
        or assessment.intent in {"hypothesis", "answer", "answer_attempt"}
        or assessment.claim_type in {"hypothesis", "answer"}
        or (assessment.contains_answer_attempt and bool(assessment.answer_attempt_text.strip()))
    )
    if hypothesis_signal and HYPOTHESIS_OTHER in declared_ids:
        return assessment.model_copy(
            update={
                "hypothesis_id": HYPOTHESIS_OTHER,
                "hypothesis_raw": hypothesis_raw or request.user_message.strip(),
            }
        )

    return assessment.model_copy(update={"hypothesis_id": "", "hypothesis_raw": ""})


def _normalize_claim_semantics(assessment: TurnAssessment) -> TurnAssessment:
    """修复模型输出的跨字段矛盾；Go 侧会把这些组合判成整轮失败。

    实测模型会把"学生提出了假设方向但没有明确断言"输出成
    claim_type=hypothesis + made_claim=false——语义上讲得通，但
    validateScenarioAssessmentConsistency 要求两者严格联动。枚举比布尔
    更具体、也不会被 JSON 缺省吞掉，因此以 claim_type 为准回填布尔；
    answer_attempt 同理，以是否存在非空文本为准。
    """

    update: dict[str, object] = {}
    expected_claim = assessment.claim_type in {"observation", "hypothesis", "answer"}
    if assessment.made_claim != expected_claim:
        update["made_claim"] = expected_claim
    expected_attempt = bool(assessment.answer_attempt_text.strip())
    if assessment.contains_answer_attempt != expected_attempt:
        update["contains_answer_attempt"] = expected_attempt
    if not update:
        return assessment
    return assessment.model_copy(update=update)


def _analysis_from_single_agent(
    request,
    final_output,
    events,
    successful_actions,
    *,
    assessment: TurnAssessment | None = None,
) -> TurnAnalysis:
    text = request.user_message.strip()
    assessment = assessment or _assessment_from_single_agent(request, final_output, events, successful_actions)
    summary = next(
        (
            item.payload.public_summary.strip()
            for item in events
            if item.kind == "understanding"
            and hasattr(item.payload, "public_summary")
            and item.payload.public_summary.strip()
        ),
        getattr(final_output, "public_summary", None) or "",
    )
    return TurnAnalysis(
        public_summary=summary,
        intent=assessment.intent,
        requested_action_raw=assessment.requested_action_raw or assessment.requested_action or (text if successful_actions else ""),
        clarification_target=assessment.clarification_target,
        action_match_status=assessment.action_match_status,
        actions=list(dict.fromkeys(successful_actions)),
        hypothesis_id=assessment.hypothesis_id,
        hypothesis_raw=assessment.hypothesis_raw,
        made_claim=assessment.made_claim,
        contains_answer_attempt=assessment.contains_answer_attempt,
        answer_attempt_text=assessment.answer_attempt_text,
        established_facts=list(assessment.established_facts),
        is_stuck=assessment.is_stuck,
        is_noise=assessment.is_noise,
        student_affect=assessment.student_affect,
        confidence=assessment.confidence,
    )


def _semantic_from_agent_output(final_output, events) -> AgentSemanticDecision | None:
    """从最近一次安全模型输出读取语义字段，不从用户原文反推。"""

    semantic = getattr(final_output, "semantic", None)
    if semantic is not None:
        return semantic
    assessment = getattr(final_output, "turn_assessment", None)
    if assessment is not None:
        return AgentSemanticDecision.model_validate(assessment.model_dump())
    for item in reversed(events):
        semantic = getattr(item.payload, "semantic", None)
        if semantic is not None:
            return semantic
        assessment = getattr(item.payload, "turn_assessment", None)
        if assessment is not None:
            return AgentSemanticDecision.model_validate(assessment.model_dump())
    return None


def _teaching_decision_from_agent(
    final_output,
    events,
    assessment: TurnAssessment,
    *,
    guidance_scope: str,
    has_observations: bool,
) -> TeachingDecision:
    decision = getattr(final_output, "teaching_decision", None)
    if decision is None:
        for item in reversed(events):
            decision = getattr(item.payload, "teaching_decision", None)
            if decision is not None:
                break
    if decision is not None:
        # 结构化模型已经表达策略；引导等级仍由 Runtime 按当前学生状态复核。
        update = {
            "guidance_scope": guidance_scope,
            "primary_task": _primary_task_for_assessment(
                assessment,
                has_observations=has_observations,
                fallback=decision.primary_task,
            ),
        }
        if assessment.intent in {"investigate", "inspect"}:
            update["reply_policy"] = "tool_result_only" if has_observations else "acknowledgement"
        return decision.model_copy(
            update=update,
        )

    if has_observations:
        return TeachingDecision(
            teaching_state="normal_diagnosis",
            strategy="acknowledge",
            primary_task="interpret_evidence",
            reply_policy="tool_result_only",
            guidance_scope=guidance_scope,
        )
    if assessment.intent in {"chat", "off_topic", "garbage", "meta"}:
        return TeachingDecision(
            teaching_state="casual_chat" if assessment.intent == "chat" else ("garbage" if assessment.intent == "garbage" else "off_topic"),
            strategy="chat" if assessment.intent == "chat" else "recover",
            primary_task=(
                "acknowledge_progress" if assessment.intent == "chat" else "redirect_investigation"
            ),
            reply_policy="casual_reply",
            guidance_scope=guidance_scope,
        )
    if assessment.intent in {"clarification", "explanation_request", "help_request", "stuck"}:
        return TeachingDecision(
            teaching_state="clarification" if assessment.intent in {"clarification", "explanation_request"} else "guided_inquiry",
            strategy="reflect",
            primary_task=(
                "explain_concept"
                if assessment.intent in {"clarification", "explanation_request"}
                else "release_hint"
            ),
            reply_policy="reflective_question",
            guidance_scope=guidance_scope,
        )
    return TeachingDecision(
        teaching_state="normal_diagnosis",
        strategy="acknowledge",
        primary_task=_primary_task_for_assessment(
            assessment,
            has_observations=has_observations,
            fallback="acknowledge_progress",
        ),
        reply_policy="acknowledgement",
        guidance_scope=guidance_scope,
    )


def _guidance_scope_for_turn(request: AgentTurnRequest, assessment: TurnAssessment) -> str:
    """按公开学生状态计算教学引导上限，不从模型布尔字段放权。"""

    if request.structured_user_action is not None:
        return "none"
    if assessment.intent in {"chat", "off_topic", "garbage", "meta"}:
        return "none"
    if assessment.intent in {"help_request", "stuck", "request_hint"} or assessment.is_stuck:
        user_message = request.user_message.strip()
        if assessment.is_stuck and any(
            marker in user_message for marker in ("具体步骤", "具体检查", "直接告诉我", "给我步骤", "查什么")
        ):
            return "explicit"
        return "directional"
    if assessment.intent in {"clarification", "explanation_request"}:
        return "conceptual"
    return "none"


def _primary_task_for_assessment(
    assessment: TurnAssessment,
    *,
    has_observations: bool,
    fallback: str,
) -> str:
    if assessment.random_investigation or assessment.frustration_level == "high":
        return "redirect_investigation"
    if assessment.is_stuck or assessment.intent in {"stuck", "help_request", "request_hint"}:
        return "release_hint"
    if assessment.intent in {"clarification", "explanation_request"}:
        return "explain_concept"
    if assessment.contains_answer_attempt:
        return "correct_conclusion"
    if has_observations:
        return "interpret_evidence"
    return fallback


def _normalize_single_agent_reply(
    reply: str,
    decision: TeachingDecision,
    observations=(),
    **_,
) -> str:
    """只做传输层空白归一化，不改写模型语义或替换成固定话术。"""

    if decision.reply_policy == "no_reply":
        return ""
    return " ".join((reply or "").split())


def _mentor_action_from_reply(
    reply: str,
    decision: TeachingDecision,
    *,
    reply_mode: str | None = None,
    required_reply_mode: str | None = None,
) -> MentorAction:
    """把 Agent 的自然语言正文装入安全动作；不生成新的用户可见文案。"""

    return MentorAction(
        rationale="single_agent_runtime",
        reply=_normalize_single_agent_reply(reply, decision),
        # 行为模式由 Runtime 的工具事实优先决定；模型字段只保留作审计，
        # 防止 provider 因枚举缺省把失败动作误标成普通确认。
        reply_mode=required_reply_mode or reply_mode or "acknowledgement",
        requested_releases=[],
        confirms_hypothesis=False,
        expected_effort="moderate",
    )


def _required_reply_mode(events, observations) -> str | None:
    """由 Runtime 事实确定本轮正文模式，不接受模型自行放宽。"""

    if any(
        item.kind == "tool_result"
        and getattr(item.payload, "error_code", "") == "unmet_prerequisite"
        for item in events
    ):
        return "no_observation"
    if observations:
        return "observation"
    return None


def _sanitize_public_summary(summary: str, *, constraints, context: GuardContext) -> str:
    """理解摘要也走公开边界；拒绝时直接丢弃，不生成替代话术。"""

    candidate = _mentor_action_from_reply(
        summary,
        TeachingDecision(reply_policy="acknowledgement"),
    )
    try:
        Guard().validate(candidate, constraints=constraints, context=context)
    except ValueError:
        return ""
    return candidate.reply


def _reply_retry_context(context, events, *, feedback: str, evidence_request=None):
    """把已经执行的公开工具结果回注给一次回复重生成。"""

    retry_context = context
    for event in events:
        if event.kind == "understanding" and isinstance(event.payload, ToolCallsOutput):
            retry_context = context_after_tool_results(
                retry_context,
                output=event.payload,
                results=[],
            )
        elif event.kind in {"tool_result", "tool_rejected"} and isinstance(event.payload, AgentToolResult):
            retry_context = context_after_tool_results(
                retry_context,
                output=None,
                results=[event.payload],
            )
        elif event.kind == "tool_deferred" and isinstance(event.payload, ToolCall):
            retry_context = context_after_tool_results(
                retry_context,
                output=None,
                results=[
                    AgentToolResult(
                        call_id=event.payload.call_id,
                        tool_id=event.payload.tool_id,
                        tool_kind="",
                        status="rejected",
                        error_code="dependency_deferred",
                    )
                ],
            )
    update = {
        "authorized_actions": [],
        "reply_feedback": _reply_guard_feedback(feedback),
    }
    if evidence_request is not None:
        update["evidence_request"] = EvidenceRequestView(
            requested_text=evidence_request.requested_text,
            availability=evidence_request.availability,
            public_message=evidence_request.public_message,
        )
    return retry_context.model_copy(
        update={
            **update,
        },
        deep=True,
    )


def _evidence_request_after_tool_results(
    analysis: TurnAnalysis,
    events,
    *,
    request: AgentTurnRequest,
    fallback: EvidenceRequest | None,
) -> EvidenceRequest | None:
    """把前置条件失败投影为不可用证据，阻止模型把失败当成观察完成。

    ``VirtualObservationExecutor`` 用结构化 error_code 表达前置条件未满足；
    这里把它接入 Guard 的证据边界，而不是从失败文本猜测含义。
    """

    if not any(
        item.kind == "tool_result"
        and getattr(item.payload, "error_code", "") == "unmet_prerequisite"
        for item in events
    ):
        return fallback
    requested = (
        analysis.requested_action_raw.strip()
        or (
            request.structured_user_action.normalized_scope.strip()
            if request.structured_user_action is not None
            else ""
        )
    )
    return EvidenceRequest(
        requested_text=requested,
        availability="PREREQUISITE_UNMET",
        public_message="本次检查缺少形成观察所需的已知对象或前置信息。",
    )


async def _retry_final_reply(runner, context, *, feedback: str, on_reasoning_delta=None):
    """在安全边界拒绝正文时请求一次新的 final_reply；不执行工具。"""

    retry_context = context
    if not retry_context.reply_feedback:
        retry_context = retry_context.model_copy(
            update={"reply_feedback": _reply_guard_feedback(feedback)},
            deep=True,
        )
    run_stream = getattr(runner, "run_stream", None)
    if on_reasoning_delta is not None and callable(run_stream):
        # 正文仍留在私有结果中，只有显式测试开关开启的原始 thinking 旁路继续
        # 输出，避免 Guard 重写阶段只剩一个没有内容的“思考中…”占位。
        output = await run_stream(
            retry_context,
            on_reasoning_delta=on_reasoning_delta,
        )
    else:
        output = await runner.run(retry_context)
    return output if isinstance(output, FinalReplyOutput) else None


def _reply_guard_feedback(feedback: str) -> str:
    """把边界失败转成模型可执行的重写约束，不生成任何固定用户文案。"""

    if feedback == "reply_repeats_observation":
        return (
            "reply_guard:reply_repeats_observation; "
            "公开观察已经由独立卡片展示；只写新的教学承接、关联或反思，"
            "不要重述日志、指标、返回码、成功率、超时或失败细节。"
        )
    return f"reply_guard:{feedback}"
