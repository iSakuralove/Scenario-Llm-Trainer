"""旧传输契约上的单 Agent 运行时适配器。

它把新的 ScenarioAgentLoop 接到现有 Go/Python V1 `AgentTurnResult`，让部署可以先
切换模型主链并验证性能，再在后续阶段替换 V2 事件与传输 DTO。确定性教学内核仍由
Runtime 持有，模型只看到 AgentContext。
"""

from __future__ import annotations

from time import perf_counter
from typing import Any

from hiddenworld.agents.scenario_agent import PydanticScenarioAgentRunner
from hiddenworld.contracts import (
    AgentTurnRequest,
    AgentToolResult,
    EvidenceRequestView,
    AgentSemanticDecision,
    GuidanceState,
    TeachingDecision,
    AgentTurnResult,
    AnswerAttempt,
    AuditTrace,
    GuardContext,
    MentorAction,
    FinalReplyOutput,
    PublicAnswerComparison,
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
from hiddenworld.kernel.guard import Guard
from hiddenworld.runtime import (
    STALL_UNLOCK_THRESHOLD,
    TurnDeadlineExceeded,
    _coverage_label,
    _elapsed_ms,
    _first_release_category,
    _forbidden_entities,
    _evidence_request_for_analysis,
    _new_items,
    _observe_actions,
    _public_trace_after_mentor,
    _public_trace_before_mentor,
    _reason_codes,
    _state_proposals,
)
from .agent_loop import AgentLoop, AgentLoopBudgetExceeded, AgentLoopEvent
from .batch_scheduler import BatchScheduler
from .context import project_agent_context
from .virtual_tools import VirtualObservationExecutor


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
    ) -> AgentTurnResult:
        request.require_contract_version()
        if request.hidden_world.canonical_answer is not None:
            validate_scenario_contract(request.hidden_world)
        timeout_seconds = max(request.budget.deadline_ms, 1) / 1000
        import asyncio

        try:
            return await asyncio.wait_for(
                self._run_turn(
                    request,
                    on_turn_analysis=on_turn_analysis,
                    on_public_trace=on_public_trace,
                    on_reply_delta=on_reply_delta,
                ),
                timeout=timeout_seconds,
            )
        except TimeoutError as exc:
            raise TurnDeadlineExceeded("turn deadline exceeded") from exc

    async def _run_turn(self, request: AgentTurnRequest, *, on_turn_analysis, on_public_trace, on_reply_delta):
        started = perf_counter()
        context = project_agent_context(request)
        authorization_context = context
        runner = self.agent
        if not isinstance(runner, PydanticScenarioAgentRunner) and hasattr(runner, "run"):
            # 测试和内部调用可直接注入 ScenarioAgentRunner；生产入口传入 Pydantic adapter。
            runner = self.agent
        # 题库当前没有可执行的工具依赖图字段；不把 query_patterns 误当成依赖。
        # 真正的跨轮依赖在下一阶段由 Runtime-owned ToolDependencyGraph 注入。
        scheduler = BatchScheduler()
        executor = VirtualObservationExecutor(request)
        buffered_reply_deltas: list[str] = []

        async def buffer_reply_delta(text: str) -> None:
            if text:
                buffered_reply_deltas.append(text)

        quick_action_turn = request.structured_user_action is not None and not request.user_message.strip()
        if quick_action_turn:
            # QuickAction 已经是用户明确授权的动作：先由 Runtime 本地执行，
            # 再让同一个 ScenarioAgent 读取结果并生成一次最终回复，不经过
            # tool_calls 规划，因此不会为一次点击额外请求第二轮模型。
            action_id = request.structured_user_action.action_id
            quick_result = await executor.execute(
                ToolCall(call_id=f"quick:{action_id}", tool_id=action_id),
                context,
            )
            model_context = context.model_copy(
                update={
                    "tool_results": [quick_result],
                    # 动作已经执行完，禁止模型再次规划同一个观察。
                    "authorized_actions": [],
                }
            )
            run_stream = getattr(runner, "run_stream", None)
            if on_reply_delta is not None and callable(run_stream):
                final_output = await run_stream(model_context, on_reply_delta=buffer_reply_delta)
            else:
                final_output = await runner.run(model_context)
            if isinstance(final_output, ToolCallsOutput):
                final_output = await _retry_final_reply(
                    runner,
                    model_context,
                    feedback="tool_result_received_but_final_reply_missing",
                )
                if final_output is None:
                    raise TurnDeadlineExceeded("agent did not produce a final reply after the authorized observation")
            events = [
                AgentLoopEvent("tool_result", quick_result),
                AgentLoopEvent("final_reply", final_output),
            ]
        else:
            loop = AgentLoop(
                runner,
                executor,
                scheduler=scheduler,
                max_model_rounds=11,
                max_tool_calls=10,
                on_reply_delta=buffer_reply_delta if on_reply_delta is not None else None,
            )
            try:
                final_output, events = await loop.run(context)
            except AgentLoopBudgetExceeded as exc:
                raise TurnDeadlineExceeded(str(exc)) from exc

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
        if on_turn_analysis is not None:
            await on_turn_analysis(analysis)

        authorizations = _authorizations_from_context(request, authorization_context)
        is_clarification = analysis.intent in {"clarification", "explanation_request"}
        actions = [] if is_clarification else [item for item in successful_actions if item]
        actions = list(dict.fromkeys(actions))
        effective_analysis = analysis.model_copy(update={"actions": actions})
        # QuickAction 已由 Runtime 直接执行，模型回注上下文里故意没有授权动作，
        # 因此模型返回的 assessment.actions 可能为空；对外传输的结构化
        # TurnAssessment 必须与最终 TurnAnalysis 共用同一份 Runtime 事实，
        # 否则 Go 会把它判定为跨层语义不一致。
        effective_assessment = assessment.model_copy(update={"actions": actions})
        teaching_decision = _teaching_decision_from_agent(
            final_output, events, effective_assessment, has_observations=False
        )
        # 先用同一个 StateReducer 入口取得本轮允许公开的证据，再过滤执行器
        # 返回；最终归约仍会在观察注入后重新计算状态、关系和教学约束。
        pre_reduction = StateReducer().reduce(
            request,
            analysis=effective_analysis,
            observations=(),
            teaching_decision=teaching_decision,
            progress_assessment=assessment.progress_assessment,
            advance_state=False,
        )
        approved_releases = pre_reduction.approved_releases
        observations = []
        if not is_clarification:
            # 工具执行器已经完成一次确定性观察，兼容层只做授权/ClueGate 投影，
            # 不再重新调用 HiddenWorldEngine.observe。
            executed = executor.executed_observations
            for action in actions:
                observation = executed.get(action)
                if observation is None:
                    continue
                allowed_yields = [
                    item
                    for item in observation.yields_evidence
                    if item in approved_releases or item in request.learner_state.collected_evidence
                ]
                if observation.yields_evidence and set(allowed_yields) != set(observation.yields_evidence):
                    observation = observation.model_copy(
                        update={
                            "result": "本轮暂未形成新的可公开观察。",
                            "is_negative": False,
                            "yields_evidence": [],
                            "rules_out": [],
                        },
                        deep=True,
                    )
                elif allowed_yields != observation.yields_evidence:
                    observation = observation.model_copy(update={"yields_evidence": allowed_yields}, deep=True)
                observations.append(observation)
        if observations:
            teaching_decision = _teaching_decision_from_agent(
                final_output, events, effective_assessment, has_observations=True
            )
        reduction = StateReducer().reduce(
            request,
            analysis=effective_analysis,
            observations=observations,
            teaching_decision=teaching_decision,
            progress_assessment=assessment.progress_assessment,
            advance_state=not is_clarification,
        )
        projected_state = reduction.projected_state
        answer_internal = None
        answer_public: PublicAnswerComparison | None = None
        answer_attempt_id = ""
        if analysis.contains_answer_attempt:
            attempt = AnswerAttempt(
                answer_attempt_id=f"{request.request_id}:answer",
                session_id=request.session_id,
                turn_id=request.request_id,
                revision=request.state_revision,
                text=analysis.answer_attempt_text,
            )
            from hiddenworld.agents.tools import CompareAnswerRuntime

            tool_runtime = CompareAnswerRuntime(
                request_id=request.request_id,
                session_id=request.session_id,
                turn_id=request.request_id,
                revision=request.state_revision,
                world=request.hidden_world,
                learner_state=projected_state,
                analysis=analysis,
                attempts={attempt.answer_attempt_id: attempt},
            )
            answer_public = tool_runtime.execute_bound()
            answer_internal = tool_runtime.internal_result
            answer_attempt_id = attempt.answer_attempt_id

        reduction = StateReducer().reduce(
            request,
            analysis=effective_analysis,
            observations=observations,
            answer_comparison=answer_internal,
            teaching_decision=teaching_decision,
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
            relation=relation,
            coverage=anti_guess.coverage,
            completion_allowed=anti_guess.completion_allowed,
            ruled_out_this_turn=ruled_out_this_turn,
            answer_comparison=answer_internal,
        )
        constraints = reduction.constraints
        guidance_state = reduction.guidance_state
        turn_control = reduction.turn_control
        evidence_request = _evidence_request_for_analysis(analysis)

        public_trace = _public_trace_before_mentor(
            public_summary=analysis.public_summary,
            observations=observations,
            analysis_contains_answer=analysis.contains_answer_attempt,
            answer_attempt_id=answer_attempt_id,
            answer_public=answer_public,
            compare_answer_ms=0,
        )
        await _emit_trace(on_public_trace, public_trace)

        guard_context = GuardContext(
            forbidden_entities=_forbidden_entities(request, projected_state),
            completion_allowed=verification.completion_allowed,
            may_release=reduction.approved_releases,
            evidence_request=evidence_request,
            public_observation_texts=[item.result for item in observations],
        )
        action = _mentor_action_from_reply(final_output.reply, teaching_decision)
        try:
            action = Guard().validate(action, constraints=constraints, context=guard_context)
        except ValueError as exc:
            last_error = exc
            accepted = False
            for _ in range(2):
                retry_context = _reply_retry_context(
                    context,
                    events,
                    feedback=getattr(last_error, "code", "reply_guard_rejected"),
                    evidence_request=evidence_request,
                )
                retry_output = await _retry_final_reply(
                    runner,
                    retry_context,
                    feedback=retry_context.reply_feedback,
                )
                if retry_output is None:
                    break
                action = _mentor_action_from_reply(retry_output.reply, teaching_decision)
                try:
                    action = Guard().validate(action, constraints=constraints, context=guard_context)
                    accepted = True
                    break
                except ValueError as retry_error:
                    last_error = retry_error
            else:
                raise TurnDeadlineExceeded(
                    "agent did not produce a reply accepted by the public boundary"
                ) from last_error
            if not accepted:
                raise TurnDeadlineExceeded(
                    "agent did not produce a reply accepted by the public boundary"
                ) from last_error
        final_trace = _public_trace_after_mentor(start_sequence=len(public_trace) + 1)
        public_trace.extend(final_trace)
        await _emit_trace(on_public_trace, final_trace)
        if on_reply_delta is not None:
            buffered_reply = "".join(buffered_reply_deltas)
            if buffered_reply == action.reply and buffered_reply:
                for piece in buffered_reply_deltas:
                    await on_reply_delta(piece)
            else:
                # Guard/归一化改变了正文时，丢弃不一致的预览，发送最终安全正文一次。
                await on_reply_delta(action.reply)

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
                interpreter_ms=0,
                mentor_ms=_elapsed_ms(started),
            ),
        )


