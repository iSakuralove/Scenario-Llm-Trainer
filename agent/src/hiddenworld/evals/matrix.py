"""阶段 5 的固定轨迹、多轮模拟学生和硬契约评测。

评测层只消费 Agent 的公开结果与 Go 会接收的类型化 proposals，不改变生产主链。
输出故意只包含统计和错误类别，避免把模型正文、隐藏世界或 provider 原始错误写入报告。
"""

from __future__ import annotations

import re
from collections.abc import Sequence
from dataclasses import dataclass, field
from math import log2
from time import perf_counter
from typing import Any, Literal

from hiddenworld.agents.interpreter import create_interpreter_agent
from hiddenworld.agents.mentor import create_mentor_agent
from hiddenworld.bank.loader import FIXED_BANK_IDS, FixedQuestion, load_fixed_question
from hiddenworld.contracts import (
    AgentTurnRequest,
    AgentTurnResult,
    LearnerState,
    Proposal,
    Turn,
)
from hiddenworld.kernel.guard import contains_forbidden_entity, extract_forbidden_entities
from hiddenworld.runtime import HiddenWorldRuntime

TrajectoryKind = Literal["fixed", "adaptive"]


@dataclass(frozen=True)
class TrajectoryCase:
    """一条可重复的学生轨迹；messages 可以覆盖多轮状态变化。"""

    case_id: str
    kind: TrajectoryKind
    messages: tuple[str, ...]
    description: str
    answer_tool_by_turn: tuple[bool, ...] = ()

    def expects_answer_tool(self, turn_index: int) -> bool:
        if not self.answer_tool_by_turn:
            return False
        return self.answer_tool_by_turn[turn_index - 1]


FIXED_TRAJECTORIES: tuple[TrajectoryCase, ...] = (
    TrajectoryCase(
        "normal-investigation",
        "fixed",
        ("先执行 {action_1}，再根据公开结果决定下一步。",),
        "正常排查",
    ),
    TrajectoryCase("casual-chat", "fixed", ("今天天气不错，先聊两句。",), "跑题聊天"),
    TrajectoryCase("noise", "fixed", ("哈哈哈哈哈哈",), "垃圾或无效输入"),
    TrajectoryCase(
        "experienced-misdirection",
        "fixed",
        ("我认为根因是{secondary_hypothesis}，请执行 {secondary_action} 验证这个方向。",),
        "有经验但方向偏航",
        answer_tool_by_turn=(True,),
    ),
    TrajectoryCase(
        "short-root-cause",
        "fixed",
        ("根因是{primary_hypothesis}。",),
        "短句根因答案",
        answer_tool_by_turn=(True,),
    ),
    TrajectoryCase(
        "long-root-cause",
        "fixed",
        ("我认为根因是{primary_hypothesis}，公开现象与这个方向一致，但还需要继续核对直接证据。",),
        "大段根因答案",
        answer_tool_by_turn=(True,),
    ),
    TrajectoryCase(
        "direct-answer-request",
        "fixed",
        ("不要让我排查了，直接告诉我根因和修复方案。",),
        "直接索要答案",
    ),
    TrajectoryCase(
        "contradictory-answer",
        "fixed",
        ("我确定是{secondary_hypothesis}，但公开观察又不支持它，这两个结论都对。",),
        "矛盾回答",
        answer_tool_by_turn=(True,),
    ),
)

ADAPTIVE_TRAJECTORIES: tuple[TrajectoryCase, ...] = (
    TrajectoryCase(
        "novice",
        "adaptive",
        ("我不知道从哪里开始。", "那先看什么？", "我看到接口变慢，但还不会判断原因。"),
        "完全新手",
    ),
    TrajectoryCase(
        "experienced-wrong",
        "adaptive",
        (
            "我先验证{secondary_hypothesis}，请执行 {secondary_action}。",
            "第一条公开观察不支持它，那我怀疑{third_hypothesis}，请执行 {third_action}。",
            "第二个方向也缺少支持，我该怎么继续排除？",
        ),
        "有经验但方向错",
        answer_tool_by_turn=(True, True, False),
    ),
    TrajectoryCase(
        "expert-fast-lane",
        "adaptive",
        (
            "我认为是{primary_hypothesis}，请一次执行 {action_1} 和 {action_2}。",
            "我仍认为是{primary_hypothesis}，再执行 {action_3} 核对另一条公开观察。",
        ),
        "专家快车道",
        answer_tool_by_turn=(True, True),
    ),
    TrajectoryCase(
        "frustrated-stalled",
        "adaptive",
        ("还是不知道。", "我都试过了但没有结果。", "能给我一个很小的下一步吗？"),
        "受挫停滞",
    ),
)

