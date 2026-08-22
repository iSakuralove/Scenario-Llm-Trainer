"""ScenarioAgent 唯一可见的安全上下文。

这个模型是 Runtime 和模型之间的硬边界。它不接受 HiddenWorld、ScenarioContract、
CanonicalAnswer 或任何内部裁判字段；Runtime 必须先完成白名单投影，再把实例交给
ScenarioAgent。新增字段时应优先在这里审查，而不是把完整请求对象直接塞进 prompt。
"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from .authorization import AuthorizedActionRef
from .assessment import GuidanceState
from .dimensions import TeachingDimensionRef
from .learner import LearnerStateView, Turn
from .version import EvidenceAvailability
from .world import ConceptDefinition, MentorPersona, PublicScenario


class ActionCatalogEntry(BaseModel):
    """Agent 可请求的工具目录投影，不含执行结果或证据映射。"""

    model_config = ConfigDict(extra="forbid")

    tool_id: str
    kind: str
    target: str
    parameter_names: list[str] = Field(default_factory=list)
    aliases: list[str] = Field(default_factory=list)


class HypothesisCatalogEntry(BaseModel):
    """单 Agent 可见的未标注假设候选。"""

    model_config = ConfigDict(extra="forbid")

    hypothesis_id: str
    label: str


ToolResultStatus = Literal[
    "succeeded",
    "failed",
    "timeout",
    "rejected",
    "unsupported",
    "already_completed",
]

TurnPhase = Literal["new_user_turn", "after_tool_call"]
ActionHistoryKind = Literal["tool_call", "tool_result"]
ToolState = Literal["available", "consumed", "attempted", "blocked", "unavailable"]


class ActionHistoryEntry(BaseModel):
    """本轮动作历史的安全摘要。

    只记录“做了什么、针对哪个工具、Runtime 归纳出的结果”，不记录参数、授权
    标识、隐藏证据 ID 或模型原始思考。它用于让同一轮的后续模型调用知道前一
    次动作是谁发起、是否已经完成，避免把工具结果误当成无归属的观察。
    """

    model_config = ConfigDict(extra="forbid")

    action: ActionHistoryKind
    tool_name: str
    decision_summary: str = Field(default="", max_length=240)
    status: ToolResultStatus | None = None


class ToolStateView(BaseModel):
    """模型可见的工具生命周期摘要，不暴露 Runtime 授权细节。"""

    model_config = ConfigDict(extra="forbid")

    state: ToolState
    reason: str = Field(default="", max_length=240)


class AgentToolResult(BaseModel):
    """回注 Agent 的安全工具结果。"""

    model_config = ConfigDict(extra="forbid")

    call_id: str
    tool_id: str
    tool_kind: str
    status: ToolResultStatus
    content: str = ""
    error_code: str = ""


class EvidenceRequestView(BaseModel):
    """当前请求的证据可用性投影；不包含 HiddenWorld 内容。"""

    model_config = ConfigDict(extra="forbid")

    requested_text: str
    availability: EvidenceAvailability
    public_message: str = ""


class AgentBudgetView(BaseModel):
    """只读预算投影，帮助 Agent 主动收束，不暴露内部 deadline。"""

    model_config = ConfigDict(extra="forbid")

    remaining_model_rounds: int = Field(ge=0)
    remaining_tool_calls: int = Field(ge=0)


class AgentTurnControlView(BaseModel):
    """Agent 只能知道会话是否已经进入终止状态。

    ``completion_allowed`` / ``completion_ready`` 仍然是 Runtime 私有裁判结果，
    不能因为把上一轮控制状态回注给 Agent 而越过安全边界。这个视图只保留
    生命周期信号 ``terminal``；上一轮的教学导航则通过 ``guidance_state``
    和 ``teaching_navigation`` 回注。
    """

    model_config = ConfigDict(extra="forbid")

    terminal: bool = False


class AgentContext(BaseModel):
    """单 Agent 的完整输入；所有字段都必须是安全投影。"""

    model_config = ConfigDict(extra="forbid")

    public_scenario: PublicScenario
    conversation_summary: str = ""
    transcript: list[Turn] = Field(default_factory=list)
    current_user_message: str = ""
    # 同一轮模型重采样时保持原始用户消息不变；phase 只区分首次理解和
    # 工具结果回注后的继续决策，不代表前端 SSE 的 understanding/replying 阶段。
    phase: TurnPhase = "new_user_turn"
    turn_id: str = ""
    original_user_message: str = ""
    # Runtime 只在模型需要重生成回复时写入内部校验反馈；它不携带答案、
    # 未公开证据或实现细节，也不应被展示给学生。
    reply_feedback: str = ""
    evidence_request: EvidenceRequestView | None = None
    learner_summary: LearnerStateView
    mentor_persona: MentorPersona = Field(default_factory=MentorPersona)
    concept_catalog: list[ConceptDefinition] = Field(default_factory=list)
    teaching_navigation: list[TeachingDimensionRef] = Field(default_factory=list)
    action_catalog: list[ActionCatalogEntry] = Field(default_factory=list)
    hypothesis_catalog: list[HypothesisCatalogEntry] = Field(default_factory=list)
    authorized_actions: list[AuthorizedActionRef] = Field(default_factory=list)
    tool_results: list[AgentToolResult] = Field(default_factory=list)
    action_history: list[ActionHistoryEntry] = Field(default_factory=list)
    tool_states: dict[str, ToolStateView] = Field(default_factory=dict)
    budget: AgentBudgetView
    # 这是上一轮归约后的安全教学状态切片。Runtime 每轮都应从持久化/请求快照
    # 回注它，而不是固定构造默认值，否则 Agent 会在每轮都误以为处于
    # normal_diagnosis，丢失卡住、核验和当前焦点。
    guidance_state: GuidanceState = Field(default_factory=GuidanceState)
    turn_control: AgentTurnControlView = Field(default_factory=AgentTurnControlView)
