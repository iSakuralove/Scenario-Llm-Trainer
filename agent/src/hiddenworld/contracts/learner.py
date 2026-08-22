"""LearnerState：学生走到哪了。

相对早期设计有三处关键调整：

- **stalled_turns 与 hint_level 分工**。前者记录连续无进展轮次，后者记录当前
  教学提示强度；两者都由 Runtime 归约并允许回落，不能再把"没解锁"等同于卡住。
- **established_facts 单列**（D11）。没有它，Mentor 会反复引导学生已经走过的地方——
  这是重复感的一个独立来源，与"深挖被判重复"（M2）无关。
"""

from __future__ import annotations

from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field

# 防复读时回看的轮数。三轮足够覆盖"每轮同一个句子形状"的观感，
# 再长会开始误伤正常的话题延续。
RECENT_OPENINGS_WINDOW = 3

MasteryScore = Annotated[int, Field(ge=0, le=4)]
DetailPreference = Literal["brief", "balanced", "detailed"]
PreferenceLevel = Literal["low", "medium", "high"]


class _Strict(BaseModel):
    model_config = ConfigDict(extra="forbid")


class ExplanationPreferences(_Strict):
    """只记录学生明确表达过的解释偏好。"""

    detail: DetailPreference = "balanced"
    analogy: PreferenceLevel = "medium"
    directness: PreferenceLevel = "medium"


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
    stalled_turns: int = Field(default=0, ge=0, description="连续无进展轮次，可回落")
    concept_mastery: dict[str, MasteryScore] = Field(
        default_factory=dict,
        description="当前会话内的概念掌握度；概念 id 必须由题目 TeachingModel 声明",
    )
    skill_mastery: dict[str, MasteryScore] = Field(
        default_factory=dict,
        description="当前会话内的工程能力掌握度",
    )
    explanation_preferences: ExplanationPreferences = Field(default_factory=ExplanationPreferences)
    hint_level: int = Field(default=0, ge=0, le=4, description="当前提示强度，0 表示未提示")
    last_hint: str = Field(default="", description="最近一次已经公开给学生的提示")
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
    concept_mastery: dict[str, MasteryScore] = Field(default_factory=dict)
    skill_mastery: dict[str, MasteryScore] = Field(default_factory=dict)
    explanation_preferences: ExplanationPreferences = Field(default_factory=ExplanationPreferences)
    hint_level: int = Field(default=0, ge=0, le=4)
    last_hint: str = ""
    recent_openings: list[str] = Field(default_factory=list)


class Turn(_Strict):
    """一轮对话。transcript 里的元素。"""

    role: str = Field(description='"user" 或 "mentor"')
    content: str
    turn_number: int = 0