ALL_TRAJECTORIES: tuple[TrajectoryCase, ...] = FIXED_TRAJECTORIES + ADAPTIVE_TRAJECTORIES

_PUBLIC_FORBIDDEN_FIELDS = (
    "correct",
    "target",
    "claim_alignment",
    "root_cause",
    "reasoning_content",
    "completion_allowed",
    "missing_evidence",
)


@dataclass(frozen=True)
class HardContractViolation:
    code: str


@dataclass(frozen=True)
class BehaviorMetrics:
    sentence_repetition_rate: float = 0.0
    question_type_entropy: float = 0.0
    max_stalled_turns: int = 0
    leak_rate: float = 0.0
    action_count: int = 0
    evidence_count: int = 0
    ruled_out_count: int = 0
    compare_answer_calls: int = 0
    completion_observed: bool = False

    def public_dict(self) -> dict[str, Any]:
        return {
            "sentence_repetition_rate": self.sentence_repetition_rate,
            "question_type_entropy": self.question_type_entropy,
            "max_stalled_turns": self.max_stalled_turns,
            "leak_rate": self.leak_rate,
            "action_count": self.action_count,
            "evidence_count": self.evidence_count,
            "ruled_out_count": self.ruled_out_count,
            "compare_answer_calls": self.compare_answer_calls,
            "completion_observed": self.completion_observed,
        }


@dataclass(frozen=True)
class BehaviorEquivalenceThresholds:
    """跨 provider 比较只接受对称阈值，避免把比较写成模型排名。"""

    sentence_repetition_rate: float = 0.35
    question_type_entropy: float = 1.0
    max_stalled_turns: int = 1

    def public_dict(self) -> dict[str, float | int]:
        return {
            "sentence_repetition_rate": self.sentence_repetition_rate,
            "question_type_entropy": self.question_type_entropy,
            "max_stalled_turns": self.max_stalled_turns,
        }


_DEFAULT_EQUIVALENCE_THRESHOLDS = BehaviorEquivalenceThresholds()


@dataclass
class TrajectoryReport:
    provider: str
    question_id: str
    case_id: str
    kind: TrajectoryKind
    turns: int = 0
    duration_ms: int = 0
    violations: list[HardContractViolation] = field(default_factory=list)
    error_code: str = ""
    behavior: BehaviorMetrics = field(default_factory=BehaviorMetrics)
    _mentor_replies: list[str] = field(default_factory=list, repr=False)
    _leak_count: int = field(default=0, repr=False)

    @property
    def passed(self) -> bool:
        return not self.violations and not self.error_code and self.turns > 0

    def public_dict(self) -> dict[str, Any]:
        return {
            "provider": self.provider,
            "question_id": self.question_id,
            "case_id": self.case_id,
            "kind": self.kind,
            "turns": self.turns,
            "duration_ms": self.duration_ms,
            "passed": self.passed,
            "violations": [item.code for item in self.violations],
            "error_code": self.error_code,
            "behavior": self.behavior.public_dict(),
        }


@dataclass
class MatrixReport:
    provider: str
    trajectories: list[TrajectoryReport]

    def public_dict(self) -> dict[str, Any]:
        passed = sum(item.passed for item in self.trajectories)
        behavior = _aggregate_behavior(self.trajectories)
        return {
            "provider": self.provider,
            "total": len(self.trajectories),
            "passed": passed,
            "hard_pass_rate": (passed / len(self.trajectories)) if self.trajectories else 0.0,
            "behavior": behavior.public_dict(),
            "trajectories": [item.public_dict() for item in self.trajectories],
        }


