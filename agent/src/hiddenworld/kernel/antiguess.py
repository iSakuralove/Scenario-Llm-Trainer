"""基于证据覆盖率的防猜答案判定。"""

from __future__ import annotations

from collections.abc import Collection
from dataclasses import dataclass

from hiddenworld.contracts import HiddenWorld, HypothesisRelation


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
    ) -> AntiGuessDecision:
        collected = set(collected_evidence)
        best_set: list[str] = []
        best_coverage = 0.0

        for candidate in world.root_cause.sufficient_evidence_sets:
            if not candidate:
                continue
            coverage = len(collected.intersection(candidate)) / len(candidate)
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