async def _emit_trace(callback, events: list[PublicTraceEvent]) -> None:
    if callback is None:
        return
    for event in events:
        await callback(event)


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
        return TurnAssessment(
            intent="investigate" if successful_actions or request.structured_user_action is not None else "chat",
            action_match_status="matched" if successful_actions else "none",
            actions=list(dict.fromkeys(successful_actions)),
            progress_assessment="progress" if successful_actions else "unknown",
            confidence=0.0,
        )
    return TurnAssessment.model_validate(semantic.model_dump())


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
    has_observations: bool,
) -> TeachingDecision:
    decision = getattr(final_output, "teaching_decision", None)
    if decision is None:
        for item in reversed(events):
            decision = getattr(item.payload, "teaching_decision", None)
            if decision is not None:
                break
    if decision is not None:
        # 结构化模型已经表达策略；权限字段仍由 Runtime 强制关闭。
        return decision.model_copy(
            update={
                "allow_explicit_next_step": False,
                "allow_ruled_out_scope": False,
            }
        )

    if has_observations:
        return TeachingDecision(
            teaching_state="normal_diagnosis",
            strategy="acknowledge",
            reply_policy="tool_result_only",
        )
    if assessment.intent in {"chat", "off_topic", "garbage", "meta"}:
        return TeachingDecision(
            teaching_state="casual_chat" if assessment.intent == "chat" else ("garbage" if assessment.intent == "garbage" else "off_topic"),
            strategy="chat" if assessment.intent == "chat" else "recover",
            reply_policy="casual_reply",
        )
    if assessment.intent in {"clarification", "explanation_request", "help_request", "stuck"}:
        return TeachingDecision(
            teaching_state="clarification" if assessment.intent in {"clarification", "explanation_request"} else "guided_inquiry",
            strategy="reflect",
            reply_policy="reflective_question",
        )
    return TeachingDecision(
        teaching_state="normal_diagnosis",
        strategy="acknowledge",
        reply_policy="acknowledgement",
    )


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