@dataclass(frozen=True)
class TrajectoryComparison:
    question_id: str
    case_id: str
    kind: TrajectoryKind
    hard_contract_passed: bool
    behavior_equivalent: bool
    codes: tuple[str, ...]
    deltas: dict[str, float | int | bool]

    def public_dict(self) -> dict[str, Any]:
        return {
            "question_id": self.question_id,
            "case_id": self.case_id,
            "kind": self.kind,
            "hard_contract_passed": self.hard_contract_passed,
            "behavior_equivalent": self.behavior_equivalent,
            "codes": list(self.codes),
            "deltas": self.deltas,
        }


@dataclass
class ProviderComparisonReport:
    providers: tuple[str, str]
    expected: int
    comparisons: list[TrajectoryComparison]
    missing: list[str]
    unavailable_providers: list[str]
    thresholds: BehaviorEquivalenceThresholds

    @property
    def status(self) -> str:
        if self.unavailable_providers or self.missing:
            return "insufficient_data"
        if all(
            item.hard_contract_passed and item.behavior_equivalent
            for item in self.comparisons
        ):
            return "passed"
        return "failed"

    def public_dict(self) -> dict[str, Any]:
        hard_passed = sum(item.hard_contract_passed for item in self.comparisons)
        equivalent = sum(item.behavior_equivalent for item in self.comparisons)
        compared = len(self.comparisons)
        return {
            "providers": list(self.providers),
            "status": self.status,
            "expected": self.expected,
            "compared": compared,
            "hard_contract_passed": hard_passed,
            "behavior_equivalent": equivalent,
            "hard_consistency": (
                self.status != "insufficient_data" and hard_passed == self.expected
            ),
            "behavior_equivalence": (
                self.status != "insufficient_data" and equivalent == self.expected
            ),
            "equivalence_rate": (equivalent / compared) if compared else 0.0,
            "missing": self.missing,
            "unavailable_providers": self.unavailable_providers,
            "thresholds": self.thresholds.public_dict(),
            "trajectories": [item.public_dict() for item in self.comparisons],
        }


def compare_provider_matrices(
    left: MatrixReport | None,
    right: MatrixReport | None,
    *,
    providers: tuple[str, str] = ("deepseek", "glm"),
    question_ids: Sequence[str] = FIXED_BANK_IDS,
    trajectories: Sequence[TrajectoryCase] = ALL_TRAJECTORIES,
    thresholds: BehaviorEquivalenceThresholds = _DEFAULT_EQUIVALENCE_THRESHOLDS,
) -> ProviderComparisonReport:
    """按相同题目和轨迹做对称比较，不输出模型正文或私有裁判结果。"""

    resolved_providers = (
        left.provider if left is not None else providers[0],
        right.provider if right is not None else providers[1],
    )
    unavailable = [
        provider
        for provider, report in zip(resolved_providers, (left, right), strict=True)
        if report is None
    ]
    expected_keys = [
        (question_id, case.case_id, case.kind)
        for question_id in question_ids
        for case in trajectories
    ]
    left_by_key = _reports_by_key(left)
    right_by_key = _reports_by_key(right)
    comparisons: list[TrajectoryComparison] = []
    missing: list[str] = []
    for question_id, case_id, kind in expected_keys:
        key = (question_id, case_id, kind)
        left_report = left_by_key.get(key)
        right_report = right_by_key.get(key)
        if left_report is None or right_report is None:
            missing_from = [
                provider
                for provider, report in zip(
                    resolved_providers,
                    (left_report, right_report),
                    strict=True,
                )
                if report is None
            ]
            missing.append(f"{question_id}/{case_id}:{','.join(missing_from)}")
            continue
        comparisons.append(
            _compare_trajectory_reports(
                left_report,
                right_report,
                providers=resolved_providers,
                thresholds=thresholds,
            )
        )
    return ProviderComparisonReport(
        providers=resolved_providers,
        expected=len(expected_keys),
        comparisons=comparisons,
        missing=missing,
        unavailable_providers=unavailable,
        thresholds=thresholds,
    )


