"""Mentor 终值的确定性安全校验。"""

from __future__ import annotations

import re
import unicodedata
from collections.abc import Collection

from hiddenworld.contracts import (
    GuardContext,
    HiddenWorld,
    MentorAction,
    PublicScenario,
    TeachingConstraints,
)

_HAN = re.compile(r"[\u3400-\u4dbf\u4e00-\u9fff]")
_IDENTIFIER = re.compile(r"(?<![A-Za-z0-9_])([A-Za-z_][A-Za-z0-9_.:/-]{2,})(?![A-Za-z0-9_])")
_NUMBER = re.compile(r"(?<!\d)(\d+(?:[.,]\d+)*)(?!\d)")
_NUMBER_ENTITY = re.compile(r"\d+(?:[.,]\d+)*")
_CHINESE_COMPONENT = re.compile(r"[\u3400-\u4dbf\u4e00-\u9fff]{1,8}(?:表|服务|接口|主库|从库|索引|字段)")
_EXPLICIT_GUIDANCE_MARKERS = (
    "下一步",
    "接下来",
    "建议检查",
    "建议查看",
    "建议核对",
    "建议从",
    "可以从",
    "可从",
    "可以先",
    "先检查",
    "先查看",
    "首先检查",
    "首先查看",
    "开始排查",
    "继续排查",
    "进一步排查",
    "继续排除",
    "排除范围",
    "排除性观察",
)
_EXPLICIT_CONCLUSION_MARKERS = (
    "问题不在",
    "根因在",
    "根因是",
    "原因是",
    "问题来自",
    "已经定位",
    "可以确定",
)


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
        if any(contains_forbidden_entity(action.reply, entity) for entity in context.forbidden_entities):
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
        if context.evidence_request is not None and context.evidence_request.availability == "UNAVAILABLE":
            internal_framing = (
                "工具目录",
                "工具清单",
                "系统目录",
                "系统实现",
                "权限",
                "内部工具",
                "环境不提供",
                "系统不提供",
                "工具不支持",
                "无法获取",
                "没有这个指标",
            )
            if any(marker in action.reply for marker in internal_framing):
                raise GuardViolation(
                    "unavailable_evidence_internal_framing",
                    "不可用证据必须按题目模拟数据边界表达，不能暴露内部工具或权限实现。",
                )
        reply = action.reply.strip()
        if any(marker in reply for marker in (*_EXPLICIT_GUIDANCE_MARKERS, *_EXPLICIT_CONCLUSION_MARKERS)):
            raise GuardViolation(
                "reply_policy_violation",
                "回复包含明确排查路径或未获证据支持的结论，请由模型重新生成自然回复。",
            )
        for observation in context.public_observation_texts:
            normalized_observation = " ".join((observation or "").split())
            if len(normalized_observation) >= 8 and normalized_observation in " ".join(reply.split()):
                raise GuardViolation(
                    "reply_repeats_observation",
                    "回复重复了已公开观察的原文，请由模型改为自然承接。",
                )
        return action


def extract_forbidden_entities(
    world: HiddenWorld,
    *,
    released_evidence_ids: Collection[str],
    public_scenario: PublicScenario | None = None,
) -> list[str]:
    """从根因与未释放证据提取 Guard 需要比对的最小实体。

    ``released_evidence_ids`` 始终是服务端维护的 evidence id 集合，不接受证据
    正文。题面里已经公开的实体必须从禁词集合移除，否则 Mentor 复述公开事实时
    会被误判为泄露。
    """

    released = set(released_evidence_ids)
    sources = [world.root_cause.description]
    sources.extend(node.content for node in world.evidence_graph if node.evidence_id not in released)

    entities = [world.root_cause.id, world.root_cause.component]
    for source in sources:
        entities.extend(_sensitive_tokens(source))
    public_entities: set[str] = set()
    public_text = "\n".join(_public_scenario_sources(public_scenario)) if public_scenario else ""
    if public_scenario is not None:
        for source in _public_scenario_sources(public_scenario):
            public_entities.update(_entity_key(token) for token in _sensitive_tokens(source))
    return _unique_non_empty(
        [
            entity
            for entity in entities
            if _entity_key(entity) not in public_entities
            and not contains_forbidden_entity(public_text, entity)
        ]
    )


def _public_scenario_sources(public_scenario: PublicScenario) -> list[str]:
    return [
        public_scenario.title,
        public_scenario.description,
        public_scenario.environment,
        *public_scenario.initial_symptoms,
        public_scenario.architecture_diagram,
    ]


def _sensitive_tokens(text: str) -> list[str]:
    tokens: list[str] = []
    for match in _IDENTIFIER.finditer(text):
        token = match.group(1)
        if any(marker in token for marker in ("_", ".", "/", ":")) or any(
            character.isdigit() for character in token
        ):
            tokens.append(token)
    tokens.extend(
        match.group(1) for match in _NUMBER.finditer(text) if _is_distinctive_number(match.group(1))
    )
    tokens.extend(match.group(0) for match in _CHINESE_COMPONENT.finditer(text))
    return tokens


def _is_distinctive_number(token: str) -> bool:
    """裸的一两位整数不作为禁词。

    实测固定题库 4 道题里有 3 道把 ``8`` / ``10`` / ``12`` / ``35`` / ``45`` / ``90``
    这类数字列进了禁词表（见 tools/audit_forbidden_entities.py）。它们几乎不携带
    识别信息，却会让 Mentor 连"10 分钟""看这 3 个方向"都写不出来——Guard 判 entity_leak
    触发重试，回复只会越改越空泛。

    带小数点或千分位的数字（``3.8`` / ``2,400,000`` / 版本号 ``3.4``）以及三位以上的
    整数仍然是禁词：那些才是真正指向隐藏内容的具体取值。

    注意：Go 侧 scenarioIsDistinctiveNumber 必须与此保持一致。Go 更严会导致
    Python Guard 放行、Go validateScenarioReply 拒绝，整轮以 reply_guard_rejected 失败。
    """
    if "." in token or "," in token:
        return True
    return len(token) >= 3


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


def contains_forbidden_entity(text: str, entity: str) -> bool:
    """按生产 Guard 的中文词典、英文词边界和数字边界检查实体。"""

    normalized_entity = _entity_key(entity)
    if not normalized_entity:
        return False
    normalized_text = unicodedata.normalize("NFKC", text).casefold()
    if _NUMBER_ENTITY.fullmatch(normalized_entity):
        pattern = re.compile(rf"(?<!\d){re.escape(normalized_entity)}(?!\d)")
        return pattern.search(normalized_text) is not None
    if _HAN.search(normalized_entity):
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


def _entity_key(value: str) -> str:
    normalized = unicodedata.normalize("NFKC", value).strip().casefold()
    if _HAN.search(normalized):
        return re.sub(r"\s+", "", normalized)
    return normalized


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
