"""公开运行事件：前端能看到的全部内容。

每轮对话是一条单列文字事件流，顺序固定：

    user_message
      → reasoning_summary_delta / reasoning_summary_completed
      → [仅答案尝试轮] tool_started(compare_answer)
      → [仅真实工具调用] tool_result / tool_completed
      → response_summary / mentor_buffered
      → guard_passed + proposal_approved
      → reply_delta...
      → turn_completed

方括号部分**只在发生真实工具调用时出现**。普通对话轮不得为了视觉完整而伪造
工具步骤，也不得用固定延时假装在思考。

关于推理摘要：用户看到的是可读的 PublicReasoningSummary，**不是模型的原始
chain-of-thought**。摘要说明系统识别了什么公开意图、为什么调用工具、公开结果
如何影响下一步；信息源严格限制为用户原文、已公开事实、公开工具结果和结构化
阶段码，并且同样要过 Guard。原始 reasoning_content、隐藏 token 和内部 rationale
不请求、不保存、不传输、不展示。

Mentor 正文走 holdback：先在 Python 私有缓冲完整生成，通过 Guard 且 Go 审批
成功后，才拆成 reply_delta 发出。Guard 失败、revision 冲突或审批失败时，
前端一个字都不会看到——已经发出去的正文是收不回来的。
"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from .answer import PublicAnswerComparison

RunEventKind = Literal[
    "user_message",
    "reasoning_summary_delta",
    "reasoning_summary_completed",
    "observation_result",
    "tool_started",
    "tool_result",
    "tool_completed",
    "response_summary",
    "mentor_buffered",
    "guard_passed",
    "proposal_approved",
    "reply_delta",
    "turn_completed",
    "turn_failed",
]

RunEventStatus = Literal["started", "running", "completed", "failed"]

# 结构化阶段码。前端据此显示"正在理解 / 正在校验 / 正在整理回复"，
# 而不是显示模型在想什么。
ReasoningStage = Literal[
    "understanding_message",
    "checking_observations",
    "verifying_answer",
    "composing_reply",
]


class ToolEventPayload(BaseModel):
    """工具行的展开内容。参数和结果都必须是脱敏后的版本。"""

    model_config = ConfigDict(extra="forbid")

    name: str
    redacted_arguments: dict[str, str] = Field(
        default_factory=dict,
        description="脱敏参数。compare_answer 只展示 answer_attempt_id，不展示答案正文。",
    )
    duration_ms: int = 0
    result: PublicAnswerComparison | None = Field(
        default=None,
        description="公开投影。绝不是 InternalAnswerComparison。",
    )


class PublicReasoningSummary(BaseModel):
    """可读的推理摘要。**不是 chain-of-thought。**"""

    model_config = ConfigDict(extra="forbid")

    stage: ReasoningStage
    text: str = Field(description="只引用用户原文、已公开事实和公开工具结果")


class PublicObservation(BaseModel):
    """已经通过世界查询并允许展示给学生的观察结果。"""

    model_config = ConfigDict(extra="forbid")

    action: str
    result: str
    is_negative: bool


class RunEvent(BaseModel):
    """前端 RunEvent[] 的元素。按 sequence 追加，支持断线重连去重。"""

    model_config = ConfigDict(extra="forbid")

    request_id: str = Field(description="幂等键。断线重连按它 + sequence 恢复。")
    sequence: int = Field(description="单调递增。前端据此排序与去重，不依赖到达顺序。")
    kind: RunEventKind
    status: RunEventStatus = "completed"
    text: str = Field(default="", description="user_message / reply_delta 的文本片段")
    reasoning: PublicReasoningSummary | None = None
    observation: PublicObservation | None = None
    tool: ToolEventPayload | None = None
    error_code: str = Field(
        default="",
        description="turn_failed 时的结构化原因码。不含内部细节，不含模型原文。",
    )


class PublicTraceEvent(BaseModel):
    """落库的公开 trace 条目。与 RunEvent 同源，用于会话回看。"""

    model_config = ConfigDict(extra="forbid")

    sequence: int
    kind: RunEventKind
    status: RunEventStatus = "completed"
    summary: str = ""
    text: str = ""
    reasoning: PublicReasoningSummary | None = None
    observation: PublicObservation | None = None
    tool: ToolEventPayload | None = None
    tool_name: str = ""
    duration_ms: int = 0
