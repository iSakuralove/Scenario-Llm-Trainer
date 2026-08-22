"""把学生连接到的假设与 CanonicalAnswer 做确定性关系判定。"""

from __future__ import annotations

import re

from hiddenworld.contracts import (
    HYPOTHESIS_OTHER,
    HiddenWorld,
    HypothesisRelation,
    LearnerState,
)
from hiddenworld.contracts.answer import CanonicalAnswer


_NON_WORD = re.compile(r"[^\w\u3400-\u9fff]+", re.UNICODE)
_NEGATION_MARKERS = ("不是", "并非", "不太像", "不应是", "排除", "排除了", "排除掉", "不会是")


class RootCauseVerifier:
    """只返回关系，不生成任何用户可见文本。

    当调用方提供答案原文时，模型自报的 ``hypothesis_id`` 不参与裁判；
    这条路径使用题目持久化的 CanonicalAnswer 和公开假设标签做保守连接。
    不带原文的旧 v1 调用仍保留 id 兼容路径。
    """

    def relation(
        self,
        world: HiddenWorld,
        *,
        hypothesis_id: str = "",
        learner_state: LearnerState,
        answer_text: str = "",
        canonical_answer: CanonicalAnswer | None = None,
    ) -> HypothesisRelation:
        if answer_text:
            return self._relation_from_answer_text(
                world,
                learner_state=learner_state,
                answer_text=answer_text,
                canonical_answer=canonical_answer or world.canonical_answer,
            )

        if not hypothesis_id or hypothesis_id == HYPOTHESIS_OTHER:
            return "unknown"
        if hypothesis_id in world.root_cause.accepted_hypotheses:
            return "target"
        if hypothesis_id in learner_state.ruled_out_hypotheses:
            return "ruled_out"
        if hypothesis_id in world.hypothesis_ids():
            return "unrelated"
        return "unknown"

    def _relation_from_answer_text(
        self,
        world: HiddenWorld,
        *,
        learner_state: LearnerState,
        answer_text: str,
        canonical_answer: CanonicalAnswer | None,
    ) -> HypothesisRelation:
        """从服务端绑定的原文做保守、确定性的答案连接。"""

        if not canonical_answer:
            return "unknown"
        # CanonicalAnswer 与 RootCause 不一致时 fail closed；题库加载层应更早
        # 报错，但裁判不能在不一致快照上给出 target。
        if canonical_answer.root_cause_id != world.root_cause.id:
            return "unknown"

        normalized_text = _normalize(answer_text)
        if not normalized_text:
            return "unknown"

        canonical_phrases = [
            canonical_answer.canonical_conclusion,
            *canonical_answer.accepted_equivalents,
        ]
        if any(_phrase_is_asserted(normalized_text, phrase) for phrase in canonical_phrases):
            return "target"

        structured_relation = _structured_answer_relation(
            normalized_text,
            canonical_answer,
        )
        if structured_relation is not None:
            if structured_relation == "target":
                return "target"
            # 有结构化答案时，单个已接受假设只能代表因果链的一部分；
            # 不能再把“只说数据库锁”与完整事故结论视为等价。
            partial_structured_relation: HypothesisRelation = "contributing"
        else:
            partial_structured_relation = None

        matched: list[str] = []
        for hypothesis in world.hypotheses:
            label = _normalize(hypothesis.label)
            if label and label in normalized_text and not _is_negated(normalized_text, label):
                matched.append(hypothesis.hypothesis_id)
        if not matched or len(set(matched)) != 1:
            return "unknown"

        matched_id = matched[0]
        if matched_id in learner_state.ruled_out_hypotheses:
            return "ruled_out"
        if matched_id in world.root_cause.accepted_hypotheses:
            return partial_structured_relation or "target"
        if matched_id in world.hypothesis_ids():
            return "unrelated"
        return "unknown"


def _normalize(value: str) -> str:
    return _NON_WORD.sub("", value.casefold())


def _phrase_is_asserted(normalized_text: str, phrase: str) -> bool:
    normalized_phrase = _normalize(phrase)
    return bool(normalized_phrase) and normalized_phrase in normalized_text and not _is_negated(
        normalized_text, normalized_phrase
    )


def _structured_answer_relation(
    normalized_text: str,
    answer: CanonicalAnswer,
) -> HypothesisRelation | None:
    """按答案维度区分完整因果链与单一贡献因素。"""

    has_structured_answer = bool(
        answer.direct_trigger.strip()
        or answer.latent_issues
        or answer.phenomenon.strip()
        or answer.derived_risks
    )
    if not has_structured_answer:
        return None

    checks: list[bool] = []
    if answer.direct_trigger.strip():
        checks.append(_phrase_is_asserted(normalized_text, answer.direct_trigger))
    latent_issues = [item for item in answer.latent_issues if item.strip()]
    if latent_issues:
        checks.append(
            any(_phrase_is_asserted(normalized_text, item) for item in latent_issues)
        )
    if answer.phenomenon.strip():
        checks.append(_phrase_is_asserted(normalized_text, answer.phenomenon))
    derived_risks = [item for item in answer.derived_risks if item.strip()]
    if derived_risks:
        checks.append(
            any(_phrase_is_asserted(normalized_text, item) for item in derived_risks)
        )
    if checks and all(checks):
        return "target"
    if any(checks):
        return "contributing"
    return "unknown"


def _is_negated(normalized_text: str, phrase: str) -> bool:
    start = normalized_text.find(phrase)
    if start <= 0:
        return False
    prefix = normalized_text[max(0, start - 8) : start]
    return any(marker in prefix for marker in _NEGATION_MARKERS)
