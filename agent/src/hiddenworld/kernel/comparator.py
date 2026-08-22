"""把当前轮服务端签发的答案尝试与 HiddenWorld 做确定性比较。"""

from __future__ import annotations

import re
from collections.abc import Sequence

from hiddenworld.contracts import (
    AnswerAttempt,
    HiddenWorld,
    HypothesisRelation,
    InternalAnswerComparison,
    LearnerState,
)

from .antiguess import AntiGuess
from .verifier import RootCauseVerifier

_POINT_SEPARATOR = re.compile(r"[，。；;！？!?\n]+")
_ALIGNMENT_BY_RELATION: dict[HypothesisRelation, float] = {
    "target": 1.0,
    "contributing": 0.75,
    "unknown": 0.25,
    "ruled_out": 0.0,
    "unrelated": 0.0,
}


class AnswerComparator:
    """返回完整内部比较；调用方只能通过 ``to_public`` 向下投影。"""

    def compare(
        self,
        world: HiddenWorld,
        *,
        learner_state: LearnerState,
        attempt: AnswerAttempt,
        hypothesis_id: str,
        contradictions: Sequence[str],
    ) -> InternalAnswerComparison:
        # 以服务端绑定的答案原文作为裁判输入；模型输出的 hypothesis_id
        # 仅为旧 v1 兼容字段，不能覆盖 CanonicalAnswer 的连接结果。
        canonical_answer = world.canonical_answer
        relation = RootCauseVerifier().relation(
            world,
            hypothesis_id=hypothesis_id,
            learner_state=learner_state,
            # v2 有权威答案时忽略模型 id；旧 v1 世界没有该字段时保留
            # hypothesis-id 兼容行为，避免历史回放改变结果。
            answer_text=attempt.text if canonical_answer is not None else "",
        )
        anti_guess = AntiGuess().evaluate(
            world,
            collected_evidence=learner_state.collected_evidence,
            relation=relation,
            canonical_answer=canonical_answer,
        )
        requirement_sources = [
            canonical_answer.solution_requirements
            if canonical_answer is not None
            else world.root_cause.solution_requirements,
            world.solution_rubric.required_actions,
            world.solution_rubric.verification_steps,
        ]
        requirements: list[str] = []
        for source in requirement_sources:
            for item in source:
                text = str(item).strip()
                if text and text not in requirements:
                    requirements.append(text)
        matched_requirements = [item for item in requirements if item and item in attempt.text]
        solution_coverage = len(matched_requirements) / len(requirements) if requirements else 1.0

        # 证据链闭合只是“可以进入收束检查”的必要条件，不代表学生已经
        # 说明了修复动作和验证闭环。最终答案必须同时覆盖题目声明的解决
        # 要求，否则不能把一个完整因果链直接当作可完成答案。
        completion_allowed = anti_guess.completion_allowed and solution_coverage >= 1.0

        return InternalAnswerComparison(
            answer_attempt_id=attempt.answer_attempt_id,
            relation=relation,
            claim_alignment=_ALIGNMENT_BY_RELATION[relation],
            evidence_coverage=anti_guess.coverage,
            best_evidence_set=anti_guess.best_evidence_set,
            missing_evidence=anti_guess.missing_evidence,
            contradictions=list(contradictions),
            solution_coverage=solution_coverage,
            missing_solution_requirements=[
                item for item in requirements if item not in matched_requirements
            ],
            completion_allowed=completion_allowed,
            user_points=_extract_user_points(attempt.text),
        )


def _extract_user_points(text: str) -> list[str]:
    return [point.strip() for point in _POINT_SEPARATOR.split(text) if point.strip()]
