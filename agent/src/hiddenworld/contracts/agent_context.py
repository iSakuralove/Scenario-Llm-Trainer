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
from .world import PublicScenario


class ActionCatalogEntry(BaseModel):
    """Agent 可请求的工具目录投影，不含执行结果或证据映射。"""

    model_config = ConfigDict(extra="forbid")

    tool_id: str
    kind: str
    target: str
    parameter_names: list[str] = Field(default_factory=list)
    aliases: list[str] = Field(default_factory=list)


ToolResultStatus = Literal[
    "succeeded",
    "failed",
    "timeout",
    "rejected",
    "unsupported",
    "already_completed",
]


class AgentToolResult(BaseModel):
    """回注 Agent 的安全工具结果。"""

    model_config = ConfigDict(extra="forbid")

    call_id: str
    tool_id: str
    tool_kind: str
    status: ToolResultStatus
    content: str = ""
    error_code: str = ""


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
    transcript: list[Turn] = Field(default_factory=list)
    current_user_message: str = ""
    learner_summary: LearnerStateView
    teaching_navigation: list[TeachingDimensionRef] = Field(default_factory=list)
    action_catalog: list[ActionCatalogEntry] = Field(default_factory=list)
    authorized_actions: list[AuthorizedActionRef] = Field(default_factory=list)
    tool_results: list[AgentToolResult] = Field(default_factory=list)
    budget: AgentBudgetView
    # 这是上一轮归约后的安全教学状态切片。Runtime 每轮都应从持久化/请求快照
    # 回注它，而不是固定构造默认值，否则 Agent 会在每轮都误以为处于
    # normal_diagnosis，丢失卡住、核验和当前焦点。
    guidance_state: GuidanceState = Field(default_factory=GuidanceState)
    turn_control: AgentTurnControlView = Field(default_factory=AgentTurnControlView)