def _mentor_action_from_reply(reply: str, decision: TeachingDecision) -> MentorAction:
    """把 Agent 的自然语言正文装入安全动作；不生成新的用户可见文案。"""

    return MentorAction(
        rationale="single_agent_runtime",
        reply=_normalize_single_agent_reply(reply, decision),
        requested_releases=[],
        confirms_hypothesis=False,
        expected_effort="moderate",
    )


def _reply_retry_context(context, events, *, feedback: str, evidence_request=None):
    """把已经执行的公开工具结果回注给一次回复重生成。"""

    tool_results = [
        item.payload
        for item in events
        if item.kind == "tool_result" and isinstance(item.payload, AgentToolResult)
    ]
    update = {
        "authorized_actions": [],
        "tool_results": [*context.tool_results, *tool_results],
        "reply_feedback": f"reply_guard:{feedback}",
    }
    if evidence_request is not None:
        update["evidence_request"] = EvidenceRequestView(
            requested_text=evidence_request.requested_text,
            availability=evidence_request.availability,
        )
    return context.model_copy(
        update={
            **update,
        },
        deep=True,
    )


async def _retry_final_reply(runner, context, *, feedback: str):
    """在安全边界拒绝正文时请求一次新的 final_reply；不执行工具。"""

    retry_context = context
    if not retry_context.reply_feedback:
        retry_context = retry_context.model_copy(
            update={"reply_feedback": f"reply_guard:{feedback}"},
            deep=True,
        )
    output = await runner.run(retry_context)
    return output if isinstance(output, FinalReplyOutput) else None
