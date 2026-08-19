"""HiddenWorld 单轮主链：两个 LLM 调用夹着确定性教学内核。"""

from __future__ import annotations

import asyncio
from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from time import perf_counter
from typing import Any

from pydantic_ai import messages as pai_messages

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
from hiddenworld.streaming_json import StreamingFieldExtractor


class TurnDeadlineExceeded(TimeoutError):
    """单轮总 deadline 已耗尽；不得在后台继续执行或重放工具。"""


PublicTraceCallback = Callable[[PublicTraceEvent], Awaitable[None]]
TurnAnalysisCallback = Callable[[Any], Awaitable[None]]
ReplyDeltaCallback = Callable[[str], Awaitable[None]]

# 卡住兜底释放的阈值。Go 侧 scenarioStallUnlockThreshold 必须与此保持一致——
# Python 提前判断只是为了不发出注定被拒的提议，真正的权威复核在 Go。
STALL_UNLOCK_THRESHOLD = 2


class _StreamSequencer:
    """外发事件的单调序号。

    与落库 ``public_trace`` 的序号**刻意分开**：推理增量只走实时通道、不落库
    （一条摘要能拆出几十个增量，会撞穿 Go 侧 64 条 public trace 上限），
    所以两边的条数不同，不能共用一套编号。Go 两侧都会用自己的计数器重新编号，
    这里只需保证各自严格递增。
    """

    def __init__(self) -> None:
        self._value = 0

    def next(self) -> int:
        self._value += 1
        return self._value


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
        on_reply_delta: ReplyDeltaCallback | None = None,
    ) -> AgentTurnResult:
        request.require_contract_version()
        timeout_seconds = max(request.budget.deadline_ms, 1) / 1000
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

    async def _run_turn(
        self,
        request: AgentTurnRequest,
        *,
        on_turn_analysis: TurnAnalysisCallback | None,
        on_public_trace: PublicTraceCallback | None,
        on_reply_delta: ReplyDeltaCallback | None,
    ) -> AgentTurnResult:
        interpreter_started = perf_counter()
        sequencer = _StreamSequencer()
        interpreter_deps = InterpreterDeps(
            public_scenario=request.public_scenario,
            hypotheses=request.hidden_world.hypotheses,
            transcript=request.transcript,
            known_actions=[item.action for item in request.hidden_world.observations],
        )

        async def emit_reasoning_delta(piece: str) -> None:
            if on_public_trace is None:
                return
            await on_public_trace(
                PublicTraceEvent(
                    sequence=sequencer.next(),
                    kind="reasoning_summary_delta",
                    status="running",
                    reasoning=PublicReasoningSummary(
                        stage="understanding_message",
                        text=piece,
                    ),
                )
            )

        # 只有首次尝试挂 callback。重试轮会从头重新生成 public_summary，
        # 增量再发一遍会让学生看到同一句话打两次。
        first_attempt = True

        def interpreter_call():
            nonlocal first_attempt
            callback = emit_reasoning_delta if (first_attempt and on_public_trace is not None) else None
            first_attempt = False
            return self._run_interpreter(
                request.user_message,
                interpreter_deps,
                on_reasoning_delta=callback,
            )

        interpreter_result = await run_with_network_retries(interpreter_call)
        interpreter_ms = _elapsed_ms(interpreter_started)
        analysis = interpreter_result.output
        if on_turn_analysis is not None:
            await on_turn_analysis(analysis)

        # 低置信 / 噪声时不把动作提交进状态：Go 的 approveScenarioProposals 对
        # release_evidence / record_action / record_established_fact /
        # set_current_hypothesis 全部有 lowConfidence 闸门，这里发出去整轮会被拒。
        # 所以低置信档位不能靠"少提交一点"来放宽——唯一的安全出口是把未提交的
        # 意图另外告诉 Mentor，而 Mentor 本来就能从 transcript 读到学生原话，
        # 再加一个字段只会拓宽 MentorDeps 这条显式守卫的安全边界，不划算。
        actions = [] if analysis.is_low_confidence() or analysis.is_noise else analysis.actions
        approved_releases = ClueGate().approve(
            request.hidden_world,
            actions=actions,
            collected_evidence=request.learner_state.collected_evidence,
            max_releases=request.budget.max_releases,
        )
        # 卡住兜底：说不出动作的学生走不到上面那条路，越求助拿到的越少。
        stall_release = ""
        if (
            not approved_releases
            and analysis.is_stuck
            and request.learner_state.stalled_turns >= STALL_UNLOCK_THRESHOLD
        ):
            stall_release = ClueGate().approve_on_stall(
                request.hidden_world,
                collected_evidence=request.learner_state.collected_evidence,
            )
            if stall_release:
                approved_releases = [stall_release]
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
        if stall_release:
            # 刻意放在 advance 之后：兜底释放是系统给的，不是学生挣来的，
            # 不能重置 stalled_turns，也不能推进 effective_turns。
            projected_state = projected_state.model_copy(deep=True)
            projected_state.collected_evidence.append(stall_release)

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
            public_summary=analysis.public_summary,
            analysis_contains_answer=analysis.contains_answer_attempt,
            answer_attempt_id=answer_attempt_id,
            answer_public=answer_public,
            compare_answer_ms=compare_answer_ms,
        )
        await _emit_public_trace(on_public_trace, public_trace, sequencer)

        mentor_started = perf_counter()
        mentor_deps = MentorDeps(
            public_scenario=request.public_scenario,
            transcript=request.transcript,
            learner_state=_learner_view(request, projected_state),
            constraints=constraints,
            released_evidence=_released_evidence_text(request, projected_state),
            answer_comparison=answer_public,
            guard_only=GuardContext(
                forbidden_entities=_forbidden_entities(request, projected_state),
                completion_allowed=verification.completion_allowed,
                may_release=approved_releases,
            ),
        )
        mentor_result = await run_with_network_retries(
            lambda: self._run_mentor(mentor_deps, on_reply_delta=on_reply_delta)
        )
        mentor_ms = _elapsed_ms(mentor_started)
        mentor_action = mentor_result.output
        final_trace = _public_trace_after_mentor(start_sequence=len(public_trace) + 1)
        public_trace.extend(final_trace)
        await _emit_public_trace(on_public_trace, final_trace, sequencer)

        return AgentTurnResult(
            request_id=request.request_id,
            expected_revision=request.state_revision,
            reply=mentor_action.reply,
            turn_analysis=analysis,
            proposals=_state_proposals(
                request.learner_state,
                projected_state,
                reply=mentor_action.reply,
                stall_release=stall_release,
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

    @staticmethod
    def _is_model_node(node) -> bool:
        from pydantic_ai.agent import Agent

        return Agent.is_model_request_node(node)

    async def _run_interpreter(self, message: str, deps: InterpreterDeps, *, on_reasoning_delta):
        """流式跑 interpreter，实时提取 public_summary 增量。

        public_summary 是 TurnAnalysis 的第一个字段，所以它的增量会最先到达——
        学生在其余判断（actions / hypothesis / confidence）还没生成时就能看到
        "我读到了什么"，而不是盯着一段固定的假动画。

        与 ``_run_mentor`` 同理：只读流式文本，不改 output schema 语义，
        Pydantic 校验仍发生在最终 output 上。
        """
        if on_reasoning_delta is None:
            return await self.interpreter.run(message, deps=deps)

        extractor = StreamingFieldExtractor("public_summary")
        async with self.interpreter.iter(message, deps=deps) as run:
            async for node in run:
                if self._is_model_node(node):
                    async with node.stream(run.ctx) as stream:
                        async for event in stream:
                            if not isinstance(event, pai_messages.PartDeltaEvent):
                                continue
                            delta = event.delta
                            if not isinstance(delta, pai_messages.TextPartDelta):
                                continue
                            piece = extractor.feed(delta.content_delta)
                            if piece:
                                await on_reasoning_delta(piece)
            return run.result

    async def _run_mentor(self, deps: MentorDeps, *, on_reply_delta: ReplyDeltaCallback | None):
        """运行 mentor：用 agent.iter() 拿 token 级事件流，实时提取 reply 字段增量。

        只读流式文本，不改变 output schema 与 output_validator 语义——
        Guard 校验仍发生在最终 output 上（pydantic-ai 在流结束后验证），
        流出的 delta 只是"预览"；若校验触发重试，重试轮的 delta 会继续流出，
        前端以最终 result.reply 为准做一次性对齐。
        """
        if on_reply_delta is None:
            return await self.mentor.run(
                "请基于本轮公开上下文生成导师回复。",
                deps=deps,
            )

        extractor = StreamingFieldExtractor("reply")
        async with self.mentor.iter(
            "请基于本轮公开上下文生成导师回复。",
            deps=deps,
        ) as run:
            async for node in run:
                if self._is_model_node(node):
                    async with node.stream(run.ctx) as stream:
                        async for event in stream:
                            if not isinstance(event, pai_messages.PartDeltaEvent):
                                continue
                            delta = event.delta
                            if not isinstance(delta, pai_messages.TextPartDelta):
                                continue
                            piece = extractor.feed(delta.content_delta)
                            if piece:
                                await on_reply_delta(piece)
            return run.result


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


def _released_evidence_text(request: AgentTurnRequest, state: LearnerState) -> list[str]:
    released = set(state.collected_evidence)
    return [node.content for node in request.hidden_world.evidence_graph if node.evidence_id in released]


def _forbidden_entities(request: AgentTurnRequest, state: LearnerState) -> list[str]:
    return extract_forbidden_entities(
        request.hidden_world,
        released_evidence_ids=state.collected_evidence,
        public_scenario=request.public_scenario,
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


def _state_proposals(
    before: LearnerState,
    after: LearnerState,
    *,
    reply: str,
    stall_release: str = "",
) -> list[Proposal]:
    proposals: list[Proposal] = []
    for item in _new_items(before.collected_evidence, after.collected_evidence):
        # 兜底释放走独立 kind：常规 release_evidence 在 Go 侧要求学生点名动作，
        # 而卡住的学生没有动作，用同一个 kind 会被 evidence_not_requested 拒掉。
        kind = "release_evidence_on_stall" if item == stall_release else "release_evidence"
        proposals.append(Proposal(kind=kind, evidence_id=item))
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
    public_summary: str,
    analysis_contains_answer: bool,
    answer_attempt_id: str,
    answer_public: PublicAnswerComparison | None,
    compare_answer_ms: int,
) -> list[PublicTraceEvent]:
    """落库的 mentor 前 trace。

    这里**不再有面向学生的硬编码文案**。以前的两条 reasoning_summary_completed
    是字面常量，不读 analysis，每一轮一字不差——看起来像 agent 在死循环，其实
    是一段固定动画。现在唯一的推理摘要来自模型本轮真实产出的 public_summary。
    """
    summary_text = public_summary.strip()
    trace: list[PublicTraceEvent] = []
    sequence = 1
    if summary_text:
        trace.append(
            PublicTraceEvent(
                sequence=sequence,
                kind="reasoning_summary_completed",
                reasoning=PublicReasoningSummary(
                    stage="understanding_message",
                    text=summary_text,
                ),
            )
        )
        sequence += 1
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
    sequencer: _StreamSequencer,
) -> None:
    """按实时通道的独立序号外发。

    落库序号从 1 重新排，实时序号要接在推理增量后面继续递增——Go 的流式校验
    要求严格递增，共用落库序号会在增量之后回退。
    """
    if callback is None:
        return
    for event in events:
        await callback(event.model_copy(update={"sequence": sequencer.next()}))


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