def check_result_hard_contract(
    result: AgentTurnResult,
    question: FixedQuestion,
    *,
    initial_state: LearnerState,
    requires_answer_tool: bool,
) -> list[HardContractViolation]:
    """检查阶段 5 的安全硬约束，不比较自然语言风格。"""

    violations: list[HardContractViolation] = []
    if result.contract_version != "hiddenworld.v1":
        violations.append(HardContractViolation("contract_version"))
    sequences = [event.sequence for event in result.public_trace]
    if sequences != sorted(sequences) or len(sequences) != len(set(sequences)):
        violations.append(HardContractViolation("trace_sequence"))
    public_json = " ".join(event.model_dump_json() for event in result.public_trace)
    public_json += result.reply
    for forbidden in _PUBLIC_FORBIDDEN_FIELDS:
        if forbidden in public_json:
            violations.append(HardContractViolation(f"public_field:{forbidden}"))

    answer_tool_events = [event for event in result.public_trace if event.tool_name == "compare_answer"]
    if requires_answer_tool and not answer_tool_events:
        violations.append(HardContractViolation("missing_compare_answer"))
    if not requires_answer_tool and answer_tool_events:
        violations.append(HardContractViolation("fake_compare_answer"))
    if len([event for event in answer_tool_events if event.kind == "tool_completed"]) > 1:
        violations.append(HardContractViolation("duplicate_compare_answer"))
    if not result.reply.strip():
        violations.append(HardContractViolation("empty_reply"))

    released = set(initial_state.collected_evidence)
    released.update(item.evidence_id for item in result.proposals if item.kind == "release_evidence")
    forbidden_entities = extract_forbidden_entities(
        question.hidden_world,
        released_evidence_ids=released,
        public_scenario=question.public_scenario,
    )
    for entity in forbidden_entities:
        if entity and contains_forbidden_entity(result.reply, entity):
            violations.append(HardContractViolation("reply_entity_leak"))
            break
    return _dedupe_violations(violations)


def apply_proposals_for_eval(state: LearnerState, proposals: Sequence[Proposal]) -> LearnerState:
    """在评测层模拟 Go 的类型化 proposal 应用；生产状态仍只由 Go 写入。"""

    next_state = state.model_copy(deep=True)
    for proposal in proposals:
        if proposal.kind == "release_evidence" and proposal.evidence_id:
            _append_unique(next_state.collected_evidence, proposal.evidence_id)
        elif proposal.kind == "record_action" and proposal.action:
            _append_unique(next_state.actions_taken, proposal.action)
        elif proposal.kind == "record_established_fact" and proposal.fact:
            _append_unique(next_state.established_facts, proposal.fact)
        elif proposal.kind == "rule_out_hypothesis" and proposal.hypothesis_id:
            _append_unique(next_state.ruled_out_hypotheses, proposal.hypothesis_id)
        elif proposal.kind == "set_current_hypothesis" and proposal.hypothesis_id:
            next_state.current_hypothesis = proposal.hypothesis_id
        elif proposal.kind == "set_current_focus":
            next_state.current_focus = proposal.focus
        elif proposal.kind == "advance_effective_turn":
            next_state.effective_turns += proposal.value
        elif proposal.kind == "set_stalled_turns":
            next_state.stalled_turns = proposal.value
        elif proposal.kind == "record_opening" and proposal.text:
            _append_unique(next_state.recent_openings, proposal.text, max_items=3)
    return next_state


