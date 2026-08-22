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

from pydantic import BaseModel, ConfigDict, Field, model_validator

from .version import HypothesisRelation


class CanonicalAnswer(BaseModel):
    """与 ScenarioContract 同版本持久化的唯一权威答案。"""

    model_config = ConfigDict(extra="forbid")

    canonical_conclusion: str
    root_cause_id: str
    direct_trigger: str = Field(default="", description="本次事故的直接触发变化")
    latent_issues: list[str] = Field(default_factory=list, description="被事故暴露的潜在问题")
    phenomenon: str = Field(default="", description="学生需要解释的可见现象")
    derived_risks: list[str] = Field(default_factory=list, description="由因果链推导出的业务风险")
    required_evidence_ids: list[str] = Field(default_factory=list)
    required_causal_relations: list[str] = Field(default_factory=list)
    accepted_equivalents: list[str] = Field(default_factory=list)
    solution_requirements: list[str] = Field(default_factory=list)
    answer_version: str

# AgentComparison 的抽象状态。它们只描述教学维度，不包含答案原文、精确
# 缺失点或 completion/terminal。missing_dimensions 同样只能使用这些抽象
# 维度名，不能动态拷贝 CanonicalAnswer 的具体要求。
ConclusionStatus = Literal["none", "partial", "supported", "contradictory"]
EvidenceStatus = Literal["none", "insufficient", "partial", "sufficient"]
CausalStatus = Literal["missing", "partial", "sufficient"]
ComparisonDimension = Literal["conclusion", "evidence", "causal_link", "consistency"]

# 旧 v1 事件/调用方仍可能读取这个名字；它不再是 PublicAnswerComparison
# 的字段，只作为兼容属性和输入适配的中间类型。
SupportStatus = Literal[
    "insufficiently_specific",
    "needs_more_evidence",
    "has_evidence_conflict",
    "evidence_consistent",
]


def _legacy_conclusion_status(status: object) -> ConclusionStatus:
    """把旧 v1 支持状态收窄为 V2 的抽象结论状态。"""

    if status == "has_evidence_conflict":
        return "contradictory"
    if status == "evidence_consistent":
        return "supported"
    if status == "needs_more_evidence":
        return "partial"
    return "none"


def _legacy_evidence_status(status: object) -> EvidenceStatus:
    """把旧 v1 支持状态收窄为 V2 的抽象证据状态。"""

    if status == "evidence_consistent":
        return "sufficient"
    if status == "needs_more_evidence":
        return "partial"
    if status == "has_evidence_conflict":
        return "insufficient"
    return "none"


class PublicAnswerComparison(BaseModel):
    """给 Agent/前端的抽象比较投影。

    V2 只输出五个比较语义字段：``conclusion_status``、``evidence_status``、
    ``causal_status``、``missing_dimensions``、``contradictions``。``tool``、
    ``status``、``user_points`` 是现有传输层所需的元数据/用户原话回显，不是
    判题字段。旧 v1 的 ``support_status`` / ``next_action`` 只在输入适配时
    接受，永不进入 JSON schema 或序列化结果。
    """

    model_config = ConfigDict(extra="forbid")

    tool: str = "compare_answer"
    status: str = Field(default="completed", description="工具执行状态，不是判题结果")
    user_points: list[str] = Field(
        default_factory=list,
        description="从学生自己的表述里抽出的要点。只回显他说过的话。",
    )
    # V2 字段必须出现在新 payload 中。旧 v1 只允许通过
    # ``_adapt_legacy_v1`` 输入适配后再落成这五个字段，不能靠默认值把
    # 契约缺字段悄悄吞掉。
    conclusion_status: ConclusionStatus
    evidence_status: EvidenceStatus
    causal_status: CausalStatus
    missing_dimensions: list[ComparisonDimension]
    contradictions: list[str]

    @model_validator(mode="before")
    @classmethod
    def _adapt_legacy_v1(cls, value: object) -> object:
        """只读兼容旧 payload；旧字段不会留在 V2 模型中。"""

        if not isinstance(value, dict):
            return value
        payload = dict(value)
        legacy = payload.pop("support_status", None)
        # next_action 是旧的自然语言引导，不能被带入新投影；即使旧事件有
        # 它，也只丢弃，不尝试把文案复制到任何新字段。
        payload.pop("next_action", None)
        if legacy is not None:
            payload.setdefault("conclusion_status", _legacy_conclusion_status(legacy))
            payload.setdefault("evidence_status", _legacy_evidence_status(legacy))
            payload.setdefault("causal_status", "missing")
            dimensions = payload.setdefault("missing_dimensions", [])
            payload.setdefault("contradictions", [])
            if legacy == "has_evidence_conflict" and "consistency" not in dimensions:
                dimensions.append("consistency")
        return payload

    # 这些属性只供尚未迁移的内部 v1 代码读取，均不会出现在 model_fields、
    # JSON Schema 或 model_dump 中。next_action 永远为空，防止旧文案继续引导。
    @property
    def support_status(self) -> SupportStatus:
        if self.contradictions or self.conclusion_status == "contradictory":
            return "has_evidence_conflict"
        if self.evidence_status == "none" or self.conclusion_status == "none":
            return "insufficiently_specific"
        if self.evidence_status in {"insufficient", "partial"}:
            return "needs_more_evidence"
        return "evidence_consistent"

    @property
    def next_action(self) -> str:
        return ""


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
            conclusion_status=self._conclusion_status(),
            evidence_status=self._evidence_status(),
            causal_status=self._causal_status(),
            missing_dimensions=self._missing_dimensions(),
            contradictions=list(self.contradictions),
        )

    def _conclusion_status(self) -> ConclusionStatus:
        # 顺序有意义：冲突优先于覆盖度。学生自己说的两句话打架时，
        # 先把这件事告诉他，比催他去多找一条证据有用得多。
        if self.contradictions:
            return "contradictory"
        if not self.user_points:
            return "none"
        if self.evidence_coverage >= 1.0:
            return "supported"
        if self.evidence_coverage <= 0.0:
            return "contradictory"
        return "partial"

    def _evidence_status(self) -> EvidenceStatus:
        """公开的下一步。措辞对"猜对了"和"猜错了"两种情况**必须一致**。

        这是最容易出安全 bug 的地方：只要两条分支的文案有任何可分辨的差异，
        学生就能靠反复提交不同答案来二分搜索标准答案。所以这里只按
        support_status 分支，不读 relation、不读 completion_allowed。
        """
        if self.evidence_coverage <= 0.0:
            return "none"
        if self.evidence_coverage >= 1.0:
            return "sufficient"
        return "partial"

    def _causal_status(self) -> CausalStatus:
        # 只按学生已经拥有的公开证据给出抽象进度，绝不读取 relation、
        # claim_alignment 或 solution_coverage；否则同一批观察下，猜中与猜错
        # 会因为答案匹配度不同而得到可区分的因果状态。
        if self.evidence_coverage <= 0.0:
            return "missing"
        if self.evidence_coverage < 1.0:
            return "partial"
        return "sufficient"

    def _missing_dimensions(self) -> list[ComparisonDimension]:
        dimensions: list[ComparisonDimension] = []
        if self._conclusion_status() in {"none", "contradictory"}:
            dimensions.append("conclusion")
        if self._evidence_status() != "sufficient":
            dimensions.append("evidence")
        if self._causal_status() != "sufficient":
            dimensions.append("causal_link")
        if self.contradictions:
            dimensions.append("consistency")
        return dimensions
