"""把规范动作映射为 HiddenWorld 中已配置的观察。"""

from __future__ import annotations

from collections.abc import Collection

from hiddenworld.contracts import HiddenWorld, Observation

UNMET_PREREQUISITE_RESULT = "当前还缺少足够上下文，暂时无法得到这项观察。"
NEUTRAL_OBSERVATION_RESULT = "这个动作暂时没有返回可用的新观察。"


class HiddenWorldEngine:
    """世界查询的唯一公开接口。"""

    def observe(
        self,
        world: HiddenWorld,
        *,
        action: str,
        collected_evidence: Collection[str],
    ) -> Observation:
        collected = set(collected_evidence)
        for observation in world.observations:
            if observation.action == action:
                prerequisites = {
                    prerequisite
                    for evidence_id in observation.yields_evidence
                    if (node := world.evidence_by_id(evidence_id)) is not None
                    for prerequisite in node.prerequisites
                }
                if not prerequisites.issubset(collected):
                    return Observation(
                        action=action,
                        result=observation.unmet_prerequisite_result or UNMET_PREREQUISITE_RESULT,
                        is_negative=False,
                        yields_evidence=[],
                        rules_out=[],
                    )
                return observation.model_copy(deep=True)
        return Observation(
            action=action,
            result=NEUTRAL_OBSERVATION_RESULT,
            is_negative=False,
            yields_evidence=[],
            rules_out=[],
        )