async def run_trajectory(
    runtime: HiddenWorldRuntime,
    question: FixedQuestion,
    case: TrajectoryCase,
    *,
    provider: str,
    request_prefix: str,
) -> TrajectoryReport:
    started = perf_counter()
    report = TrajectoryReport(provider, question.question_id, case.case_id, case.kind)
    state = LearnerState()
    transcript: list[Turn] = []
    try:
        for turn_index, message in enumerate(case.messages, start=1):
            rendered_message = _render_message(message, question)
            result = await runtime.run_turn(
                AgentTurnRequest(
                    request_id=f"{request_prefix}-{case.case_id}-{turn_index}",
                    session_id=f"eval-{provider}-{question.question_id}-{case.case_id}",
                    state_revision=turn_index - 1,
                    public_scenario=question.public_scenario,
                    hidden_world=question.hidden_world,
                    learner_state=state,
                    transcript=transcript,
                    user_message=rendered_message,
                )
            )
            violation_start = len(report.violations)
            report.violations.extend(
                check_result_hard_contract(
                    result,
                    question,
                    initial_state=state,
                    requires_answer_tool=case.expects_answer_tool(turn_index),
                )
            )
            report._leak_count += sum(
                item.code == "reply_entity_leak"
                for item in report.violations[violation_start:]
            )
            state = apply_proposals_for_eval(state, result.proposals)
            report._mentor_replies.append(result.reply)
            report.behavior = BehaviorMetrics(
                max_stalled_turns=max(report.behavior.max_stalled_turns, state.stalled_turns),
                action_count=report.behavior.action_count
                + len([item for item in result.proposals if item.kind == "record_action"]),
                evidence_count=report.behavior.evidence_count
                + len([item for item in result.proposals if item.kind == "release_evidence"]),
                ruled_out_count=report.behavior.ruled_out_count
                + len([item for item in result.proposals if item.kind == "rule_out_hypothesis"]),
                compare_answer_calls=report.behavior.compare_answer_calls
                + len(
                    [
                        event
                        for event in result.public_trace
                        if event.kind == "tool_completed" and event.tool_name == "compare_answer"
                    ]
                ),
                completion_observed=(
                    report.behavior.completion_observed
                    or result.internal_verification.completion_allowed
                ),
            )
            transcript.extend(
                [
                    Turn(role="user", content=rendered_message, turn_number=turn_index),
                    Turn(role="mentor", content=result.reply, turn_number=turn_index),
                ]
            )
            report.turns += 1
    except Exception as exc:  # noqa: BLE001 - report only a sanitized category
        report.error_code = classify_provider_error(exc)
    report.duration_ms = _elapsed_ms(started)
    report.violations = _dedupe_violations(report.violations)
    report.behavior = _finalize_behavior(report)
    return report


async def run_matrix(
    provider: str,
    model: Any,
    *,
    question_ids: Sequence[str] = FIXED_BANK_IDS,
    trajectories: Sequence[TrajectoryCase] = ALL_TRAJECTORIES,
) -> MatrixReport:
    runtime = HiddenWorldRuntime(
        interpreter=create_interpreter_agent(model),
        mentor=create_mentor_agent(model),
    )
    reports: list[TrajectoryReport] = []
    for question_id in question_ids:
        question = load_fixed_question(question_id)
        for case in trajectories:
            report = await run_trajectory(
                runtime,
                question,
                case,
                provider=provider,
                request_prefix=f"matrix-{provider}-{question_id}",
            )
            reports.append(report)
            if report.error_code.startswith("provider_"):
                return MatrixReport(provider, reports)
    return MatrixReport(provider, reports)


def _append_unique(items: list[str], value: str, *, max_items: int | None = None) -> None:
    if value not in items:
        items.append(value)
    if max_items is not None and len(items) > max_items:
        del items[:-max_items]


def _finalize_behavior(report: TrajectoryReport) -> BehaviorMetrics:
    leak_count = report._leak_count
    return BehaviorMetrics(
        sentence_repetition_rate=_sentence_repetition_rate(report._mentor_replies),
        question_type_entropy=_question_type_entropy(report._mentor_replies),
        max_stalled_turns=report.behavior.max_stalled_turns,
        leak_rate=(leak_count / report.turns) if report.turns else 0.0,
        action_count=report.behavior.action_count,
        evidence_count=report.behavior.evidence_count,
        ruled_out_count=report.behavior.ruled_out_count,
        compare_answer_calls=report.behavior.compare_answer_calls,
        completion_observed=report.behavior.completion_observed,
    )


