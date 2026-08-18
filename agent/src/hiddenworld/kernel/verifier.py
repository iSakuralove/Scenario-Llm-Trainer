"""把学生连接到的假设与 HiddenWorld 真相做确定性关系判定。"""

from __future__ import annotations

from hiddenworld.contracts import (
    HYPOTHESIS_OTHER,
    HiddenWorld,
    HypothesisRelation,
    LearnerState,
)


class RootCauseVerifier:
    """只返回关系，不生成任何用户可见文本。"""

    def relation(
        self,
        world: HiddenWorld,
        *,
        hypothesis_id: str,
        learner_state: LearnerState,
    ) -> HypothesisRelation:
        if not hypothesis_id or hypothesis_id == HYPOTHESIS_OTHER:
            return "unknown"
        if hypothesis_id in world.root_cause.accepted_hypotheses:
            return "target"
        if hypothesis_id in learner_state.ruled_out_hypotheses:
            return "ruled_out"
        if hypothesis_id in world.hypothesis_ids():
            return "unrelated"
        return "unknown"
