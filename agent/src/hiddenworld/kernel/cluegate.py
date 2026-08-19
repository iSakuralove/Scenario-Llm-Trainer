"""按显式动作、前置条件与单轮预算审批证据释放。"""

from __future__ import annotations

from collections.abc import Collection, Sequence

from hiddenworld.contracts import HiddenWorld


class ClueGate:
    """一次调用可批准多条证据，让专家快车道自然涌现。"""

    def approve(
        self,
        world: HiddenWorld,
        *,
        actions: Sequence[str],
        collected_evidence: Collection[str],
        max_releases: int,
    ) -> list[str]:
        if max_releases <= 0:
            return []

        requested = []
        requested_ids: set[str] = set()
        for action in actions:
            for node in world.evidence_graph:
                if action not in node.obtained_by or node.evidence_id in requested_ids:
                    continue
                requested.append(node)
                requested_ids.add(node.evidence_id)

        available = set(collected_evidence)
        approved: list[str] = []
        made_progress = True
        while made_progress and len(approved) < max_releases:
            made_progress = False
            for node in requested:
                if len(approved) >= max_releases:
                    break
                if node.evidence_id in available:
                    continue
                if not set(node.prerequisites).issubset(available):
                    continue
                approved.append(node.evidence_id)
                available.add(node.evidence_id)
                made_progress = True
        return approved

    def approve_on_stall(
        self,
        world: HiddenWorld,
        *,
        collected_evidence: Collection[str],
    ) -> str:
        """卡住时的兜底释放：最多一条无前置、尚未收集的证据。

        ``approve`` 的唯一释放条件是学生点名一个已知动作，但真正卡住的学生
        恰恰是说不出动作的那个人——他越说"我不知道从哪看起"，系统给得越少。
        这条通路专门接住这种情况。

        风险由三重约束兜住：只放前置为空的入口级证据、每轮至多一条、
        且 Go 侧用自己持有的 stalled_turns 独立复核阈值，不信任模型自报的
        ``is_stuck``。这里返回空串表示没有可兜底的证据。
        """
        available = set(collected_evidence)
        for node in world.evidence_graph:
            if node.evidence_id in available or node.prerequisites:
                continue
            return node.evidence_id
        return ""
