"""把一轮观察归并进 LearnerState。"""

from __future__ import annotations

from collections.abc import Collection, Iterable, Sequence

from hiddenworld.contracts import LearnerState, Observation, TurnAnalysis


class EvidenceEngine:
    """学习状态推进的唯一公开接口；输入状态不会被就地修改。"""

    def advance(
        self,
        state: LearnerState,
        *,
        analysis: TurnAnalysis,
        observations: Sequence[Observation],
        valid_hypothesis_ids: Collection[str] | None = None,
    ) -> LearnerState:
        """把本轮输入归约为新状态。

        ``hypothesis_id`` 来自模型，不能直接成为状态事实。生产 Runtime 会
        传入题目声明的假设集合；集合外的值只能保持为未连接，不能生成后续的
        ``set_current_hypothesis`` 提议。``H_OTHER`` 是否可进入状态也由题目
        是否声明它决定，不能在这里用一个全局特例覆盖题目契约。
        """
        updated = state.model_copy(deep=True)
        progress = False

        if not analysis.is_low_confidence() and not analysis.is_noise:
            _extend_unique(updated.actions_taken, analysis.actions)
            progress |= _extend_unique(updated.established_facts, analysis.established_facts)
            hypothesis_id = analysis.hypothesis_id.strip()
            hypothesis_is_declared = (
                bool(hypothesis_id)
                and (valid_hypothesis_ids is None or hypothesis_id in valid_hypothesis_ids)
            )
            if hypothesis_is_declared and updated.current_hypothesis != hypothesis_id:
                updated.current_hypothesis = hypothesis_id
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
