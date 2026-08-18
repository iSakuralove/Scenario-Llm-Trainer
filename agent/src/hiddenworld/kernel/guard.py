"""Mentor 终值的确定性安全校验。"""

from __future__ import annotations

import re
import unicodedata
from collections.abc import Collection

from hiddenworld.contracts import GuardContext, HiddenWorld, MentorAction, TeachingConstraints

_HAN = re.compile(r"[\u3400-\u4dbf\u4e00-\u9fff]")
_IDENTIFIER = re.compile(r"(?<![A-Za-z0-9_])([A-Za-z_][A-Za-z0-9_.:/-]{2,})(?![A-Za-z0-9_])")
_NUMBER = re.compile(r"(?<!\d)(\d+(?:[.,]\d+)*)(?!\d)")
_CHINESE_COMPONENT = re.compile(r"[\u3400-\u4dbf\u4e00-\u9fff]{1,8}(?:表|服务|接口|主库|从库|索引|字段)")


class GuardViolation(ValueError):
    """不携带命中实体的固定错误，供 output validator 转成安全重试。"""

    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


class Guard:
    """校验 MentorAction；失败时由 Agent output validator 转成 ModelRetry。"""

    def validate(
        self,
        action: MentorAction,
        *,
        constraints: TeachingConstraints,
        context: GuardContext,
    ) -> MentorAction:
        if any(_contains_entity(action.reply, entity) for entity in context.forbidden_entities):
            raise GuardViolation("entity_leak", "回复包含尚未公开的信息，请只依据已公开观察重写。")
        confirmation_forbidden = any(
            item == "confirm_hypothesis" for item in constraints.must_not
        )
        if action.confirms_hypothesis and (confirmation_forbidden or not context.completion_allowed):
            raise GuardViolation(
                "premature_confirmation",
                "当前证据尚不足以确认结论，请改为引导学生继续验证。",
            )
        approved_releases = set(context.may_release).intersection(constraints.may_release)
        if any(item not in approved_releases for item in action.requested_releases):
            raise GuardViolation(
                "release_not_approved",
                "回复请求了尚未获批的信息，请只使用本轮允许释放的内容。",
            )
        return action


def extract_forbidden_entities(
    world: HiddenWorld,
    *,
    released_evidence: Collection[str],
) -> list[str]:
    """从根因与未释放证据提取 Guard 需要比对的最小实体。"""

    released = set(released_evidence)
    sources = [world.root_cause.description]
    sources.extend(node.content for node in world.evidence_graph if node.evidence_id not in released)

    entities = [world.root_cause.id, world.root_cause.component]
    for source in sources:
        entities.extend(_sensitive_tokens(source))
    return _unique_non_empty(entities)


def _sensitive_tokens(text: str) -> list[str]:
    tokens: list[str] = []
    for match in _IDENTIFIER.finditer(text):
        token = match.group(1)
        if any(marker in token for marker in ("_", ".", "/", ":")) or any(
            character.isdigit() for character in token
        ):
            tokens.append(token)
    tokens.extend(match.group(1) for match in _NUMBER.finditer(text))
    tokens.extend(match.group(0) for match in _CHINESE_COMPONENT.finditer(text))
    return tokens


def _unique_non_empty(values: list[str]) -> list[str]:
    result: list[str] = []
    seen: set[str] = set()
    for value in values:
        normalized = value.strip()
        if not normalized or normalized in seen:
            continue
        result.append(normalized)
        seen.add(normalized)
    return result


def _contains_entity(text: str, entity: str) -> bool:
    normalized_entity = unicodedata.normalize("NFKC", entity).strip().casefold()
    if not normalized_entity:
        return False
    normalized_text = unicodedata.normalize("NFKC", text).casefold()
    if _HAN.search(normalized_entity):
        normalized_entity = re.sub(r"\s+", "", normalized_entity)
        normalized_text = re.sub(r"\s+", "", normalized_text)
        return any(
            token == normalized_entity
            for token in _dictionary_tokens(normalized_text, [normalized_entity])
        )
    pattern = re.compile(
        rf"(?<![A-Za-z0-9_]){re.escape(normalized_entity)}(?![A-Za-z0-9_])",
        re.IGNORECASE,
    )
    return pattern.search(normalized_text) is not None


def _dictionary_tokens(text: str, dictionary: list[str]) -> list[str]:
    """用禁止实体表做中文最大匹配；英文实体另走词边界。"""

    terms = sorted({term for term in dictionary if term}, key=len, reverse=True)
    tokens: list[str] = []
    index = 0
    while index < len(text):
        matched = next((term for term in terms if text.startswith(term, index)), None)
        if matched is not None:
            tokens.append(matched)
            index += len(matched)
            continue
        index += 1
    return tokens
