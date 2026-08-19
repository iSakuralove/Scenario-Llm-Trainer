"""阶段 5 的固定轨迹、多轮模拟学生和硬契约评测。

评测层只消费 Agent 的公开结果与 Go 会接收的类型化 proposals，不改变生产主链。
输出故意只包含统计和错误类别，避免把模型正文、隐藏世界或 provider 原始错误写入报告。
"""

from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass, field
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
        }


@dataclass
class MatrixReport:
    provider: str
    trajectories: list[TrajectoryReport]

    def public_dict(self) -> dict[str, Any]:
        passed = sum(item.passed for item in self.trajectories)
        return {
            "provider": self.provider,
            "total": len(self.trajectories),
            "passed": passed,
            "hard_pass_rate": (passed / len(self.trajectories)) if self.trajectories else 0.0,
            "trajectories": [item.public_dict() for item in self.trajectories],
        }


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
            report.violations.extend(
                check_result_hard_contract(
                    result,
                    question,
                    initial_state=state,
                    requires_answer_tool=case.expects_answer_tool(turn_index),
                )
            )
            state = apply_proposals_for_eval(state, result.proposals)
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
