"""LearnerState：学生走到哪了。

相对早期设计有两处关键调整：

- **hint_level 移除**，改用 stalled_turns。前者只增不减（审查报告 M3），后者由
  "最近 N 轮有效观察数"推出，天然能回落。提示等级本就不该由"没解锁"驱动。
- **established_facts 单列**（D11）。没有它，Mentor 会反复引导学生已经走过的地方——
  这是重复感的一个独立来源，与"深挖被判重复"（M2）无关。
"""

from __future__ import annotations

from pydantic import BaseModel, ConfigDict, Field

# 防复读时回看的轮数。三轮足够覆盖"每轮同一个句子形状"的观感，
# 再长会开始误伤正常的话题延续。
RECENT_OPENINGS_WINDOW = 3


class _Strict(BaseModel):
    model_config = ConfigDict(extra="forbid")


class LearnerState(_Strict):
    """权威学生状态。含 evidence id，只在确定性组件之间传递。"""

    collected_evidence: list[str] = Field(default_factory=list)
    ruled_out_hypotheses: list[str] = Field(default_factory=list)
    current_hypothesis: str | None = None
    established_facts: list[str] = Field(
        default_factory=list,
        description="学生自己确立的事实，含他自行推出、系统从未释放的那些",
    )
    actions_taken: list[str] = Field(default_factory=list)
    current_focus: str = ""
    effective_turns: int = 0
    stalled_turns: int = Field(default=0, description="可回落，取代只增不减的 hint_level")
    recent_openings: list[str] = Field(
        default_factory=list,
        description=f"最近 {RECENT_OPENINGS_WINDOW} 轮 Mentor 的开场句式，用于防复读",
    )


class LearnerStateView(_Strict):
    """给 Mentor 的脱敏视图。

    这里出现的每一项都是**学生自己产生的信息**：他做过什么、说过什么、
    自己排除了什么。回给他不构成泄露。

    刻意不含 collected_evidence 的 id 列表——evidence id 会暗示世界里
    还有多少条没拿到，那是结构信息。证据的进度由 ConstraintFacts.evidence_coverage
    以 "2/4" 的粗粒度形式表达。
    """

    established_facts: list[str] = Field(default_factory=list)
    actions_taken: list[str] = Field(default_factory=list)
    current_focus: str = ""
    current_hypothesis_label: str | None = Field(
        default=None,
        description="用 label 而非 hypothesis_id：id 是内部结构，label 是学生说过的话",
    )
    ruled_out_labels: list[str] = Field(
        default_factory=list,
        description="学生自己排除掉的方向，同样用 label",
    )
    effective_turns: int = 0
    stalled_turns: int = 0
    recent_openings: list[str] = Field(default_factory=list)


class Turn(_Strict):
    """一轮对话。transcript 里的元素。"""

    role: str = Field(description='"user" 或 "mentor"')
    content: str
    turn_number: int = 0
