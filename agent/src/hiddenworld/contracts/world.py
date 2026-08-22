"""HiddenWorld 数据模型：故障现场作为一个可查询的世界。

这是本架构相对"线索池"的核心差异。旧模型里学生说中关键词就解锁一条预写好的
线索，没命中就 NoNewClueStreak++；于是"先看看 CPU，CPU 正常"这样一次**正确的
排除动作**被系统记成了失败。

这里把现场建模成一个可以对动作作出响应的世界：学生发出动作，世界返回观察结果，
**阴性结果同样是合法信息，同样推进 LearnerState**。

本模块的对象含答案，只允许确定性组件（HiddenWorldEngine / EvidenceEngine /
RootCauseVerifier / AntiGuess / ClueGate / Guard）消费。根因原文从头到尾
不进入任何一个 LLM prompt——这不是"叮嘱模型别说"，是"模型没有渠道知道"。
"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from .version import EvidenceAvailability, EvidenceCategory
from .answer import CanonicalAnswer


class _Strict(BaseModel):
    """契约模型的公共基类：拒绝未知字段。

    题库是生成出来的，模型多吐一个字段时必须当场失败，而不是静默丢弃——
    静默丢弃会让一道结构不完整的题一路走到学生面前才暴露。
    """

    model_config = ConfigDict(extra="forbid")


class Hypothesis(_Strict):
    """学生可能提出的假设。

    **刻意不含 is_correct。** TurnInterpreter 需要这张表才能把自由文本连接到
    假设 ID，而正确答案就在表里；它看到的必须是一份未标注的 4–6 选一。
    正确性只存在于 RootCause.accepted_hypotheses，那是 HiddenWorld 的字段，
    不出确定性组件。
    """

    hypothesis_id: str
    label: str = Field(description="供 TurnInterpreter 做连接的自然语言标签，如「索引问题」")


class EvidenceNode(_Strict):
    """一条证据。content 是原文，只有 HiddenWorld / EvidenceEngine / Guard 可见。"""

    evidence_id: str
    content: str
    category: EvidenceCategory
    prerequisites: list[str] = Field(
        default_factory=list,
        description="前置证据 id。迁移自 Go 侧 Clue.PrerequisiteClues。",
    )
    obtained_by: list[str] = Field(
        default_factory=list,
        description="哪些 action 能取得这条证据",
    )
    clue_importance: Literal["none", "supporting", "core"] = "none"
    public_title: str = Field(default="", description="公开线索标题；不得包含内部 evidence id")
    diagnostic_role: str = Field(default="", description="该证据在排查链中的公开教学角色")


class MentorPersona(_Strict):
    """题目级导师表达偏好；只控制说法，不控制事实。"""

    style_name: str = ""
    tone: str = ""
    detail: Literal["brief", "balanced", "detailed"] = "balanced"
    directness: float = Field(default=0.5, ge=0.0, le=1.0)
    humor: float = Field(default=0.0, ge=0.0, le=1.0)
    timeline_focus: float = Field(default=0.0, ge=0.0, le=1.0)
    question_frequency: float = Field(default=0.5, ge=0.0, le=1.0)


class ConceptDefinition(_Strict):
    """可安全进入 AgentContext 的概念目录。"""

    concept_id: str
    label: str
    summary: str
    aliases: list[str] = Field(default_factory=list)


class EvidenceAvailabilityRule(_Strict):
    """题面数据可用性；Runtime 用它诚实回答未提供的数据请求。"""

    request_patterns: list[str] = Field(default_factory=list)
    availability: EvidenceAvailability
    public_message: str = ""
    action_ids: list[str] = Field(
        default_factory=list,
        description="AVAILABLE/SIMULATED_ALLOWED 对应的已声明观察动作；不可据此生成数据",
    )


class HintStep(_Strict):
    """确定性提示阶梯。提示事实不进入 AgentContext。"""

    level: int = Field(ge=1, le=4)
    public_hint: str
    focus_action_ids: list[str] = Field(default_factory=list)


class TeachingModel(_Strict):
    """题目可复用的教学配置；答案与安全投影仍保持物理隔离。"""

    mentor_persona: MentorPersona = Field(default_factory=MentorPersona)
    concepts: list[ConceptDefinition] = Field(default_factory=list)
    evidence_availability_rules: list[EvidenceAvailabilityRule] = Field(default_factory=list)
    hint_ladder: list[HintStep] = Field(default_factory=list)


class Observation(_Strict):
    """世界对一个动作的响应。让排除法成为一等公民。

    学生查 CPU 得到正常：is_negative=True，rules_out=["H_CPU_BOUND"]，
    LearnerState 前进。这一条直接消灭了 NoNewClueStreak 那套逻辑。
    """

    action: str = Field(description='动作标识，如 "inspect:metrics.cpu"')
    result: str = Field(description='世界返回的观察结果，如 "CPU 使用率 35%，无异常"')
    is_negative: bool = Field(description="阴性结果同样是进展")
    yields_evidence: list[str] = Field(default_factory=list, description="产出的 evidence id，可为空")
    rules_out: list[str] = Field(default_factory=list, description="排除掉的 hypothesis id")
    unmet_prerequisite_result: str = Field(
        default="",
        description=(
            "前置证据还没到手时，世界给出的回应。\n\n"
            "学生第 1 轮就要求看 EXPLAIN，此时他还没找到慢 SQL。**不能**回答"
            "「该线索尚未解锁」——那既暴露了系统的存在，也暗示了存在一条前置。\n"
            "正确做法是让世界的物理性自己表达约束：「你要看哪条 SQL 的执行计划？」\n"
            "留空时由引擎给出一句不含新答案信息的中性回应。"
        ),
    )


class VirtualTool(_Strict):
    """题目自带的只读虚拟工具声明。

    工具只描述可模拟的查询入口，运行时永远不会执行 SQL、Shell、HTTP 或外部 API。
    ``simulated_output`` 必须与对应 Observation 的公开结果保持一致；真正的证据
    释放仍由 Observation/EvidenceEngine 控制。
    """

    tool_id: str = Field(description='稳定的工具标识，如 "tool.logs.callback"')
    kind: str = Field(description="工具类型：logs/config/metrics/database/dependency")
    target: str = Field(description="可查询的对象，例如回调服务日志或订单库写入")
    aliases: list[str] = Field(default_factory=list, description="自然语言入口别名")
    query_patterns: list[str] = Field(
        default_factory=list,
        description="允许识别的只读 SQL、日志查询或配置查询片段；仅用于意图匹配",
    )
    redacted_parameters: list[str] = Field(
        default_factory=list,
        description="可在公开线索中显示的脱敏参数名",
    )
    simulated_output: str = Field(description="该工具在题目虚拟数据集上的模拟输出")
    observation_action: str = Field(description="映射到 observations 的规范动作")
    evidence_ids: list[str] = Field(default_factory=list, description="该工具可关联的公开 evidence id")



class RootCause(_Strict):
    """真相。永不进入任何 prompt。"""

    id: str
    category: EvidenceCategory
    component: str
    description: str
    sufficient_evidence_sets: list[list[str]] = Field(
        description=(
            "若干组**充分**证据集，覆盖率取各组最大值。\n"
            "不用定长 required_evidence，因为真实排障常有多条有效路径：索引失效这个案例，"
            "从慢 SQL 进和从发布记录进都成立，但走后者的人可能永远不看 rows_examined——"
            "定长清单会判他证据不足。"
        ),
    )
    accepted_hypotheses: list[str] = Field(description="哪些 hypothesis_id 算命中真相")
    solution_requirements: list[str] = Field(default_factory=list)


class SolutionRubric(_Strict):
    """解决方案的评判标准，用于复盘页与最终答案比较。"""

    required_actions: list[str] = Field(default_factory=list, description="必须提到的修复动作")
    verification_steps: list[str] = Field(default_factory=list, description="验证闭环步骤")
    rollback_notes: list[str] = Field(default_factory=list, description="回滚/观察要点")


class MisconceptionRule(_Strict):
    """常见误解方向。用于识别学生走进了已知的坑，不用于惩罚。"""

    misconception_id: str
    pattern_hypotheses: list[str] = Field(
        default_factory=list,
        description="触发该误解的 hypothesis id",
    )
    why_wrong: str = Field(description="为什么这条路走不通。仅供内部审计与 TeachingPolicy 参考。")


class ArchitectureDiagramNode(_Strict):
    """后端生成的架构图节点；仅作为题面上下文透传。"""

    id: str
    label: str


class ArchitectureDiagramEdge(_Strict):
    """后端生成的架构图连线；仅作为题面上下文透传。"""

    from_node: str = Field(alias="from")
    to: str
    label: str = ""
    style: str = ""


class ArchitectureDiagramSpec(_Strict):
    """结构化架构图描述，与 Go 侧 PublicScenario 保持兼容。"""

    direction: str = ""
    nodes: list[ArchitectureDiagramNode] = Field(default_factory=list)
    edges: list[ArchitectureDiagramEdge] = Field(default_factory=list)


class PublicScenario(_Strict):
    """学生可见的题面。这是唯一允许进入 Mentor 上下文的场景描述。"""

    title: str
    description: str
    environment: str = Field(default="", description="架构/环境说明")
    initial_symptoms: list[str] = Field(default_factory=list, description="学生一开始就知道的现象")
    architecture_diagram: str = Field(default="", description="Mermaid 源码，可为空")
    architecture_diagram_spec: ArchitectureDiagramSpec | None = Field(
        default=None,
        description="后端生成的结构化架构图描述；Agent 只读，不参与根因判断。",
    )


class HiddenWorld(_Strict):
    """完整的世界。含答案，只由确定性组件消费。"""

    root_cause: RootCause
    teaching_model: TeachingModel = Field(default_factory=TeachingModel)
    canonical_answer: CanonicalAnswer | None = Field(
        default=None,
        description="V2 题目必填；None 仅供旧 v1 题目读取兼容。",
    )
    diagnostic_relations: list[str] = Field(default_factory=list)
    hypotheses: list[Hypothesis]
    evidence_graph: list[EvidenceNode]
    observations: list[Observation]
    virtual_tools: list[VirtualTool] = Field(
        default_factory=list,
        description="题目自带的虚拟工具目录；为空时由 observations 向后兼容",
    )
    solution_rubric: SolutionRubric
    misconception_rules: list[MisconceptionRule] = Field(default_factory=list)

    def evidence_by_id(self, evidence_id: str) -> EvidenceNode | None:
        for node in self.evidence_graph:
            if node.evidence_id == evidence_id:
                return node
        return None

    def hypothesis_ids(self) -> set[str]:
        return {item.hypothesis_id for item in self.hypotheses}
