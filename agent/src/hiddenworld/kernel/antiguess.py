"""基于证据覆盖率的防猜答案判定。"""

from __future__ import annotations

from collections.abc import Collection
from dataclasses import dataclass

from hiddenworld.contracts import HiddenWorld, HypothesisRelation
from hiddenworld.contracts.answer import CanonicalAnswer


@dataclass(frozen=True)
class AntiGuessDecision:
    coverage: float
    best_evidence_set: list[str]
    missing_evidence: list[str]
    completion_allowed: bool


class AntiGuess:
    """取多条充分证据路径中的最大覆盖率，不因猜中而跳过验证。"""

    def evaluate(
        self,
        world: HiddenWorld,
        *,
        collected_evidence: Collection[str],
        relation: HypothesisRelation,
        canonical_answer: CanonicalAnswer | None = None,
    ) -> AntiGuessDecision:
        """根据权威答案的 required_evidence_ids 计算证据覆盖率。

        V2 题目优先使用 CanonicalAnswer；RootCause.sufficient_evidence_sets
        仅作为旧 v1 题目的兼容回退，不能覆盖已持久化的权威答案。
        """

        collected = set(collected_evidence)
        best_set: list[str] = []
        best_coverage = 0.0

        answer = canonical_answer or world.canonical_answer
        if answer is not None:
            candidates = [answer.required_evidence_ids]
        else:
            candidates = world.root_cause.sufficient_evidence_sets

        for candidate in candidates:
            # 空证据集在 CanonicalAnswer 中表示该题不要求额外观察，覆盖率
            # 应为 1，而不是让“没有证据”被误判成 0。
            coverage = (
                len(collected.intersection(candidate)) / len(candidate)
                if candidate
                else 1.0
            )
            if coverage > best_coverage or not best_set:
                best_set = list(candidate)
                best_coverage = coverage

        missing = [evidence_id for evidence_id in best_set if evidence_id not in collected]
        return AntiGuessDecision(
            coverage=best_coverage,
            best_evidence_set=best_set,
            missing_evidence=missing,
            completion_allowed=relation == "target" and best_coverage >= 1.0,
        )
