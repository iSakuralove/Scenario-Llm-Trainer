"""单 Agent 模型输出的严格判别联合。"""

from __future__ import annotations

from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

from .assessment import (
    ConcernLevel,
    HumorLevel,
    IntensityLevel,
    MasterySignal,
    PreferenceSignalKey,
    PreferenceSignalValue,
    TeachingDecision,
    TurnAssessment,
)
from .version import ReplyMode, StudentAffect


class ToolCall(BaseModel):
    model_config = ConfigDict(extra="forbid")

    call_id: str
    tool_id: str
    arguments: dict[str, str] = Field(default_factory=dict)


class AgentSemanticDecision(TurnAssessment):
    """旧字段名兼容层，实际契约已经升级为 TurnAssessment。"""


class ToolCallsOutput(BaseModel):
    model_config = ConfigDict(extra="forbid")

    kind: Literal["tool_calls"]
    public_summary: str = Field(min_length=1)
    calls: list[ToolCall] = Field(min_length=1)
    semantic: AgentSemanticDecision | None = None
    turn_assessment: TurnAssessment | None = None
    teaching_decision: TeachingDecision | None = None


class FinalReplyOutput(BaseModel):
    model_config = ConfigDict(extra="forbid")

    kind: Literal["final_reply"]
    public_summary: str | None = None
    reply: str = Field(min_length=1)
    reply_mode: ReplyMode | None = None
    semantic: AgentSemanticDecision | None = None
    turn_assessment: TurnAssessment | None = None
    teaching_decision: TeachingDecision | None = None


AgentModelOutput = Annotated[ToolCallsOutput | FinalReplyOutput, Field(discriminator="kind")]


class AgentOutputEnvelope(BaseModel):
    """PydanticAI 结构化输出适配层。

    PydanticAI 2.31 的 prompted output 对 Annotated discriminated union 在不同
    provider 上的 text/tool 模式并不一致，因此模型层先接收一个扁平 envelope，
    再由 ``to_contract`` 收敛为严格的 AgentModelOutput。业务代码不能直接使用
    envelope 绕过二选一校验。
    """

    # 兼容不同 GLM/OpenAI 兼容端点偶尔附带的 reasoning/usage 字段；
    # 这些字段会在 envelope 解析时丢弃，最终仍只能通过 to_contract() 进入
    # 严格的 AgentModelOutput，不能扩大业务输出面。
    model_config = ConfigDict(extra="ignore")

    kind: Literal["tool_calls", "final_reply"]
    # reply 放在联合字段前面：纯 final_reply 的 JSON 流可以尽早进入正文，
    # 不必先等待 calls/public_summary 的可选字段全部生成。
    reply: str | None = None
    public_summary: str | None = None
    reply_mode: ReplyMode | None = None
    calls: list[ToolCall] = Field(default_factory=list)
    # 这两份对象是单 Agent 的语义大脑，不允许 provider 只返回 reply 后
    # 由 Runtime 用默认值假装完成意图识别。缺失时让 PydanticAI 触发有界
    # 结构化重试，而不是把空语义继续归约成 chat/normal_diagnosis。
    turn_assessment: TurnAssessment = Field(...)
    teaching_decision: TeachingDecision = Field(...)
    # 保持扁平字段，兼容 DeepSeek/GLM 的共同 JSON 输出能力；
    # 业务层再收敛成 AgentSemanticDecision，不把 CoT 引入传输契约。
    intent: str = "chat"
    user_goal: str = ""
    requested_action: str = ""
    requested_action_raw: str = ""
    clarification_target: str = ""
    action_match_status: str = "none"
    actions: list[str] = Field(default_factory=list)
    hypothesis_id: str = ""
    hypothesis_raw: str = ""
    claim_type: str = "none"
    made_claim: bool = False
    contains_answer_attempt: bool = False
    answer_attempt_text: str = ""
    established_facts: list[str] = Field(default_factory=list)
    progress_assessment: str = "unknown"
    is_stuck: bool = False
    is_off_topic: bool = False
    is_noise: bool = False
    student_affect: StudentAffect = "engaged"
    confidence: float = Field(default=0.0, ge=0.0, le=1.0)
    humor_level: HumorLevel = "none"
    frustration_level: ConcernLevel = "none"
    confusion_level: ConcernLevel = "none"
    confidence_level: IntensityLevel = "low"
    urgency_level: IntensityLevel = "low"
    random_investigation: bool = False
    concept_mastery_signals: dict[str, MasterySignal] = Field(default_factory=dict)
    skill_mastery_signals: dict[str, MasterySignal] = Field(default_factory=dict)
    preference_signals: dict[PreferenceSignalKey, PreferenceSignalValue] = Field(default_factory=dict)

    @model_validator(mode="after")
    def validate_shape(self) -> "AgentOutputEnvelope":
        if self.kind == "tool_calls":
            if not self.public_summary or not self.public_summary.strip() or not self.calls or self.reply is not None:
                raise ValueError("tool_calls requires public_summary and calls, without reply")
        elif self.reply is None or not self.reply.strip() or self.calls:
            raise ValueError("final_reply requires reply and cannot include calls")
        return self

    def to_contract(self) -> AgentModelOutput:
        assessment = self.turn_assessment
        semantic = AgentSemanticDecision.model_validate(assessment.model_dump())
        if self.kind == "tool_calls":
            return ToolCallsOutput(
                kind="tool_calls",
                public_summary=self.public_summary or "",
                calls=self.calls,
                semantic=semantic,
                turn_assessment=assessment,
                teaching_decision=self.teaching_decision,
            )
        return FinalReplyOutput(
            kind="final_reply",
            public_summary=self.public_summary,
            reply=self.reply or "",
            reply_mode=self.reply_mode,
            semantic=semantic,
            turn_assessment=assessment,
            teaching_decision=self.teaching_decision,
        )
