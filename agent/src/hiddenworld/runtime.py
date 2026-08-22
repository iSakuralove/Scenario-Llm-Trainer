"""HiddenWorld 单轮主链：两个 LLM 调用夹着确定性教学内核。"""

from __future__ import annotations

import asyncio
import logging
import re
import unicodedata
from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from time import perf_counter
from typing import Any

from pydantic_ai import messages as pai_messages

from hiddenworld.action_resolver import legacy_action_aliases, resolve_declared_items
from hiddenworld.agents.tools import CompareAnswerRuntime
from hiddenworld.contracts import (
    AgentTurnRequest,
    AgentTurnResult,
    AuditTrace,
    AuthorizedActionRef,
    EvidenceRequest,
    GuardContext,
    InterpreterDeps,
    LearnerState,
    LearnerStateView,
    MentorAction,
    MentorDeps,
    Observation,
    Proposal,
    PublicAnswerComparison,
    PublicObservation,
    PublicReasoningSummary,
    PublicTraceEvent,
    ToolEventPayload,
    TurnAnalysis,
    UserActionAuthorization,
    VerificationResult,
    validate_scenario_contract,
)
from hiddenworld.kernel import (
    AntiGuess,
    ClueGate,
    EvidenceEngine,
    HiddenWorldEngine,
    RootCauseVerifier,
    TeachingPolicy,
)
from hiddenworld.kernel.guard import Guard, extract_forbidden_entities
from hiddenworld.evidence_availability import resolve_evidence_request
from hiddenworld.retry import run_with_network_retries
from hiddenworld.streaming_json import StreamingFieldExtractor


class TurnDeadlineExceeded(TimeoutError):
    """单轮总 deadline 已耗尽；不得在后台继续执行或重放工具。"""


class PublicBoundaryRejected(RuntimeError):
    """模型多次重写后仍未形成可安全公开的完整回复。"""


PublicTraceCallback = Callable[[PublicTraceEvent], Awaitable[None]]
TurnAnalysisCallback = Callable[[Any], Awaitable[None]]
ReplyDeltaCallback = Callable[[str], Awaitable[None]]