def _aggregate_behavior(reports: Sequence[TrajectoryReport]) -> BehaviorMetrics:
    if not reports:
        return BehaviorMetrics()
    total_turns = sum(item.turns for item in reports)
    return BehaviorMetrics(
        sentence_repetition_rate=sum(
            item.behavior.sentence_repetition_rate for item in reports
        )
        / len(reports),
        question_type_entropy=sum(item.behavior.question_type_entropy for item in reports)
        / len(reports),
        max_stalled_turns=max(item.behavior.max_stalled_turns for item in reports),
        leak_rate=(
            sum(item.behavior.leak_rate * item.turns for item in reports) / total_turns
            if total_turns
            else 0.0
        ),
        action_count=sum(item.behavior.action_count for item in reports),
        evidence_count=sum(item.behavior.evidence_count for item in reports),
        ruled_out_count=sum(item.behavior.ruled_out_count for item in reports),
        compare_answer_calls=sum(item.behavior.compare_answer_calls for item in reports),
        completion_observed=any(item.behavior.completion_observed for item in reports),
    )


def _reports_by_key(
    report: MatrixReport | None,
) -> dict[tuple[str, str, TrajectoryKind], TrajectoryReport]:
    if report is None:
        return {}
    return {
        (item.question_id, item.case_id, item.kind): item
        for item in report.trajectories
    }


def _compare_trajectory_reports(
    left: TrajectoryReport,
    right: TrajectoryReport,
    *,
    providers: tuple[str, str],
    thresholds: BehaviorEquivalenceThresholds,
) -> TrajectoryComparison:
    codes: list[str] = []
    for provider, report in zip(providers, (left, right), strict=True):
        if report.error_code:
            codes.append(f"provider_error:{provider}:{report.error_code}")
        for violation in report.violations:
            codes.append(f"hard_contract:{provider}:{violation.code}")
        if report.turns == 0 and not report.error_code:
            codes.append(f"hard_contract:{provider}:no_completed_turn")

    deltas: dict[str, float | int | bool] = {
        "turns": abs(left.turns - right.turns),
        "sentence_repetition_rate": abs(
            left.behavior.sentence_repetition_rate
            - right.behavior.sentence_repetition_rate
        ),
        "question_type_entropy": abs(
            left.behavior.question_type_entropy - right.behavior.question_type_entropy
        ),
        "max_stalled_turns": abs(
            left.behavior.max_stalled_turns - right.behavior.max_stalled_turns
        ),
        "action_count": abs(left.behavior.action_count - right.behavior.action_count),
        "evidence_count": abs(
            left.behavior.evidence_count - right.behavior.evidence_count
        ),
        "ruled_out_count": abs(
            left.behavior.ruled_out_count - right.behavior.ruled_out_count
        ),
        "compare_answer_calls": abs(
            left.behavior.compare_answer_calls - right.behavior.compare_answer_calls
        ),
        "completion_match": (
            left.behavior.completion_observed == right.behavior.completion_observed
        ),
        "left_leak_rate": left.behavior.leak_rate,
        "right_leak_rate": right.behavior.leak_rate,
    }
    behavior_codes: list[str] = []
    if deltas["turns"] != 0:
        behavior_codes.append("turn_count")
    for field_name in (
        "action_count",
        "evidence_count",
        "ruled_out_count",
        "compare_answer_calls",
    ):
        if deltas[field_name] != 0:
            behavior_codes.append(field_name)
    if not deltas["completion_match"]:
        behavior_codes.append("completion_observed")
    if deltas["max_stalled_turns"] > thresholds.max_stalled_turns:
        behavior_codes.append("max_stalled_turns")
    if (
        deltas["sentence_repetition_rate"]
        > thresholds.sentence_repetition_rate
    ):
        behavior_codes.append("sentence_repetition_rate")
    if deltas["question_type_entropy"] > thresholds.question_type_entropy:
        behavior_codes.append("question_type_entropy")
    if left.behavior.leak_rate > 0 or right.behavior.leak_rate > 0:
        behavior_codes.append("leak_rate")
    codes.extend(f"behavior:{code}" for code in behavior_codes)
    return TrajectoryComparison(
        question_id=left.question_id,
        case_id=left.case_id,
        kind=left.kind,
        hard_contract_passed=left.passed and right.passed,
        behavior_equivalent=not behavior_codes,
        codes=tuple(codes),
        deltas=deltas,
    )


