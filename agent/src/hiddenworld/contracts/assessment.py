"""单 Agent 的语义评估、教学决策与回合控制契约。

这些类型把 #1/#6/#16 约定的职责显式化：ScenarioAgent 负责理解与教学决策，
Runtime 负责校验、执行和状态归约。旧的扁平字段仍保留在兼容模型中，但新的
回合结果可以逐步迁移到这些结构化对象，避免再由 Runtime 解析中文回复猜意图。
"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from .dimensions import TeachingDimensionRef
from .version import StudentAffect


TurnIntent = Literal[
    "answer",
    "probe_plan",
    "hypothesis",
    "request_hint",
    "direct_answer_request",
    "chat",
    "off_topic",
    "garbage",
    "stuck",
    "contradiction",
    "meta",
    "investigate",
    "clarification",
    "explanation_request",
    "answer_attempt",
    "help_request",
]

InvestigationImpact = Literal[
    "progress",
    "partial",
    "no_progress",
    "unsupported",
    "contradictory",
    "leak_risk",
    "unknown",
]

ClaimType = Literal[
    "none",
    "observation",
    "hypothesis",
    "answer",
    "question",
    "meta",
]

TeachingState = Literal[
    "guided_inquiry",
    "unsupported_hypothesis",
    "anti_guess_detected",
    "premature_conclusion",
    "conclusion_grilling",
    "evidence_reconstruction",
    "normal_diagnosis",
    "debrief",
    "casual_chat",
    "clarification",
    "off_topic",
    "garbage",
]

TeachingStrategy = Literal[
    "observe",
    "acknowledge",
    "reflect",
    "clarify",
    "debrief",
    "chat",
    "recover",
    "silence",
]

ReplyPolicy = Literal[
    "neutral_summary",
    "tool_result_only",
    "acknowledgement",
    "reflective_question",
    "casual_reply",
    "no_reply",
]


class TurnAssessment(BaseModel):
    """ScenarioAgent 对本轮用户行为的结构化理解。

    该对象只描述用户输入与公开上下文，不携带标准答案、内部比较或隐藏证据。
    """

    model_config = ConfigDict(extra="forbid")

    intent: TurnIntent = "chat"
    user_goal: str = ""
    requested_action: str = ""
    requested_action_raw: str = ""
    clarification_target: str = ""
    action_match_status: str = "none"
    actions: list[str] = Field(default_factory=list)
    hypothesis_id: str = ""
    hypothesis_raw: str = ""
    claim_type: ClaimType = "none"
    made_claim: bool = False
    contains_answer_attempt: bool = False
    answer_attempt_text: str = ""
    established_facts: list[str] = Field(default_factory=list)
    progress_assessment: InvestigationImpact = "unknown"
    is_stuck: bool = False
    is_off_topic: bool = False
    is_noise: bool = False
    student_affect: StudentAffect = "engaged"
    confidence: float = Field(default=0.0, ge=0.0, le=1.0)


class TeachingDecision(BaseModel):
    """ScenarioAgent 的教学策略裁决。

    ``allow_explicit_next_step`` 与 ``allow_ruled_out_scope`` 固定为 False，
    把当前产品约束编码成契约，而不是只写在 prompt 里。
    """

    model_config = ConfigDict(extra="forbid")

    teaching_state: TeachingState = "normal_diagnosis"
    strategy: TeachingStrategy = "acknowledge"
    guidance_direction: str = ""
    reply_policy: ReplyPolicy = "acknowledgement"
    allow_explicit_next_step: Literal[False] = False
    allow_ruled_out_scope: Literal[False] = False


class GuidanceState(BaseModel):
    """Runtime 归约后的安全教学导航切片。"""

    model_config = ConfigDict(extra="forbid")

    teaching_state: TeachingState = "normal_diagnosis"
    progress_assessment: InvestigationImpact = "unknown"
    navigation: list[TeachingDimensionRef] = Field(default_factory=list)
    stalled_turns: int = Field(default=0, ge=0)
    current_focus: str = ""


class TurnControl(BaseModel):
    """Runtime 私有回合控制；Agent 只读看到 terminal。"""

    model_config = ConfigDict(extra="forbid")

    terminal: bool = False
    completion_allowed: bool = False
    completion_ready: bool = False
    allowed_action_ids: list[str] = Field(default_factory=list)
