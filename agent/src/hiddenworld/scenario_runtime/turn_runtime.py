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
    UserActionAuthorization,
    VerificationResult,
    validate_scenario_contract,
)
from hiddenworld.kernel import AntiGuess, ClueGate, EvidenceEngine, RootCauseVerifier, TeachingPolicy
from hiddenworld.kernel.guard import Guard
from hiddenworld.runtime import (
    STALL_UNLOCK_THRESHOLD,
    TurnDeadlineExceeded,
    _coverage_label,
    _elapsed_ms,
    _fallback_mentor_action,
    _first_release_category,
    _forbidden_entities,
    _new_items,
    _normalize_mentor_reply,
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
                final_output = FinalReplyOutput(
                    kind="final_reply",
                    reply="这次快捷检查的公开结果已经返回，请根据结果继续判断。",
                )
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
        analysis = _analysis_from_single_agent(request, final_output, events, successful_actions)
        if on_turn_analysis is not None:
            await on_turn_analysis(analysis)

        authorizations = _authorizations_from_context(request, authorization_context)
        is_clarification = analysis.intent in {"clarification", "explanation_request"}
        actions = [] if is_clarification else [item for item in successful_actions if item]
        actions = list(dict.fromkeys(actions))
        effective_analysis = analysis.model_copy(update={"actions": actions})
        approved_releases = (
            []
            if is_clarification
            else ClueGate().approve(
                request.hidden_world,
                actions=actions,
                collected_evidence=request.learner_state.collected_evidence,
                max_releases=request.budget.max_releases,
            )
        )
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
        projected_state = (
            request.learner_state.model_copy(deep=True)
            if is_clarification
            else EvidenceEngine().advance(
                request.learner_state,
                analysis=effective_analysis,
                observations=observations,
            )
        )
        relation = RootCauseVerifier().relation(
            request.hidden_world,
            hypothesis_id=effective_analysis.hypothesis_id,
            learner_state=projected_state,
        )
        anti_guess = AntiGuess().evaluate(
            request.hidden_world,
            collected_evidence=projected_state.collected_evidence,
            relation=relation,
        )
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
        constraints = TeachingPolicy().compile(
            projected_state,
            analysis=effective_analysis,
            completion_allowed=verification.completion_allowed,
            evidence_coverage=_coverage_label(projected_state, anti_guess.best_evidence_set),
            may_release=approved_releases,
            allowed_category=_first_release_category(request, approved_releases),
            contradictions=(answer_internal.contradictions if answer_internal is not None else []),
        )

        public_trace = _public_trace_before_mentor(
            public_summary=analysis.public_summary,
            observations=observations,
            analysis_contains_answer=analysis.contains_answer_attempt,
            answer_attempt_id=answer_attempt_id,
            answer_public=answer_public,
            compare_answer_ms=0,
        )
        await _emit_trace(on_public_trace, public_trace)

        reply = _normalize_mentor_reply(request, analysis, observations, final_output.reply)
        action = MentorAction(
            rationale="single_agent_runtime",
            reply=reply,
            requested_releases=[],
            confirms_hypothesis=False,
            expected_effort="moderate",
        )
        try:
            action = Guard().validate(
                action,
                constraints=constraints,
                context=GuardContext(
                    forbidden_entities=_forbidden_entities(request, projected_state),
                    completion_allowed=verification.completion_allowed,
                    may_release=approved_releases,
                ),
            )
        except ValueError:
            action = _fallback_mentor_action(
                request=request,
                analysis=analysis,
                observations=observations,
                constraints=constraints,
                guard_context=GuardContext(
                    forbidden_entities=_forbidden_entities(request, projected_state),
                    completion_allowed=verification.completion_allowed,
                    may_release=approved_releases,
                ),
            )
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


def _analysis_from_single_agent(request, final_output, events, successful_actions) -> TurnAnalysis:
    text = request.user_message.strip()
    lowered = text.casefold()
    hypothesis_id = ""
    for hypothesis in request.hidden_world.hypotheses:
        if hypothesis.label and hypothesis.label.casefold() in lowered:
            hypothesis_id = hypothesis.hypothesis_id
            break
    contains_answer = bool(
        text
        and any(marker in text for marker in ("我认为", "根因", "原因是", "问题在", "应该是", "因为"))
        and not text.endswith("?")
        and "？" not in text
    )
    intent = "investigate" if successful_actions or request.structured_user_action is not None else "chat"
    if any(marker in text for marker in ("什么意思", "怎么理解", "为什么", "解释一下")):
        intent = "clarification"
    summary = next(
        (str(item.payload).strip() for item in events if item.kind == "understanding" and str(item.payload).strip()),
        text[:80] or "我先根据当前公开信息继续处理。",
    )
    return TurnAnalysis(
        public_summary=summary,
        intent=intent,
        requested_action_raw=text if successful_actions else "",
        clarification_target="" if intent != "clarification" else text,
        action_match_status="matched" if successful_actions else "none",
        actions=list(dict.fromkeys(successful_actions)),
        hypothesis_id=hypothesis_id,
        hypothesis_raw="",
        made_claim=contains_answer,
        contains_answer_attempt=contains_answer,
        answer_attempt_text=text if contains_answer else "",
        established_facts=[],
        is_stuck=any(marker in text for marker in ("不知道", "不知从", "卡住", "没头绪")),
        is_noise=False,
        student_affect="frustrated" if any(marker in text for marker in ("烦", "崩溃", "还是不行")) else "engaged",
        confidence=0.9,
    )
