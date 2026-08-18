"""把一轮观察归并进 LearnerState。"""

from __future__ import annotations

from collections.abc import Iterable, Sequence

from hiddenworld.contracts import LearnerState, Observation, TurnAnalysis


class EvidenceEngine:
    """学习状态推进的唯一公开接口；输入状态不会被就地修改。"""

    def advance(
        self,
        state: LearnerState,
        *,
        analysis: TurnAnalysis,
        observations: Sequence[Observation],
    ) -> LearnerState:
        updated = state.model_copy(deep=True)
        progress = False

        if not analysis.is_low_confidence() and not analysis.is_noise:
            _extend_unique(updated.actions_taken, analysis.actions)
            progress |= _extend_unique(updated.established_facts, analysis.established_facts)
            if analysis.hypothesis_id and updated.current_hypothesis != analysis.hypothesis_id:
                updated.current_hypothesis = analysis.hypothesis_id
                progress = True

        for observation in observations:
            progress |= _extend_unique(updated.collected_evidence, observation.yields_evidence)
            progress |= _extend_unique(updated.ruled_out_hypotheses, observation.rules_out)

        if progress:
            updated.effective_turns += 1
            updated.stalled_turns = 0
        elif not analysis.is_noise:
            updated.stalled_turns += 1

        return updated


def _extend_unique(target: list[str], values: Iterable[str]) -> bool:
    changed = False
    known = set(target)
    for value in values:
        if not value or value in known:
            continue
        target.append(value)
        known.add(value)
        changed = True
    return changed
