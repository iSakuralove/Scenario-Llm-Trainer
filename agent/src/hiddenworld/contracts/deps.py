"""两个 LLM Agent 的依赖类型。信息隔离在这里落地。

PydanticAI 的 deps 不会自动进入 prompt——deps 只在 instructions、tools 和
output validator 里被**显式读取**时才会影响模型看到的内容。这个性质是本模块
安全设计的基础：

- InterpreterDeps 里的假设表没有正确性标记，所以 TurnInterpreter 看到的是
  一份未标注的多选一。
- MentorDeps 里没有 RootCause、没有未释放证据、没有内部验证结果、没有 depth、
  没有 is_distractor。泄露在类型层面就不可能发生。
- GuardContext 挂在 MentorDeps.guard_only 上，但**instructions 渲染函数绝不
  读取它**，因此它不进入 Mentor 的 prompt；只有 output validator 读它。

最后一条是约定而非类型强制，所以必须有测试兜底：单元测试渲染 Mentor 的完整
prompt，断言其中不含任何禁止实体、根因描述或未释放证据原文。
"""

from __future__ import annotations

from dataclasses import dataclass, field

from .answer import PublicAnswerComparison
from .authorization import AuthorizedActionRef
from .learner import LearnerStateView, Turn
from .teaching import TeachingConstraints
from .version import EvidenceAvailability, ReplyMode
from .world import Hypothesis, PublicScenario, VirtualTool


@dataclass
class InterpreterDeps:
    """TurnInterpreter 的依赖。

    它需要假设候选表才能把自由文本连接成 ID，而正确假设就在表里。
    这是本设计仅有的残留泄露面，被两件事容纳在组件内部：
    表是**未标注的 4–6 选一**，且 TurnInterpreter 不与学生对话。
    """

    public_scenario: PublicScenario
    hypotheses: list[Hypothesis]  # 无 is_correct 字段，见 world.Hypothesis
    conversation_summary: str = ""
    transcript: list[Turn] = field(default_factory=list)
    known_actions: list[str] = field(
        default_factory=list,
        metadata={"why": "世界支持的动作词表，帮助模型把口语映射到规范动作串"},
    )
    virtual_tools: list[VirtualTool] = field(
        default_factory=list,
        metadata={"why": "题目声明的只读虚拟工具目录，仅用于意图匹配"},
    )


@dataclass
class GuardContext:
    """Guard 的私有上下文。**只被 output validator 读取，绝不进入 prompt。**"""

    forbidden_entities: list[str] = field(
        default_factory=list,
        metadata={
            "why": "从未释放 evidence 的 content 与根因描述中抽取的具名实体"
            "（数值、配置项名、服务名、路径）"
        },
    )
    completion_allowed: bool = False
    may_release: list[str] = field(default_factory=list)
    evidence_request: EvidenceRequest | None = None
    required_reply_mode: ReplyMode | None = None
    public_observation_texts: list[str] = field(
        default_factory=list,
        metadata={"why": "本轮已经公开的观察原文，仅用于拒绝回复重复整段工具结果"},
    )


@dataclass
class EvidenceRequest:
    """当前消息请求的证据及其世界模型可用性。"""

    requested_text: str
    availability: EvidenceAvailability
    public_message: str = ""


@dataclass
class MentorDeps:
    """Mentor 的依赖。

    没有 root_cause、没有未释放 evidence、没有 hypothesis 正确性字段、
    没有 depth、没有 is_distractor。

    released_evidence 给的是**原文且必须给全**：这些内容学生已经见过，
    藏着只会让 Mentor 说出前后矛盾的话。
    """

    public_scenario: PublicScenario
    transcript: list[Turn]
    learner_state: LearnerStateView
    constraints: TeachingConstraints
    conversation_summary: str = ""
    current_user_message: str = ""
    current_intent: str = "investigate"
    requested_action_raw: str = ""
    action_match_status: str = "none"
    evidence_request: EvidenceRequest | None = None
    authorized_actions: list[AuthorizedActionRef] = field(default_factory=list)
    simulation_tools: list[str] = field(
        default_factory=list,
        metadata={"why": "公开的虚拟工具类型与目标，不含隐藏证据"},
    )
    released_evidence: list[str] = field(default_factory=list)
    answer_comparison: PublicAnswerComparison | None = None

    # 下面这个字段对 prompt 不可见。instructions 函数绝不读取它。
    guard_only: GuardContext = field(default_factory=GuardContext)