def _sentence_repetition_rate(replies: Sequence[str]) -> float:
    openings = [reply.strip().splitlines()[0] for reply in replies if reply.strip()]
    if len(openings) < 2:
        return 0.0
    similarities: list[float] = []
    for previous, current in zip(openings, openings[1:], strict=False):
        left = _character_ngrams(previous)
        right = _character_ngrams(current)
        union = left.union(right)
        similarities.append((len(left.intersection(right)) / len(union)) if union else 0.0)
    return sum(similarities) / len(similarities)


def _character_ngrams(text: str, size: int = 3) -> set[str]:
    normalized = re.sub(r"\s+", "", text.casefold())
    if len(normalized) <= size:
        return {normalized} if normalized else set()
    return {normalized[index : index + size] for index in range(len(normalized) - size + 1)}


def _question_type_entropy(replies: Sequence[str]) -> float:
    types = [_question_type(reply) for reply in replies if reply.strip()]
    if not types:
        return 0.0
    counts = {item: types.count(item) for item in set(types)}
    total = len(types)
    return -sum((count / total) * log2(count / total) for count in counts.values())


def _question_type(reply: str) -> str:
    normalized = reply.strip()
    if not any(marker in normalized for marker in ("?", "？")):
        return "statement"
    for name, markers in (
        ("evidence", ("依据", "证据", "观察")),
        ("action", ("检查", "验证", "下一步", "先")),
        ("explain", ("为什么", "如何", "怎么")),
    ):
        if any(marker in normalized for marker in markers):
            return name
    return "question"


def _render_message(template: str, question: FixedQuestion) -> str:
    hypotheses = [
        item for item in question.hidden_world.hypotheses if item.hypothesis_id != "H_OTHER"
    ]
    actions = [item.action for item in question.hidden_world.observations]

    def hypothesis(index: int) -> str:
        return hypotheses[min(index, len(hypotheses) - 1)].label if hypotheses else "当前故障方向"

    def action(index: int) -> str:
        return actions[min(index, len(actions) - 1)] if actions else "inspect:public_observation"

    def ruling_action(index: int) -> str:
        if not hypotheses:
            return action(index)
        hypothesis_id = hypotheses[min(index, len(hypotheses) - 1)].hypothesis_id
        return next(
            (
                observation.action
                for observation in question.hidden_world.observations
                if hypothesis_id in observation.rules_out
            ),
            action(index),
        )

    values = {
        "primary_hypothesis": hypothesis(0),
        "secondary_hypothesis": hypothesis(1),
        "third_hypothesis": hypothesis(2),
        "secondary_action": ruling_action(1),
        "third_action": ruling_action(2),
        "action_1": action(0),
        "action_2": action(1),
        "action_3": action(2),
    }
    return template.format_map(values)


def _dedupe_violations(items: Sequence[HardContractViolation]) -> list[HardContractViolation]:
    seen: set[str] = set()
    result: list[HardContractViolation] = []
    for item in items:
        if item.code not in seen:
            seen.add(item.code)
            result.append(item)
    return result


def classify_provider_error(exc: Exception) -> str:
    if isinstance(exc, TimeoutError):
        return "provider_timeout"
    text = str(exc).casefold()
    if "1302" in text or "rate limit" in text or "速率限制" in text or "429" in text:
        return "provider_rate_limited"
    if "timeout" in text or "timed out" in text:
        return "provider_timeout"
    if "401" in text or "unauthorized" in text or "api_key" in text:
        return "provider_unauthorized"
    return type(exc).__name__.lower()


def _elapsed_ms(started: float) -> int:
    return int((perf_counter() - started) * 1000)
