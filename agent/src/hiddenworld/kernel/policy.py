"""把内部状态编译为 Mentor 可以安全消费的教学约束。"""

from __future__ import annotations

from collections.abc import Sequence

from hiddenworld.contracts import (
    ConstraintFacts,
    EvidenceCategory,
    LearnerState,
    TeachingConstraints,
    TurnAnalysis,
    direction_for_category,
)


class TeachingPolicy:
    """只决定能不能做，不决定 Mentor 该怎么说。"""

    def compile(
        self,
        learner_state: LearnerState,
        *,
        analysis: TurnAnalysis,
        completion_allowed: bool,
        evidence_coverage: str,
        may_release: Sequence[str],
        allowed_category: EvidenceCategory | None,
        contradictions: Sequence[str],
    ) -> TeachingConstraints:
        must_not = ["reveal_unreleased"]
        if not completion_allowed:
            must_not = ["confirm_hypothesis", "reveal_unreleased", "start_debrief"]

        hypothesis_supported = bool(
            learner_state.current_hypothesis
            and (learner_state.collected_evidence or learner_state.established_facts)
        )
        return TeachingConstraints(
            must_not=must_not,
            may_release=list(may_release),
            allowed_direction=(
                direction_for_category(allowed_category) if allowed_category is not None else None
            ),
            facts=ConstraintFacts(
                hypothesis_supported=hypothesis_supported,
                evidence_coverage=evidence_coverage,
                stalled_turns=learner_state.stalled_turns,
                contradictions=list(contradictions),
                student_affect=analysis.student_affect,
            ),
        )
