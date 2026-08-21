"""模型节点构造器；旧双节点入口暂时保留以兼容历史测试。"""

from .interpreter import build_interpreter_prompt, create_interpreter_agent
from .mentor import build_mentor_prompt, create_mentor_agent
from .models import build_deepseek_model, build_glm_model, build_xuan_model
from .tools import CompareAnswerRuntime, compare_answer_tool
from .scenario_agent import (
    build_scenario_agent_prompt,
    create_scenario_agent,
    create_scenario_agent_runner,
)

__all__ = [
    "build_interpreter_prompt",
    "build_mentor_prompt",
    "create_interpreter_agent",
    "create_mentor_agent",
    "CompareAnswerRuntime",
    "compare_answer_tool",
    "build_scenario_agent_prompt",
    "create_scenario_agent",
    "create_scenario_agent_runner",
    "build_deepseek_model",
    "build_glm_model",
    "build_xuan_model",
]
