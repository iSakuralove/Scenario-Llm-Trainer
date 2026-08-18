"""答案对比的内部结果与公开投影。

`compare_answer` 是一期唯一的工具，它是**答案对比，不是答案探测器**。
这个区分全靠这里的两个类型：完整比较结果只进内部审计，Mentor 和前端
只拿得到公开投影。

公开投影永远不包含：对错、标准答案、命中假设、相似度、缺失的正确要点、
completion_allowed，以及任何等价暗示。support_status 只表达"本轮观察够不够
继续行动"，**不表达"用户是否猜中答案"**。

会话过程中工具只提供安全辅导信号。用户点击"提交最终答案"结束训练后，
复盘页才展示用户答案与参考答案、命中点、缺失证据、因果链和验证闭环的完整比较。
"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from .version import HypothesisRelation

# 公开的支持状态。四选一，且四个取值都只描述"证据够不够"，
# 没有任何一个能被反推成"猜中了 / 没猜中"。
SupportStatus = Literal[
    "insufficiently_specific",  # 说法太笼统，还没法对照任何观察
    "needs_more_evidence",  # 方向可以继续，但手上的观察还不足以支撑
    "has_evidence_conflict",  # 与学生自己已确立的事实存在冲突
    "evidence_consistent",  # 与已有观察一致，可以继续往下走
]


class PublicAnswerComparison(BaseModel):
    """给 Mentor 和前端看的投影。字段白名单在类型层面固定。

    注意这里**没有** correct / target / claim_alignment / matched_hypotheses /
    similarity / missing_points / completion_allowed。不是靠提示词约束不说，
    是这些字段根本不存在于这个类型里。
    """

    model_config = ConfigDict(extra="forbid")

    tool: str = "compare_answer"
    status: str = Field(default="completed", description="工具执行状态，不是判题结果")
    user_points: list[str] = Field(
        default_factory=list,
        description="从学生自己的表述里抽出的要点。只回显他说过的话。",
    )
    support_status: SupportStatus
    next_action: str = Field(
        default="",
        description="结构化的下一步建议，只引用已公开事实。不得暗示对错。",
    )


class InternalAnswerComparison(BaseModel):
    """完整比较结果。**只进内部审计，永不出现在任何 prompt 或 SSE 里。**"""

    model_config = ConfigDict(extra="forbid")

    answer_attempt_id: str
    relation: HypothesisRelation
    claim_alignment: float = Field(ge=0.0, le=1.0)
    evidence_coverage: float = Field(ge=0.0, le=1.0, description="各充分证据集覆盖率的最大值")
    best_evidence_set: list[str] = Field(default_factory=list)
    missing_evidence: list[str] = Field(default_factory=list)
    contradictions: list[str] = Field(
        default_factory=list,
        description="与学生自己已确立事实的冲突。只引用他自己的原话。",
    )
    solution_coverage: float = Field(default=0.0, ge=0.0, le=1.0)
    missing_solution_requirements: list[str] = Field(default_factory=list)
    completion_allowed: bool = Field(
        description="relation == target 且 evidence_coverage >= 1.0。猜对但没有证明时为 False。",
    )
    user_points: list[str] = Field(default_factory=list)

    def to_public(self) -> PublicAnswerComparison:
        """向下投影。这是内部结果通向学生的**唯一**通道。

        投影逻辑刻意写得笨：先算出 support_status，再只搬运 user_points。
        任何想"顺便带一点有用信息"的改动都会在这里被 review 抓住。
        """
        return PublicAnswerComparison(
            user_points=list(self.user_points),
            support_status=self._support_status(),
            next_action=self._next_action(),
        )

    def _support_status(self) -> SupportStatus:
        # 顺序有意义：冲突优先于覆盖度。学生自己说的两句话打架时，
        # 先把这件事告诉他，比催他去多找一条证据有用得多。
        if self.contradictions:
            return "has_evidence_conflict"
        if not self.user_points:
            return "insufficiently_specific"
        if self.evidence_coverage >= 1.0:
            return "evidence_consistent"
        if self.evidence_coverage <= 0.0:
            return "insufficiently_specific"
        return "needs_more_evidence"

    def _next_action(self) -> str:
        """公开的下一步。措辞对"猜对了"和"猜错了"两种情况**必须一致**。

        这是最容易出安全 bug 的地方：只要两条分支的文案有任何可分辨的差异，
        学生就能靠反复提交不同答案来二分搜索标准答案。所以这里只按
        support_status 分支，不读 relation、不读 completion_allowed。
        """
        status = self._support_status()
        if status == "has_evidence_conflict":
            return "先回头核对一下你自己给出的两处说法，它们对不上。"
        if status == "insufficiently_specific":
            return "把结论说得更具体一些，指向某个组件或某次变更。"
        if status == "needs_more_evidence":
            return "继续补充能支撑这个结论的直接观察。"
        return "你的说法和已有观察一致，可以继续往下推进。"
