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
    "可以看看",
    "可看看",
    "看看前面",
    "查看前面",
    "可以先",
    "先从",
    "入手",
    "哪里开始",
    "先检查",
    "先查看",
    "首先检查",
    "首先查看",
    "开始排查",
    "继续排查",
    "进一步排查",
    "继续排除",
    "稍后再试",
    "可以稍后",
    "继续梳理",
    "继续分析",
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
_INTERNAL_GUIDANCE_MARKERS = (
    "引导关注",
    "建议关注",
    "请关注",
    "已确认用户意图",
    "已识别用户意图",
    "确认用户意图",
)
_OBSERVATION_RECORDING_MARKERS = (
    "已记录观察",
    "已记录你的观察",
    "记录了观察",
    "观察已记录",
)
_POSITIVE_ACTION_RECORD_RE = re.compile(
    r"(?:已|已经|好的[，, ]*)[^。！？!?；;]{0,24}"
    r"(?:记录|记下|保存|确认|收到)[^。！？!?；;]{0,24}"
    r"(?:意图|请求|动作|观察|检查|查询)"
)
_POSITIVE_OBSERVATION_ACK_RE = re.compile(
    r"(?:已|已经|好的[，, ]*)[^。！？!?；;]{0,24}"
    r"(?:得到|拿到|完成|返回|查到)[^。！？!?；;]{0,24}"
    r"(?:观察|结果|指标|日志|数据)"
)
_IMPLICIT_CONCLUSION_RE = re.compile(
    r"(?:可能|大概率|更可能|提示|说明)[^。！？!?；;]{0,24}(?:不在|出现在|来自|位于)"
)
_IMPLICIT_GUIDANCE_RE = re.compile(
    r"(?:下一步|接下来|进一步|继续|还有哪些|哪些环节|从哪里|如何|怎么)[^。！？!?；;]{0,24}"
    r"(?:验证|排查|查看|检查|确认|核对|观察|入手)"
)
_SYSTEM_CONFIRMATION_RE = re.compile(
    r"(?:已|已经|刚才|目前|我们已经)[^。！？!?；;]{0,18}"
    r"(?:确认|核实|验证|检查完成|查看完成|查明|定位)[^。！？!?；;]{0,28}"
)
_SCOPE_EXCLUSION_RE = re.compile(
    r"(?:这一段|这部分|这一层|这一块|订单落库|数据库(?:这一段|层面)?|入口层|服务层|该环节)"
    r"[^。！？!?；;]{0,18}(?:看起来|基本|整体上)?"
    r"(?:正常|没什么异常|没有什么异常|没有问题|没有异常|未见异常|无异常|没异常)"
)
_REMAINING_SCOPE_RE = re.compile(
    r"(?:剩下的|其余的|其他的|其它的)[^。！？!?；;]{0,12}"
    r"(?:链路|环节|方向|部分)"
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
                "可用工具",
                "工具中",
                "监控工具",
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
        if any(marker in reply for marker in (*_INTERNAL_GUIDANCE_MARKERS, *_OBSERVATION_RECORDING_MARKERS)):
            raise GuardViolation(
                "reply_internal_framing",
                "回复不能暴露内部引导状态或把观察写成系统记录动作，请由模型重新生成。",
            )
        # 当 Runtime 已确定本轮没有形成公开观察时，不能把动作、意图或
        # 查询结果写成“已记录/已完成”。这不是依赖某一句固定文案，而是
        # 对公开事实状态与回复中的正向行为声明做一致性校验。
        if context.required_reply_mode == "no_observation" and (
            _POSITIVE_ACTION_RECORD_RE.search(reply) is not None
            or _POSITIVE_OBSERVATION_ACK_RE.search(reply) is not None
        ):
            raise GuardViolation(
                "reply_claims_observation_without_result",
                "本轮没有形成公开观察，回复不能声称已记录动作、已完成检查或已得到结果。",
            )
        if (
            any(marker in reply for marker in (*_EXPLICIT_GUIDANCE_MARKERS, *_EXPLICIT_CONCLUSION_MARKERS))
            or _IMPLICIT_CONCLUSION_RE.search(reply) is not None
            or _IMPLICIT_GUIDANCE_RE.search(reply) is not None
            or _SYSTEM_CONFIRMATION_RE.search(reply) is not None
            or _SCOPE_EXCLUSION_RE.search(reply) is not None
            or _REMAINING_SCOPE_RE.search(reply) is not None
        ):
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
            if _reply_repeats_observation(reply, observation):
                raise GuardViolation(
                    "reply_repeats_observation",
                    "回复复述了已公开观察的主要内容，请只做自然承接或反思。",
                )
        return action


def _reply_repeats_observation(reply: str, observation: str) -> bool:
    """检测工具卡片已经展示后，回复是否又密集改写同一事实。

    这不是用关键词决定 Agent 意图，而是对两个已经确定的公开文本做表面
    重复度闸门。只在观察和回复都足够长、且共享多个连续三字片段时触发，
    避免阻止一句自然的「这条观察说明什么？」。
    """

    reply_chars = _replay_chars(reply)
    observation_chars = _replay_chars(observation)
    if len(reply_chars) < 18 or len(observation_chars) < 24:
        return False
    reply_shingles = {"".join(reply_chars[index : index + 3]) for index in range(len(reply_chars) - 2)}
    observation_shingles = {
        "".join(observation_chars[index : index + 3])
        for index in range(len(observation_chars) - 2)
    }
    shared = reply_shingles.intersection(observation_shingles)
    observation_coverage = len(shared) / max(len(observation_shingles), 1)
    reply_coverage = len(shared) / max(len(reply_shingles), 1)
    return (
        (len(shared) >= 5 and reply_coverage >= 0.18)
        or (len(shared) >= 10 and observation_coverage >= 0.12)
    )


def _replay_chars(value: str) -> list[str]:
    normalized = unicodedata.normalize("NFKC", value or "").casefold()
    return [char for char in normalized if char.isalnum() or "\u3400" <= char <= "\u9fff"]


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
