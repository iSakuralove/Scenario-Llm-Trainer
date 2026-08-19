"""HiddenWorld 单轮主链：两个 LLM 调用夹着确定性教学内核。"""

from __future__ import annotations

import asyncio
from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from time import perf_counter
from typing import Any

from hiddenworld.agents.tools import CompareAnswerRuntime
from hiddenworld.contracts import (
    AgentTurnRequest,
    AgentTurnResult,
    AnswerAttempt,
    AuditTrace,
    GuardContext,
    InterpreterDeps,
    LearnerState,
    LearnerStateView,
    MentorDeps,
    Observation,
    Proposal,
    PublicAnswerComparison,
    PublicReasoningSummary,
    PublicTraceEvent,
    ToolEventPayload,
    VerificationResult,
)
from hiddenworld.kernel import (
    AntiGuess,
    ClueGate,
    EvidenceEngine,
    HiddenWorldEngine,
    RootCauseVerifier,
    TeachingPolicy,
)
from hiddenworld.kernel.guard import extract_forbidden_entities
from hiddenworld.retry import run_with_network_retries


class TurnDeadlineExceeded(TimeoutError):
    """单轮总 deadline 已耗尽；不得在后台继续执行或重放工具。"""


PublicTraceCallback = Callable[[PublicTraceEvent], Awaitable[None]]
TurnAnalysisCallback = Callable[[Any], Awaitable[None]]


