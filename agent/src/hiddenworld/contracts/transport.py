"""Go ↔ Python 传输契约。

Go 保留权威业务状态、revision、request_id 幂等、类型化提议审批、原子持久化、
私有审计和对外 HTTP/SSE 协议。Python 无状态，不直接写业务数据库，只返回
类型化结果和提议。

**proposals 是类型化提议，不是通用 state_patch。** 通用补丁意味着 Python 能
写任意字段，Go 的审批就退化成盖章；类型化提议让 Go 能逐条按当前业务事实核对。
"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from .answer import InternalAnswerComparison
from .agent_context import ActionHistoryEntry, ToolStateView, TurnEnvelope, TurnPhase
from .assessment import GuidanceState, TeachingDecision, TurnAssessment, TurnControl
from .authorization import StructuredUserAction
from .events import PublicTraceEvent
from .learner import LearnerState, Turn
from .turn import TurnAnalysis
from .version import CONTRACT_VERSION, HypothesisRelation
from .world import HiddenWorld, PublicScenario

ProposalKind = Literal[
    "release_evidence",
    # 卡住兜底释放。与 release_evidence 分开是因为审批条件完全不同：
    # 常规释放要求学生点名动作，而卡住的学生恰恰是说不出动作的人。
    # Go 侧对这条走独立分支，用自己持有的 stalled_turns 复核，不信任 is_stuck。
    "release_evidence_on_stall",
    "record_action",
    "record_established_fact",
    "set_current_hypothesis",
    "rule_out_hypothesis",
    "set_current_focus",
    "advance_effective_turn",
    "set_stalled_turns",
    "increment_concept_mastery",
    "increment_skill_mastery",
    "set_explanation_preference",
    "set_hint_level",
    "set_last_hint",
    "record_opening",
]


class ContractVersionMismatch(ValueError):
    """契约版本不匹配。整轮拒绝，不做字段级兼容猜测。"""

    def __init__(self, received: str) -> None:
        super().__init__(f"contract_version mismatch: expected {CONTRACT_VERSION!r}, got {received!r}")
        self.received = received


class Proposal(BaseModel):
    """一条状态变更提议。扁平结构，Go 侧一个 struct 就能接。"""

    model_config = ConfigDict(extra="forbid")

    kind: ProposalKind
    evidence_id: str = ""
    hypothesis_id: str = ""
    fact: str = ""
    action: str = ""
    focus: str = ""
    concept_id: str = ""
    skill_id: str = ""
    preference_key: Literal["", "detail", "analogy", "directness"] = ""
    preference_value: str = ""
    value: int = 0
    text: str = ""


class Budget(BaseModel):
    """单轮预算。deadline 是硬上限，网络重试也受它约束。"""

    model_config = ConfigDict(extra="forbid")

    deadline_ms: int = 15_000
    max_releases: int = Field(default=3, description="本轮最多释放几条证据")


class VerificationResult(BaseModel):
    """内部裁判结果。**仅内部审计，不进入公开 DTO / SSE。**"""

    model_config = ConfigDict(extra="forbid")

    relation: HypothesisRelation
    coverage: float = Field(ge=0.0, le=1.0)
    completion_allowed: bool
    ruled_out_this_turn: list[str] = Field(default_factory=list)
    answer_comparison: InternalAnswerComparison | None = None


class AuditTrace(BaseModel):
    """结构化审计。原因码 + Mentor 的自然语言 rationale。

    **不含 chain-of-thought**：rationale 是 Mentor 在结构化输出里自己写下的
    一句话，不是模型的隐藏推理 token。原始 reasoning_content 从不请求、
    不保存、不传输。
    """

    model_config = ConfigDict(extra="forbid")

    reason_codes: list[str] = Field(default_factory=list)
    mentor_rationale: str = ""
    guard_retries: int = 0
    interpreter_ms: int = 0
    mentor_ms: int = 0
    rules_version: str = CONTRACT_VERSION


class AgentTurnRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    contract_version: str = CONTRACT_VERSION
    request_id: str = Field(description="幂等键。同一个 request_id 重放必须返回同一结果。")
    session_id: str
    state_revision: int
    public_scenario: PublicScenario = Field(description="学生可见题面；供 Interpreter 与 Mentor 使用。")
    hidden_world: HiddenWorld = Field(description="含答案。仅确定性组件消费。")
    learner_state: LearnerState
    conversation_summary: str = ""
    transcript: list[Turn] = Field(default_factory=list)
    user_message: str
    phase: TurnPhase = "new_user_turn"
    turn_id: str = ""
    original_user_message: str = ""
    turn_context: TurnEnvelope | None = None
    action_history: list[ActionHistoryEntry] = Field(default_factory=list)
    tool_states: dict[str, ToolStateView] = Field(default_factory=dict)
    # 上一轮 Go 归约后的安全教学导航；缺失时 Runtime 从 learner_state 构造兼容默认值。
    guidance_state: GuidanceState | None = None
    structured_user_action: StructuredUserAction | None = None
    budget: Budget = Field(default_factory=Budget)

    def require_contract_version(self) -> None:
        if self.contract_version != CONTRACT_VERSION:
            raise ContractVersionMismatch(self.contract_version)


class AgentTurnResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    contract_version: str = CONTRACT_VERSION
    request_id: str
    expected_revision: int = Field(
        description="Python 读到的 state_revision。Go 写入前核对；不一致则整轮丢弃。",
    )
    reply: str = Field(
        default="",
        description=(
            "Mentor 正文。Guard 未通过时为空串——"
            "不能「丢 patch 留 reply」：基于旧状态生成的回复可能请求释放一条已释放的证据，"
            "对话与状态脱节是最难查的一类 bug。"
        ),
    )
    turn_analysis: TurnAnalysis
    turn_assessment: TurnAssessment | None = None
    teaching_decision: TeachingDecision | None = None
    guidance_state: GuidanceState = Field(default_factory=GuidanceState)
    turn_control: TurnControl = Field(default_factory=TurnControl)
    proposals: list[Proposal] = Field(default_factory=list)
    public_trace: list[PublicTraceEvent] = Field(default_factory=list)
    internal_verification: VerificationResult
    internal_audit: AuditTrace
