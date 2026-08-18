"""TeachingPolicy 的产物：约束，不是动作。

信息原则（知道答案的组件不说话，说话的组件不知道答案）只约束了**谁知道什么**，
没约束**谁决定什么**。缺了对称的一条：

    决定"能不能做"的组件，不决定"该怎么说"。

旧实现里上游选定言语行为（decision = ask_for_evidence），prompt 再要求模型
"只能改写得自然，不能新增任何新事实"。不论策略是规则还是模型、跑在 Go 还是
Python——**言语行为一旦在"说话的组件"之外被决定，死板就回来了**。

所以这里只圈定边界，Mentor 在边界内自行决定这一轮做什么：可能要求验证；
可能发现学生连卡三轮、先退一步帮他梳理已有结论；可能给两条路让他选；
也可能这轮不提问，让他消化。

安全性没有损失——must_not 由 Guard 强制执行，Mentor 想确认也确认不了。
"""

from __future__ import annotations

from pydantic import BaseModel, ConfigDict, Field

from .version import AllowedDirection, MustNotConstraint, StudentAffect


class ConstraintFacts(BaseModel):
    """交给 Mentor 的事实。每一项都经过"能不能反推出答案"的审查。"""

    model_config = ConfigDict(extra="forbid")

    hypothesis_supported: bool = Field(
        description=(
            "学生**已收集的证据**是否构成对他当前假设的支持链。\n"
            "这是相对 LearnerState 的判断，与真相无关：一个错误的假设也可以"
            "暂时被学生手上的证据支持，一个正确的假设在没有证据时同样是 False。"
            "因此它不泄露正确性。"
        ),
    )
    evidence_coverage: str = Field(
        description='覆盖最好的那组充分证据集的进度，形如 "2/4"。粗粒度进度是可接受的教学信号。',
    )
    stalled_turns: int = Field(description="连续无进展轮次。可回落。")
    contradictions: list[str] = Field(
        default_factory=list,
        description="学生自己前后说法的冲突。**只引用他自己的原话**，不引入任何外部判断。",
    )
    student_affect: StudentAffect


class TeachingConstraints(BaseModel):
    """本轮的边界。

    **刻意不含 action / decision / teaching_state。** schema 里一旦出现这类枚举，
    模型就会"为了填对枚举而思考"，分类查表就从后门溜回来了。
    """

    model_config = ConfigDict(extra="forbid")

    must_not: list[MustNotConstraint] = Field(
        default_factory=list,
        description="硬禁止。由 Guard 强制执行，不是给模型的建议。",
    )
    may_release: list[str] = Field(
        default_factory=list,
        description="本轮 ClueGate 批准释放的 evidence id。Mentor 请求释放的集合必须是它的子集。",
    )
    allowed_direction: AllowedDirection | None = Field(
        default=None,
        description="粗类别方向。永远不下钻到节点级描述。",
    )
    facts: ConstraintFacts