# 旧回放/Go 兼容常量。新 Runtime 已把 Hint 与 collected_evidence 分离，
# 不再据此生成 release_evidence_on_stall；保留值只避免兼容读取发生漂移。
STALL_UNLOCK_THRESHOLD = 2
logger = logging.getLogger("hiddenworld.runtime")


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
        on_reasoning_delta: Callable[[str], Awaitable[None]] | None = None,
    ) -> AgentTurnResult:
        request.require_contract_version()
        # 旧 v1 题目仍允许读取；一旦带有 V2 独立答案字段，加载时执行强校验。
        if request.hidden_world.canonical_answer is not None:
            validate_scenario_contract(request.hidden_world)
        timeout_seconds = max(request.budget.deadline_ms, 1) / 1000
        try:
            return await asyncio.wait_for(
                self._run_turn(
                    request,
                    on_turn_analysis=on_turn_analysis,
                    on_public_trace=on_public_trace,
                    on_reply_delta=on_reply_delta,
                    on_reasoning_delta=on_reasoning_delta,
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
        on_reasoning_delta: Callable[[str], Awaitable[None]] | None,
    ) -> AgentTurnResult:
        interpreter_started = perf_counter()
        interpreter_ms = 0
        sequencer = _StreamSequencer()
        interpreter_deps = InterpreterDeps(
            public_scenario=request.public_scenario,
            hypotheses=request.hidden_world.hypotheses,
            conversation_summary=request.conversation_summary,
            transcript=request.transcript[-8:],
            known_actions=[item.action for item in request.hidden_world.observations],
            virtual_tools=list(request.hidden_world.virtual_tools),
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

        if request.structured_user_action is not None and not request.user_message.strip():
            # QuickAction 点击轮没有自然语言，Interpreter 无从解析；
            # 空 prompt 直发模型会被部分网关拒绝（智谱 1213）。动作本身
            # 就是输入，直接确定性构造分析，不再调用意图模型。
            analysis = _analysis_for_structured_action(request)
        else:
            interpreter_result = await run_with_network_retries(interpreter_call)
            interpreter_ms = _elapsed_ms(interpreter_started)
            analysis = interpreter_result.output
            analysis = _resolve_declared_virtual_tool(request, analysis)
        authorizations = _build_user_action_authorizations(request, analysis)
        if on_turn_analysis is not None:
            await on_turn_analysis(analysis)

        # 低置信 / 噪声时不把动作提交进状态：Go 的 approveScenarioProposals 对
        # release_evidence / record_action / record_established_fact /
        # set_current_hypothesis 全部有 lowConfidence 闸门，这里发出去整轮会被拒。
        # 所以低置信档位不能靠"少提交一点"来放宽；当前消息仍由 Mentor 通过
        # MentorDeps.current_user_message 显式接收，但不会因此获得隐藏状态。
        is_clarification = analysis.intent in {"clarification", "explanation_request"}
        action_match_is_unsafe = analysis.action_match_status in {"unsupported", "ambiguous"}
        actions = [] if analysis.is_low_confidence() or analysis.is_noise or is_clarification or action_match_is_unsafe else analysis.actions
        authorized_action_refs = {item.action_ref for item in authorizations}
        actions = [item for item in actions if item in authorized_action_refs]
        if (
            request.structured_user_action is not None
            and request.structured_user_action.action_id in authorized_action_refs
            and request.structured_user_action.action_id not in actions
        ):
            # QuickAction 点击轮没有自然语言，Interpreter 无从解析出动作；
            # 结构化动作本身就是用户授权，不需要再过一遍意图猜测。
            actions.append(request.structured_user_action.action_id)
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
        # 提示与线索严格分离：卡住只提升 Hint，不再把系统提示伪装成学生取得的证据。
        stall_release = ""
        observations = [] if is_clarification else _observe_actions(
            request,
            actions=actions,
            approved_releases=approved_releases,
            authorizations=authorizations,
        )
        projected_state = (
            request.learner_state.model_copy(deep=True)
            if is_clarification
            else EvidenceEngine().advance(
                request.learner_state,
                analysis=effective_analysis,
                observations=observations,
                valid_hypothesis_ids=request.hidden_world.hypothesis_ids(),
            )
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
            compare_started = perf_counter()
            answer_public = tool_runtime.execute_bound()
            compare_answer_ms = _elapsed_ms(compare_started)
            answer_internal = tool_runtime.internal_result

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
        allowed_category = _first_release_category(request, approved_releases)
        constraints = TeachingPolicy().compile(
            projected_state,
            analysis=effective_analysis,
            completion_allowed=verification.completion_allowed,
            evidence_coverage=_coverage_label(projected_state, anti_guess.best_evidence_set),
            may_release=approved_releases,
            allowed_category=allowed_category,
            contradictions=(answer_internal.contradictions if answer_internal is not None else []),
        )

        public_trace = _public_trace_before_mentor(
            public_summary=analysis.public_summary,
            observations=observations,
            analysis_contains_answer=analysis.contains_answer_attempt,
            answer_attempt_id=answer_attempt_id,
            answer_public=answer_public,
            compare_answer_ms=compare_answer_ms,
        )
        await _emit_public_trace(on_public_trace, public_trace, sequencer)

        mentor_started = perf_counter()
        mentor_deps = MentorDeps(
            public_scenario=request.public_scenario,
            transcript=request.transcript[-8:],
            learner_state=_learner_view(request, projected_state),
            constraints=constraints,
            conversation_summary=request.conversation_summary,
            current_user_message=request.user_message,
            current_intent=analysis.intent,
            requested_action_raw=analysis.requested_action_raw,
            action_match_status=analysis.action_match_status,
            evidence_request=_evidence_request_for_analysis(analysis, request),
            authorized_actions=[AuthorizedActionRef.from_authorization(item) for item in authorizations],
            simulation_tools=_simulation_tool_labels(request),
            released_evidence=_released_evidence_text(request, projected_state),
            answer_comparison=answer_public,
            guard_only=GuardContext(
                forbidden_entities=_forbidden_entities(request, projected_state),
                completion_allowed=verification.completion_allowed,
                may_release=approved_releases,
                evidence_request=_evidence_request_for_analysis(analysis, request),
                current_user_message=request.user_message,
                public_observation_texts=_public_guard_observation_texts(
                    request,
                    projected_state,
                    observations,
                ),
            ),
        )
        mentor_fallback = False
        # ``pydantic-ai`` 的流式 partial output 会在最终 output validator 之前
        # 触发回调。旧双节点链路不能把这段尚未通过 Guard 的预览直接发给浏览器，
        # 否则模型被重试或 fallback 时会留下未获批准的半句回复。
        buffered_reply_deltas: list[str] = []

        async def buffer_reply_delta(piece: str) -> None:
            if piece:
                buffered_reply_deltas.append(piece)

        try:
            mentor_result = await run_with_network_retries(
                lambda: self._run_mentor(
                    mentor_deps,
                    on_reply_delta=buffer_reply_delta if on_reply_delta is not None else None,
                )
            )
            mentor_action = mentor_result.output
        except Exception:
            # Interpreter 和确定性内核已经完成，本轮公开观察也已经准备好；
            # Mentor 上游短暂失败时，不能把整轮已完成的排查一起判成失败。
            # 兜底正文只使用当前已公开上下文，并继续走同一个 Guard。
            mentor_fallback = True
            logger.exception("mentor generation failed; using public-context fallback")
            mentor_action = _fallback_mentor_action(
                request=request,
                analysis=analysis,
                observations=observations,
                constraints=constraints,
                guard_context=mentor_deps.guard_only,
            )
        mentor_ms = _elapsed_ms(mentor_started)
        mentor_action = mentor_action.model_copy(
            update={"reply": _normalize_mentor_reply(request, analysis, observations, mentor_action.reply)},
        )
        try:
            # 模型输出通常已经在 Mentor output validator 中过 Guard；规范化会
            # 改写公开正文，所以这里无论正常还是 fallback 都再走一次最终 Guard。
            mentor_action = Guard().validate(
                mentor_action,
                constraints=constraints,
                context=mentor_deps.guard_only,
            )
        except ValueError:
            if not mentor_fallback:
                mentor_fallback = True
                logger.exception("mentor reply failed final guard; using public-context fallback")
                mentor_action = _fallback_mentor_action(
                    request=request,
                    analysis=analysis,
                    observations=observations,
                    constraints=constraints,
                    guard_context=mentor_deps.guard_only,
                )
                mentor_action = Guard().validate(
                    mentor_action,
                    constraints=constraints,
                    context=mentor_deps.guard_only,
                )
            else:
                raise
        final_trace = _public_trace_after_mentor(start_sequence=len(public_trace) + 1)
        public_trace.extend(final_trace)
        await _emit_public_trace(on_public_trace, final_trace, sequencer)

        if on_reply_delta is not None:
            buffered_reply = "".join(buffered_reply_deltas)
            if buffered_reply == mentor_action.reply and buffered_reply:
                for piece in buffered_reply_deltas:
                    await on_reply_delta(piece)
            else:
                # Guard/规范化或重试改变了正文时，丢弃全部 partial output，
                # 只发最终获批的公开回复一次。
                await on_reply_delta(mentor_action.reply)

        return AgentTurnResult(
            request_id=request.request_id,
            expected_revision=request.state_revision,
            reply=mentor_action.reply,
            # 返回授权过滤后的 actions：Go 侧要求 observation_result 必须源自
            # TurnAnalysis.Actions，原始 analysis 可能带有 Agent 自主提出、
            # 未获用户授权的动作，直接外发会被 Go 整轮拒绝。
            turn_analysis=effective_analysis,
            proposals=_state_proposals(
                request.learner_state,
                projected_state,
                reply=mentor_action.reply,
                stall_release=stall_release,
            ),
            public_trace=public_trace,
            internal_verification=verification,
            internal_audit=AuditTrace(
                reason_codes=(
                    _reason_codes(analysis, observations, answer_public)
                    + (["mentor_fallback"] if mentor_fallback else [])
                ),
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
        try:
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
        except Exception:
            # 某些 OpenAI 兼容端点或测试模型不提供可验证的结构化增量，
            # 流式读取失败时只回退一次非流式终值；正文仍留在 Runtime 私有缓冲，
            # 不把半截候选直接发给学生。
            return await self.mentor.run(
                "请基于本轮公开上下文生成导师回复。",
                deps=deps,
            )


def _observe_actions(
    request: AgentTurnRequest,
    *,
    actions: list[str],
    approved_releases: list[str],
    authorizations: list[UserActionAuthorization] | None = None,
) -> list[Observation]:
    approved = set(approved_releases)
    authorized = {item.action_ref for item in (authorizations or [])}
    available = set(request.learner_state.collected_evidence)
    observations: list[Observation] = []
    engine = HiddenWorldEngine()
    for action in actions:
        if action not in authorized:
            # Agent 自己提出但没有学生授权的观察动作只能被忽略，不能读取新的世界事实。
            continue
        configured = next((item for item in request.hidden_world.observations if item.action == action), None)
        if configured is None:
            continue
        # 已经公开过同一工具的完整结果时，只保留状态上下文，不再重复释放卡片。
        # 若此前只是因为前置不足而得到中性回应，所需证据仍未收集，允许再次查询。
        if action in request.learner_state.actions_taken:
            configured_evidence = set(configured.yields_evidence)
            prerequisites_ready = all(
                set(node.prerequisites).issubset(available)
                for evidence_id in configured_evidence
                if (node := request.hidden_world.evidence_by_id(evidence_id)) is not None
            )
            if (not configured_evidence or configured_evidence.issubset(available)) and prerequisites_ready:
                continue
        observation = engine.observe(
            request.hidden_world,
            action=action,
            collected_evidence=available,
        )
        allowed_yields = [item for item in observation.yields_evidence if item in approved or item in available]
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
        available.update(allowed_yields)
        observations.append(observation)
    return observations


def _analysis_for_structured_action(request: AgentTurnRequest) -> TurnAnalysis:
    """QuickAction 轮的确定性意图分析：动作已知，不需要模型猜测。

    摘要从题目声明的工具目标推导，不引入学生没有表达的新信息；
    confidence 置高保证 Go 侧 low_confidence 闸门放行。
    """

    action_id = request.structured_user_action.action_id if request.structured_user_action else ""
    target = ""
    for tool in request.hidden_world.virtual_tools:
        if tool.observation_action == action_id:
            target = tool.target
            break
    summary = f"你选择查看{target}。" if target else "已收到该项公开检查。"
    return TurnAnalysis(
        public_summary=summary,
        intent="investigate",
        requested_action_raw=request.structured_user_action.normalized_scope if request.structured_user_action else "",
        clarification_target="",
        action_match_status="matched",
        actions=[action_id],
        hypothesis_id="",
        hypothesis_raw="",
        made_claim=False,
        contains_answer_attempt=False,
        answer_attempt_text="",
        established_facts=[],
        is_stuck=False,
        is_noise=False,
        student_affect="engaged",
        confidence=0.95,
    )


def _build_user_action_authorizations(
    request: AgentTurnRequest,
    analysis,
) -> list[UserActionAuthorization]:
    """从真实用户动作签发授权，绝不接受 Agent 自己的 tool_call 作为授权。"""

    authorizations: list[UserActionAuthorization] = []
    if request.structured_user_action is not None:
        action = request.structured_user_action
        if action.state_revision == request.state_revision:
            configured = _observation_for_action(request, action.action_id)
            if configured is not None:
                tool_kind = _tool_kind_for_action(request, action.action_id)
                authorizations.append(
                    UserActionAuthorization(
                        source="structured_user_action",
                        action_ref=action.action_id,
                        tool_kind=tool_kind,
                        normalized_scope=action.normalized_scope,
                        state_revision=request.state_revision,
                        authorization_id=f"{request.request_id}:structured:{action.action_id}",
                    )
                )

    if (
        request.user_message.strip()
        and analysis.intent in {"investigate", "inspect"}
        and analysis.action_match_status in {"matched", "none"}
    ):
        for index, action in enumerate(analysis.actions):
            explicitly_matched = analysis.action_match_status == "matched" or _action_is_explicitly_requested(
                request.user_message, action
            )
            if _observation_for_action(request, action) is not None and explicitly_matched:
                authorizations.append(
                    UserActionAuthorization(
                        source="user_message",
                        action_ref=action,
                        tool_kind=_tool_kind_for_action(request, action),
                        normalized_scope=analysis.requested_action_raw.strip(),
                        state_revision=request.state_revision,
                        authorization_id=f"{request.request_id}:message:{index}",
                    )
                )
    return authorizations


def _tool_kind_for_action(request: AgentTurnRequest, action: str) -> str:
    for item in request.hidden_world.virtual_tools:
        if item.observation_action == action:
            return item.kind
    _, _, remainder = action.partition(":")
    return remainder.split(".", 1)[0] or "observation"


def _observation_for_action(request: AgentTurnRequest, action: str) -> Observation | None:
    for item in request.hidden_world.observations:
        if item.action == action:
            return item
    for item in request.hidden_world.virtual_tools:
        if item.observation_action == action:
            return next(
                (observation for observation in request.hidden_world.observations if observation.action == action),
                None,
            )
    return None


def _action_is_explicitly_requested(user_message: str, action: str) -> bool:
    """旧题目兼容匹配：只用动作标识中的自然词，不让 Agent 自己授予权限。"""

    text = user_message.casefold()
    tokens = [item for item in re.split(r"[:._-]+", action.casefold()) if item and item not in {"inspect"}]
    return bool(tokens) and any(token in text for token in tokens)


def _resolve_declared_virtual_tool(request: AgentTurnRequest, analysis):
    """把自然语言和只读查询绑定到题目声明的唯一虚拟工具。

    LLM 仍负责意图、假设和教学状态判断；这里负责安全边界内的确定性映射，
    避免模型把“数据库日志”误改写成相近的网关动作。
    """

    tools = request.hidden_world.virtual_tools
    if not tools or analysis.intent in {"clarification", "explanation_request", "help_request", "chat"}:
        return analysis
    text = request.user_message.strip().lower()
    if not text:
        return analysis

    candidates = [
        tool
        for tool in tools
        if tool.observation_action in resolve_declared_items(
            request.user_message,
            [tool.model_copy(update={"aliases": [*tool.aliases, *_legacy_virtual_tool_aliases(tool)]})],
            action_attr="observation_action",
        )
    ]

    # 对 SELECT/SHOW/EXPLAIN 等只读语句，优先按题目声明的 query_patterns 匹配；
    # 没有匹配时不执行、不猜测，继续走 unsupported 分支。
    readonly = bool(re.match(r"^(select|show|describe|desc|explain|with)\b", text, re.I))
    external_command = bool(re.match(r"^(curl|wget|ssh|kubectl|docker|bash|sh|cat|tail|grep|psql|mysql)\b", text, re.I))
    if readonly and candidates:
        pass
    elif readonly and not candidates:
        candidates = [
            tool
            for tool in tools
            if tool.observation_action in resolve_declared_items(
                request.user_message,
                [tool],
                action_attr="observation_action",
            )
        ]

    if (readonly or external_command) and not candidates:
        return analysis.model_copy(
            update={
                "actions": [],
                "requested_action_raw": request.user_message.strip(),
                "action_match_status": "unsupported",
                "intent": "investigate",
            }
        )

    unique_actions = {tool.observation_action for tool in candidates}
    if len(unique_actions) != 1:
        return analysis
    action = next(iter(unique_actions))
    return analysis.model_copy(
        update={
            "actions": [action],
            "requested_action_raw": request.user_message.strip(),
            "action_match_status": "matched",
            "intent": "investigate",
        }
    )


def _legacy_virtual_tool_aliases(tool) -> tuple[str, ...]:
    """为已创建的旧会话补充已批准的明确别名，不做模糊关键词猜测。

    场景会把题目快照复制进 session；题库 JSON 更新后，旧 session 不会自动重写。
    这里仅按稳定 observation_action 提供向后兼容短语，避免用户必须重开会话，
    同时不把任意包含“日志/数据库”的消息放行。
    """

    return legacy_action_aliases(tool.observation_action)


def _simulation_tool_labels(request: AgentTurnRequest) -> list[str]:
    if request.hidden_world.virtual_tools:
        return [f"教学模拟 {item.kind}：{item.target}" for item in request.hidden_world.virtual_tools]
    return [f"教学模拟观察：{item.action}" for item in request.hidden_world.observations]


def _normalize_mentor_reply(
    request: AgentTurnRequest,
    analysis,
    observations: list[Observation],
    reply: str,
) -> str:
    """只做空白归一化；不把模型正文替换成固定回复。"""

    return (reply or "").strip()


def _public_observation_summary(observations: list[Observation]) -> str:
    """只复述本轮已经公开的观察，不替学生给出判断或下一步。"""

    results = [item.result.strip() for item in observations if item.result.strip()]
    return "；".join(results)


def _fallback_mentor_action(
    *,
    request: AgentTurnRequest,
    analysis: TurnAnalysis,
    observations: list[Observation],
    constraints,
    guard_context: GuardContext,
) -> MentorAction:
    """模型失败时的最小公开回复；仍需经过同一 Guard，不执行旁路。"""

    if observations:
        subject = "这些观察" if len(observations) > 1 else "这条观察"
        reply = f"{subject}能支撑局部判断，但还不足以连接完整因果链。"
    else:
        candidate = analysis.public_summary.strip()
        normalized_candidate = " ".join(unicodedata.normalize("NFKC", candidate).split()).casefold()
        normalized_user_message = " ".join(
            unicodedata.normalize("NFKC", request.user_message).split()
        ).casefold()
        if candidate and normalized_candidate != normalized_user_message:
            reply = candidate
        else:
            # 兜底正文不能回显学生问题，也不能借机暴露题面之外的结论或下一步。
            reply = "本轮没有新增公开观察。"
    return MentorAction(
        rationale="deterministic_fallback",
        reply=reply,
        requested_releases=[],
        confirms_hypothesis=False,
        expected_effort="quick",
    )


def _evidence_request_for_analysis(
    analysis: TurnAnalysis,
    request: AgentTurnRequest | None = None,
) -> EvidenceRequest | None:
    requested = analysis.requested_action_raw.strip()
    if not requested:
        return None
    if analysis.action_match_status in {"unsupported", "ambiguous"}:
        availability = "UNAVAILABLE"
    elif analysis.actions:
        availability = "SIMULATED_ALLOWED"
    else:
        availability = "DERIVABLE"
    if request is not None:
        return resolve_evidence_request(
            request,
            requested,
            fallback_availability=availability,
        )
    return EvidenceRequest(requested_text=requested, availability=availability)


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
        concept_mastery={
            key: value
            for key, value in state.concept_mastery.items()
            if key in {item.concept_id for item in request.hidden_world.teaching_model.concepts}
        },
        skill_mastery={
            key: value
            for key, value in state.skill_mastery.items()
            if key in {"log_reading", "causal_reasoning", "cross_layer_debugging"}
        },
        explanation_preferences=state.explanation_preferences.model_copy(deep=True),
        hint_level=state.hint_level,
        last_hint=state.last_hint,
        repair_status=state.repair_status,
        recent_openings=list(state.recent_openings),
    )


def _released_evidence_text(request: AgentTurnRequest, state: LearnerState) -> list[str]:
    released = set(state.collected_evidence)
    return [node.content for node in request.hidden_world.evidence_graph if node.evidence_id in released]


def _public_guard_observation_texts(
    request: AgentTurnRequest,
    state: LearnerState,
    observations: list[Observation],
) -> list[str]:
    """汇总会话已公开事实，供 Guard 区分合法引用与隐藏实体。"""

    return list(
        dict.fromkeys(
            [
                *_released_evidence_text(request, state),
                *(item.result for item in observations if item.result.strip()),
            ]
        )
    )


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
    for concept_id, score in after.concept_mastery.items():
        increment = score - before.concept_mastery.get(concept_id, 0)
        if increment > 0:
            proposals.append(
                Proposal(
                    kind="increment_concept_mastery",
                    concept_id=concept_id,
                    value=min(1, increment),
                )
            )
    for skill_id, score in after.skill_mastery.items():
        increment = score - before.skill_mastery.get(skill_id, 0)
        if increment > 0:
            proposals.append(
                Proposal(
                    kind="increment_skill_mastery",
                    skill_id=skill_id,
                    value=min(1, increment),
                )
            )
    before_preferences = before.explanation_preferences.model_dump()
    after_preferences = after.explanation_preferences.model_dump()
    for key in ("detail", "analogy", "directness"):
        if after_preferences[key] != before_preferences[key]:
            proposals.append(
                Proposal(
                    kind="set_explanation_preference",
                    preference_key=key,
                    preference_value=after_preferences[key],
                )
            )
    if after.effective_turns != before.effective_turns:
        proposals.append(
            Proposal(kind="advance_effective_turn", value=after.effective_turns - before.effective_turns)
        )
    if after.stalled_turns != before.stalled_turns:
        proposals.append(Proposal(kind="set_stalled_turns", value=after.stalled_turns))
    if after.current_focus != before.current_focus:
        proposals.append(Proposal(kind="set_current_focus", focus=after.current_focus))
    if after.hint_level != before.hint_level:
        proposals.append(Proposal(kind="set_hint_level", value=after.hint_level))
    if after.last_hint != before.last_hint:
        proposals.append(Proposal(kind="set_last_hint", text=after.last_hint))
    opening = reply.strip().splitlines()[0][:80] if reply.strip() else ""
    if opening:
        proposals.append(Proposal(kind="record_opening", text=opening))
    return proposals


def _public_trace_before_mentor(
    *,
    public_summary: str,
    observations: list[Observation],
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
    for observation in observations:
        trace.append(
            PublicTraceEvent(
                sequence=sequence,
                kind="observation_result",
                observation=PublicObservation(
                    action=observation.action,
                    result=observation.result,
                    is_negative=observation.is_negative,
                ),
            )
        )
        sequence += 1
    if analysis_contains_answer and answer_public is not None:
        tool_payload = ToolEventPayload(
            name="compare_answer",
            redacted_arguments={},
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