@dataclass
class HiddenWorldRuntime:
    """无状态主链；Go 仍负责 revision、幂等审批和持久化。"""

    interpreter: Any
    mentor: Any

    async def run_turn(
        self,
        request: AgentTurnRequest,
        *,
        on_turn_analysis: TurnAnalysisCallback | None = None,
        on_public_trace: PublicTraceCallback | None = None,
    ) -> AgentTurnResult:
        request.require_contract_version()
        timeout_seconds = max(request.budget.deadline_ms, 1) / 1000
        try:
            return await asyncio.wait_for(
                self._run_turn(
                    request,
                    on_turn_analysis=on_turn_analysis,
                    on_public_trace=on_public_trace,
                ),
                timeout=timeout_seconds,
            )
        except TimeoutError as exc:
            raise TurnDeadlineExceeded("turn deadline exceeded") from exc

    async def _run_turn(
        self,
        request: AgentTurnRequest,
        *,
        on_turn_analysis: TurnAnalysisCallback | None,
        on_public_trace: PublicTraceCallback | None,
    ) -> AgentTurnResult:
        interpreter_started = perf_counter()
        interpreter_deps = InterpreterDeps(
            public_scenario=request.public_scenario,
            hypotheses=request.hidden_world.hypotheses,
            transcript=request.transcript,
            known_actions=[item.action for item in request.hidden_world.observations],
        )
        interpreter_result = await run_with_network_retries(
            lambda: self.interpreter.run(
                request.user_message,
                deps=interpreter_deps,
            )
        )
        interpreter_ms = _elapsed_ms(interpreter_started)
        analysis = interpreter_result.output
        if on_turn_analysis is not None:
            await on_turn_analysis(analysis)

        actions = [] if analysis.is_low_confidence() or analysis.is_noise else analysis.actions
        approved_releases = ClueGate().approve(
            request.hidden_world,
            actions=actions,
            collected_evidence=request.learner_state.collected_evidence,
            max_releases=request.budget.max_releases,
        )
        observations = _observe_actions(
            request,
            actions=actions,
            approved_releases=approved_releases,
        )
        projected_state = EvidenceEngine().advance(
            request.learner_state,
            analysis=analysis,
            observations=observations,
        )

        relation = RootCauseVerifier().relation(
            request.hidden_world,
            hypothesis_id=(analysis.hypothesis_id if not analysis.is_low_confidence() else ""),
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
        compare_answer_ms = 0
        if analysis.contains_answer_attempt and not analysis.is_low_confidence():
            attempt = AnswerAttempt(
                answer_attempt_id=f"{request.request_id}:answer",
                session_id=request.session_id,
                turn_id=request.request_id,
                revision=request.state_revision,
                text=analysis.answer_attempt_text,
            )
            answer_attempt_id = attempt.answer_attempt_id
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
            compare_started = perf_counter()
            answer_public = tool_runtime.execute(attempt.answer_attempt_id)
            compare_answer_ms = _elapsed_ms(compare_started)
            answer_internal = tool_runtime.internal_result

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
        allowed_category = _first_release_category(request, approved_releases)
        constraints = TeachingPolicy().compile(
            projected_state,
            analysis=analysis,
            completion_allowed=verification.completion_allowed,
            evidence_coverage=_coverage_label(projected_state, anti_guess.best_evidence_set),
            may_release=approved_releases,
            allowed_category=allowed_category,
            contradictions=(answer_internal.contradictions if answer_internal is not None else []),
        )

        public_trace = _public_trace_before_mentor(
            analysis_contains_answer=analysis.contains_answer_attempt,
            answer_attempt_id=answer_attempt_id,
            answer_public=answer_public,
            compare_answer_ms=compare_answer_ms,
        )
        await _emit_public_trace(on_public_trace, public_trace)

        mentor_started = perf_counter()
        mentor_deps = MentorDeps(
            public_scenario=request.public_scenario,
            transcript=request.transcript,
            learner_state=_learner_view(request, projected_state),
            constraints=constraints,
            released_evidence=_released_evidence(request, projected_state),
            answer_comparison=answer_public,
            guard_only=GuardContext(
                forbidden_entities=_forbidden_entities(request, projected_state),
                completion_allowed=verification.completion_allowed,
                may_release=approved_releases,
            ),
        )
        mentor_result = await run_with_network_retries(
            lambda: self.mentor.run(
                "请基于本轮公开上下文生成导师回复。",
                deps=mentor_deps,
            )
        )
        mentor_ms = _elapsed_ms(mentor_started)
        mentor_action = mentor_result.output
        final_trace = _public_trace_after_mentor(start_sequence=len(public_trace) + 1)
        public_trace.extend(final_trace)
        await _emit_public_trace(on_public_trace, final_trace)

        return AgentTurnResult(
            request_id=request.request_id,
            expected_revision=request.state_revision,
            reply=mentor_action.reply,
            turn_analysis=analysis,
            proposals=_state_proposals(
                request.learner_state,
                projected_state,
                reply=mentor_action.reply,
            ),
            public_trace=public_trace,
            internal_verification=verification,
            internal_audit=AuditTrace(
                reason_codes=_reason_codes(analysis, observations, answer_public),
                mentor_rationale=mentor_action.rationale,
                interpreter_ms=interpreter_ms,
                mentor_ms=mentor_ms,
            ),
        )


def _observe_actions(
    request: AgentTurnRequest,
    *,
    actions: list[str],
    approved_releases: list[str],
) -> list[Observation]:
    approved = set(approved_releases)
    available = set(request.learner_state.collected_evidence)
    observations: list[Observation] = []
    engine = HiddenWorldEngine()
    for action in actions:
        observation = engine.observe(
            request.hidden_world,
            action=action,
            collected_evidence=available,
        )
        allowed_yields = [item for item in observation.yields_evidence if item in approved]
        if observation.yields_evidence and not allowed_yields:
            observation = observation.model_copy(
                update={"yields_evidence": [], "rules_out": []},
                deep=True,
            )
        elif allowed_yields != observation.yields_evidence:
            observation = observation.model_copy(update={"yields_evidence": allowed_yields}, deep=True)
        available.update(allowed_yields)
        observations.append(observation)
    return observations


def _learner_view(request: AgentTurnRequest, state: LearnerState) -> LearnerStateView:
    labels = {item.hypothesis_id: item.label for item in request.hidden_world.hypotheses}
    return LearnerStateView(
        established_facts=list(state.established_facts),
        actions_taken=list(state.actions_taken),
        current_focus=state.current_focus,
        current_hypothesis_label=labels.get(state.current_hypothesis or ""),
        ruled_out_labels=[labels[item] for item in state.ruled_out_hypotheses if item in labels],
        effective_turns=state.effective_turns,
        stalled_turns=state.stalled_turns,
        recent_openings=list(state.recent_openings),
    )


def _released_evidence(request: AgentTurnRequest, state: LearnerState) -> list[str]:
    released = set(state.collected_evidence)
    return [node.content for node in request.hidden_world.evidence_graph if node.evidence_id in released]


def _forbidden_entities(request: AgentTurnRequest, state: LearnerState) -> list[str]:
    return extract_forbidden_entities(
        request.hidden_world,
        released_evidence=state.collected_evidence,
    )


def _first_release_category(request: AgentTurnRequest, releases: list[str]):
    for evidence_id in releases:
        node = request.hidden_world.evidence_by_id(evidence_id)
        if node is not None:
            return node.category
    return None


def _coverage_label(state: LearnerState, best_set: list[str]) -> str:
    if not best_set:
        return "0/0"
    collected = set(state.collected_evidence)
    return f"{len(collected.intersection(best_set))}/{len(best_set)}"


def _state_proposals(before: LearnerState, after: LearnerState, *, reply: str) -> list[Proposal]:
    proposals: list[Proposal] = []
    proposals.extend(
        Proposal(kind="release_evidence", evidence_id=item)
        for item in _new_items(before.collected_evidence, after.collected_evidence)
    )
    proposals.extend(
        Proposal(kind="record_action", action=item)
        for item in _new_items(before.actions_taken, after.actions_taken)
    )
    proposals.extend(
        Proposal(kind="record_established_fact", fact=item)
        for item in _new_items(before.established_facts, after.established_facts)
    )
    proposals.extend(
        Proposal(kind="rule_out_hypothesis", hypothesis_id=item)
        for item in _new_items(before.ruled_out_hypotheses, after.ruled_out_hypotheses)
    )
    if after.current_hypothesis and after.current_hypothesis != before.current_hypothesis:
        proposals.append(Proposal(kind="set_current_hypothesis", hypothesis_id=after.current_hypothesis))
    if after.effective_turns != before.effective_turns:
        proposals.append(
            Proposal(kind="advance_effective_turn", value=after.effective_turns - before.effective_turns)
        )
    if after.stalled_turns != before.stalled_turns:
        proposals.append(Proposal(kind="set_stalled_turns", value=after.stalled_turns))
    opening = reply.strip().splitlines()[0][:80] if reply.strip() else ""
    if opening:
        proposals.append(Proposal(kind="record_opening", text=opening))
    return proposals


def _public_trace_before_mentor(
    *,
    analysis_contains_answer: bool,
    answer_attempt_id: str,
    answer_public: PublicAnswerComparison | None,
    compare_answer_ms: int,
) -> list[PublicTraceEvent]:
    trace = [
        PublicTraceEvent(
            sequence=1,
            kind="reasoning_summary_completed",
            summary="已完成本轮公开意图与动作识别。",
            reasoning=PublicReasoningSummary(
                stage="understanding_message",
                text="已根据你的原话识别本轮希望验证的公开方向。",
            ),
        ),
        PublicTraceEvent(
            sequence=2,
            kind="reasoning_summary_completed",
            summary="已根据公开观察更新本轮排查进展。",
            reasoning=PublicReasoningSummary(
                stage="checking_observations",
                text="已核对本轮动作产生的公开观察，并更新可继续使用的事实。",
            ),
        ),
    ]
    sequence = 3
    if analysis_contains_answer and answer_public is not None:
        tool_payload = ToolEventPayload(
            name="compare_answer",
            redacted_arguments={"answer_attempt_id": answer_attempt_id},
            duration_ms=compare_answer_ms,
            result=answer_public,
        )
        trace.extend(
            [
                PublicTraceEvent(
                    sequence=sequence,
                    kind="tool_started",
                    status="started",
                    summary="正在对比本轮答案表述与已公开证据。",
                    tool=tool_payload.model_copy(update={"result": None, "duration_ms": 0}),
                    tool_name="compare_answer",
                ),
                PublicTraceEvent(
                    sequence=sequence + 1,
                    kind="tool_result",
                    summary="答案对比工具已返回公开辅导信号。",
                    tool=tool_payload,
                    tool_name="compare_answer",
                    duration_ms=compare_answer_ms,
                ),
                PublicTraceEvent(
                    sequence=sequence + 2,
                    kind="tool_completed",
                    summary="答案对比工具调用完成。",
                    tool=tool_payload,
                    tool_name="compare_answer",
                    duration_ms=compare_answer_ms,
                ),
            ]
        )
        sequence += 3
    trace.append(
        PublicTraceEvent(
            sequence=sequence,
            kind="response_summary",
            status="running",
            summary="正在根据公开事实整理导师回复。",
            reasoning=PublicReasoningSummary(
                stage="composing_reply",
                text="正在把已公开观察整理成下一步可执行的引导。",
            ),
        )
    )
    return trace


def _public_trace_after_mentor(*, start_sequence: int) -> list[PublicTraceEvent]:
    return [
        PublicTraceEvent(
            sequence=start_sequence,
            kind="mentor_buffered",
            summary="导师回复已完成私有缓冲。",
        ),
        PublicTraceEvent(
            sequence=start_sequence + 1,
            kind="guard_passed",
            summary="回复已通过安全校验。",
        ),
    ]


async def _emit_public_trace(
    callback: PublicTraceCallback | None,
    events: list[PublicTraceEvent],
) -> None:
    if callback is None:
        return
    for event in events:
        await callback(event)


def _reason_codes(analysis, observations, answer_public) -> list[str]:
    codes: list[str] = []
    if analysis.is_low_confidence():
        codes.append("interpreter_low_confidence")
    if analysis.is_noise:
        codes.append("noise_ignored")
    if any(item.is_negative and item.yields_evidence for item in observations):
        codes.append("negative_observation_progress")
    if answer_public is not None:
        codes.append("answer_compared")
    return codes


def _new_items(before: list[str], after: list[str]) -> list[str]:
    known = set(before)
    return [item for item in after if item not in known]


def _elapsed_ms(started: float) -> int:
    return max(0, int(round((perf_counter() - started) * 1000)))
