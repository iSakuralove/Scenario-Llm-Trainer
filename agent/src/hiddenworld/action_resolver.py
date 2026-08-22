"""用户动作目录与自然语言授权的共享解析器。

动作是否能执行只能由 Runtime 决定，但“用户刚才说的对象”与题目动作目录
必须使用同一套匹配规则。否则 UI/Agent 展示了一个名称，用户原样输入时却会
在另一套字符串规则里变成 ``user_action_required``。
"""

from __future__ import annotations

import re
import unicodedata
from collections.abc import Iterable


LEGACY_ACTION_ALIASES: dict[str, tuple[str, ...]] = {
    "inspect:database.order_write": (
        "订单库日志",
        "订单库写入日志",
        "看订单库日志",
    ),
}

# 这里只用于“用户明确要求查看某个公开对象”时的授权候选解析，
# 不参与 chat / hypothesis / stuck 等 TurnIntent 判断。意图仍由 ScenarioAgent
# 通过 TurnAssessment 给出，Runtime 只负责把候选绑定到题目声明的唯一动作。
_OBSERVATION_CUES = (
    "看",
    "查看",
    "查询",
    "查一下",
    "查下",
    "检查",
    "调取",
    "发给我",
    "日志",
    "配置差异",
    "发布记录",
    "路由差异",
    "慢查询",
    "写入日志",
)

_SEMANTIC_STOP_BIGRAMS = {
    "一下",
    "一下",
    "可以",
    "帮我",
    "帮你",
    "请看",
    "看一",
    "查看",
    "查询",
    "检查",
    "发给",
    "给我",
    "有什",
    "什么",
    "显示",
    "是否",
    "的是",
    "以及",
    "前后",
}

_RELATION_CUES = (
    "对比",
    "比较",
    "同一个",
    "同一",
    "时间线",
    "完成时间",
    "耗时",
)


def normalize_user_text(value: str) -> str:
    """规范化中英文空白与大小写，保留语义字符。"""

    normalized = unicodedata.normalize("NFKC", value or "").casefold().strip()
    return re.sub(r"\s+", "", normalized)


def explicit_action_terms(item) -> tuple[str, ...]:
    """返回题目声明的、允许作为用户动作入口的公开短语。"""

    values = [getattr(item, "target", ""), *getattr(item, "aliases", ()), *getattr(item, "query_patterns", ())]
    result: list[str] = []
    seen: set[str] = set()
    for value in values:
        value = str(value or "").strip()
        key = normalize_user_text(value)
        if not key or key in seen:
            continue
        seen.add(key)
        result.append(value)
    return tuple(result)


def legacy_action_aliases(action: str) -> tuple[str, ...]:
    return LEGACY_ACTION_ALIASES.get(action, ())


def resolve_declared_items(text: str, items: Iterable, *, action_attr: str) -> list[str]:
    """按题目声明的公开短语解析动作，返回去重后的稳定 action id。"""

    normalized_text = normalize_user_text(text)
    if not normalized_text:
        return []
    matches: list[str] = []
    seen: set[str] = set()
    for item in items:
        action = str(getattr(item, action_attr, "") or "").strip()
        if not action or action in seen:
            continue
        if any(normalize_user_text(term) in normalized_text for term in explicit_action_terms(item)):
            matches.append(action)
            seen.add(action)
    return matches


