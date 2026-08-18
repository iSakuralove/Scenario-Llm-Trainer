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
