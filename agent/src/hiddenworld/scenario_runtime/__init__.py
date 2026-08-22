"""ScenarioAgent Runtime 的可复用调度组件。"""

from .agent_loop import AgentLoop, AgentLoopBudgetExceeded, AgentLoopEvent, ScenarioRunFrame
from .batch_scheduler import BatchPlan, BatchScheduler
from .context import project_agent_context
from .response_brief import (
    CausalBoundary,
    InvestigationView,
    PublicObservationBrief,
    ResponseBrief,
    ResponseBriefBuilder,
)
from .virtual_tools import VirtualObservationExecutor


def __getattr__(name: str):
    """按需加载 TurnRuntime，避免 ScenarioAgent ↔ Runtime 循环导入。

    ScenarioAgent 的 prompt 需要读取 response_brief，但 turn_runtime 又需要
    ScenarioAgentRunner。把重量级运行时改成懒加载既保留旧的包级导出，也让
    纯 prompt/契约模块可以独立导入。
    """

    if name == "SingleAgentRuntime":
        from .turn_runtime import SingleAgentRuntime

        return SingleAgentRuntime
    raise AttributeError(name)

__all__ = [
    "AgentLoop",
    "AgentLoopBudgetExceeded",
    "AgentLoopEvent",
    "ScenarioRunFrame",
    "BatchPlan",
    "BatchScheduler",
    "project_agent_context",
    "CausalBoundary",
    "InvestigationView",
    "PublicObservationBrief",
    "ResponseBrief",
    "ResponseBriefBuilder",
    "VirtualObservationExecutor",
    "SingleAgentRuntime",
]