def resolve_user_requested_actions(text: str, items: Iterable, *, action_attr: str) -> list[str]:
    """解析用户明确要求查看的公开动作，返回唯一的候选动作。

    第一层仍然使用题目声明的精确 target/alias/query pattern。对于自然语言
    中常见的“网关切换前后的配置差异”这类不逐字复述目录标题的表达，第二层
    只做保守的短语重叠：必须包含观察语气，且最佳候选至少命中两个有区分度的
    中文双字短语；并列候选不放行。它不是意图识别，也不会把“数据库有异常吗”
    这类问题自动当成查询动作。
    """

    exact = resolve_declared_items(text, items, action_attr=action_attr)
    if exact:
        return exact

    normalized_text = normalize_user_text(text)
    if not normalized_text or not any(cue in normalized_text for cue in _OBSERVATION_CUES):
        return []

    # 复合排查请求优先于单个模糊候选：例如“对比 Gateway 和 Nginx 的
    # 完成时间”同时明确点名两个公开对象，不能被“网关”相关的单个配置
    # 动作抢走。只有两个以上题目声明的实体词命中时才放行。
    related = _resolve_related_entity_actions(normalized_text, items, action_attr)
    if len(related) >= 2:
        return related

    scored: list[tuple[str, int]] = []
    seen: set[str] = set()
    for item in items:
        action = str(getattr(item, action_attr, "") or "").strip()
        if not action or action in seen:
            continue
        seen.add(action)
        score = 0
        for term in explicit_action_terms(item):
            normalized_term = normalize_user_text(term)
            if not normalized_term:
                continue
            if normalized_term in normalized_text:
                score = max(score, 100 + len(normalized_term))
                continue
            shared = _meaningful_bigram_overlap(normalized_text, normalized_term)
            # “前后/变更/对比/差异”表达的是比较请求；在多个网关动作都
            # 命中“网关”时，只有声明了差异、路由或后端池的动作才可成为
            # 唯一候选。这个加权仍只做动作绑定，不替代模型的意图判断。
            contrastive = any(token in normalized_text for token in ("前后", "变更", "对比", "差异"))
            comparison_target = any(token in normalized_term for token in ("差异", "路由", "后端池", "对比"))
            if contrastive and comparison_target:
                shared += 2
            score = max(score, shared)
        if score >= 2:
            scored.append((action, score))

    if not scored:
        # 复合排查请求经常只写出多个系统名，而不是逐字复述题目目录，
        # 例如“对比 Gateway 和 Nginx 的完成时间”。这种情况只在明确的
        # 关联/对比语气下放行，并且只匹配 target/alias 中声明的拉丁实体词；
        # 不把 query_pattern 里的 request_id、SELECT 等实现字段当成动作名，
        # 避免把一个请求扩展成无关工具枚举。
        if not any(cue in normalized_text for cue in _RELATION_CUES):
            return []
        return []
    best = max(score for _, score in scored)
    winners = [action for action, score in scored if score == best]
    return winners if len(winners) == 1 else []


def _entity_tokens(value: str) -> set[str]:
    """提取题目声明中的稳定拉丁实体词，用于复合观察的保守绑定。"""

    return {
        token
        for token in re.findall(r"[a-z][a-z0-9_-]{2,}", value.casefold())
        if token not in {"select", "show", "from", "where", "status"}
    }


def _resolve_related_entity_actions(text: str, items: Iterable, action_attr: str) -> list[str]:
    if not any(cue in text for cue in _RELATION_CUES):
        return []
    text_tokens = _entity_tokens(text)
    related: list[str] = []
    seen: set[str] = set()
    for item in items:
        action = str(getattr(item, action_attr, "") or "").strip()
        if not action or action in seen:
            continue
        declared_tokens = _entity_tokens(
            normalize_user_text(
                " ".join(
                    [
                        str(getattr(item, "target", "") or ""),
                        *[str(value or "") for value in getattr(item, "aliases", ())],
                    ]
                )
            )
        )
        if text_tokens.intersection(declared_tokens):
            related.append(action)
            seen.add(action)
    return related


def _meaningful_bigram_overlap(left: str, right: str) -> int:
    """计算用于动作绑定的中文短语重叠，不做通用语义分类。"""

    def bigrams(value: str) -> set[str]:
        result: set[str] = set()
        for index in range(len(value) - 1):
            pair = value[index : index + 2]
            if pair in _SEMANTIC_STOP_BIGRAMS or not any("\u4e00" <= char <= "\u9fff" for char in pair):
                continue
            result.add(pair)
        return result

    return len(bigrams(left).intersection(bigrams(right)))


def resolve_legacy_observation_action(text: str, observations: Iterable) -> list[str]:
    """旧题目没有 virtual_tools 时的保守兼容解析。"""

    normalized_text = normalize_user_text(text)
    if not normalized_text:
        return []
    matches: list[str] = []
    seen: set[str] = set()
    for observation in observations:
        action = str(getattr(observation, "action", "") or "").strip()
        if not action or action in seen:
            continue
        tokens = [part for part in re.split(r"[:.]", action.casefold()) if part and part != "inspect"]
        if any(normalize_user_text(token) in normalized_text for token in tokens):
            matches.append(action)
            seen.add(action)
    return matches
