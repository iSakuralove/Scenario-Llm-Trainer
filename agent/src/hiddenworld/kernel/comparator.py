"""把当前轮服务端签发的答案尝试与 HiddenWorld 做确定性比较。"""

from __future__ import annotations

import re
from collections.abc import Sequence
from typing import Literal

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

SolutionBehavior = Literal[
    "gateway_timeout",
    "database_lock_tail",
    "idempotency",
    "index_action",
    "index_validation",
    "cache_key",
    "cache_validation",
    "temp_storage",
    "eviction_monitoring",
    "slow_query",
    "verification",
    "generic",
]

_VERIFICATION_MARKERS = (
    "观察",
    "验证",
    "确认",
    "检查",
    "监控",
    "复核",
    "回归",
    "复跑",
    "压测",
    "测试",
    "校验",
    "复查",
    "observe",
    "verify",
    "confirm",
    "check",
    "monitor",
)
_ACTION_MARKERS = (
    "调整",
    "恢复",
    "治理",
    "处理",
    "保证",
    "实现",
    "增加",
    "补充",
    "重建",
    "设置",
    "回滚",
    "优化",
    "修复",
    "降低",
    "清理",
    "缩短",
    "减少",
    "隔离",
    "纳入",
    "加进",
    "加入",
    "前置",
    "排查",
    "配置",
    "建立",
    "添加",
    "调成",
    "改成",
    "确保",
    "做到",
    "需要",
    "fix",
    "set",
    "change",
    "ensure",
    "implement",
    "add",
)
_NEGATION_MARKERS = (
    "不要",
    "无需",
    "不需要",
    "不应",
    "不能",
    "无法",
    "未",
    "没有",
    "不是",
    "尚未",
    "仍未",
    "never",
    "not",
    "no",
)
_NEGATIVE_RESULT_MARKERS = (
    "未回落",
    "没有回落",
    "仍然升高",
    "仍升高",
    "仍然很高",
    "仍很高",
    "仍存在",
    "依旧存在",
    "未恢复",
    "没有恢复",
    "未消失",
    "没有消失",
    "尚未恢复",
    "尚未消失",
    "not recovered",
    "still high",
)


def _normalize_solution_text(text: str) -> str:
    """为内部行为断言做轻量归一化，不改变学生原话或公开投影。"""

    normalized = str(text).casefold().strip()
    normalized = normalized.replace("response_timeout", "timeout")
    normalized = normalized.replace("response-timeout", "timeout")
    normalized = normalized.replace("response timeout", "timeout")
    normalized = normalized.replace("responsetimeout", "timeout")
    normalized = normalized.replace("database", "db")
    normalized = normalized.replace("lock wait", "lock_wait")
    normalized = normalized.replace("lock-wait", "lock_wait")
    normalized = normalized.replace("lock contention", "lock_contention")
    normalized = normalized.replace("eviction threshold", "eviction_threshold")
    normalized = normalized.replace("evictionthreshold", "eviction_threshold")
    normalized = normalized.replace("tenant id", "tenant_id")
    normalized = normalized.replace("tenantid", "tenant_id")
    normalized = normalized.replace("cache key", "cache_key")
    normalized = normalized.replace("cachekey", "cache_key")
    normalized = normalized.replace("ephemeral storage", "临时存储")
    normalized = normalized.replace("ephemeralstorage", "临时存储")
    normalized = normalized.replace("execution plan", "explain")
    normalized = normalized.replace("executionplan", "explain")
    return re.sub(r"\s+", "", normalized)


def _contains_any(text: str, markers: Sequence[str]) -> bool:
    for marker in markers:
        value = marker.casefold()
        if not value:
            continue
        # 英文别名使用边界匹配，避免 lock 命中 unlock、set 命中 reset、
        # check 命中 checksum；中文短语仍使用子串匹配。
        if re.fullmatch(r"[a-z0-9_]+", value):
            if re.search(rf"(?<![a-z0-9_]){re.escape(value)}(?![a-z0-9_])", text):
                return True
        elif value in text:
            return True
    return False


def _clauses(text: str) -> list[str]:
    return [item.strip() for item in re.split(r"[，。；;！？!?\n]+", text) if item.strip()]


def _clause_has_negation(clause: str, targets: Sequence[str]) -> bool:
    for target in targets:
        value = target.casefold()
        if not value:
            continue
        start = clause.find(value)
        if start < 0:
            continue
        prefix = clause[max(0, start - 10):start]
        if _contains_any(prefix, _NEGATION_MARKERS):
            return True
    return _contains_any(clause, _NEGATIVE_RESULT_MARKERS)


def _matches_action(answer: str, targets: Sequence[str]) -> bool:
    for clause in _clauses(answer):
        if not _contains_any(clause, targets):
            continue
        if _clause_has_negation(clause, targets):
            continue
        if _contains_any(clause, _ACTION_MARKERS):
            return True
    return False


def _requirement_is_verification(requirement: str) -> bool:
    value = _normalize_solution_text(requirement)
    return _contains_any(value, _VERIFICATION_MARKERS) and not _contains_any(
        value,
        ("加进", "纳入", "检查清单", "前置", "配置", "增加", "补充"),
    )


def _classify_solution_requirement(requirement: str) -> SolutionBehavior:
    """把题库要求归到稳定的行为类别，未知要求保留精确匹配语义。"""

    value = _normalize_solution_text(requirement)
    if not value:
        return "generic"
    if _contains_any(value, ("tenant_id", "租户", "cache_key", "缓存键", "键隔离")):
        return "cache_validation" if _requirement_is_verification(value) else "cache_key"
    if _contains_any(value, ("临时存储", "临时盘", "临时文件", "中间文件", "日志目录", "ephemeral", "emptydir")):
        return "eviction_monitoring" if _contains_any(value, ("eviction", "驱逐", "threshold", "使用率", "使用量", "监控")) else "temp_storage"
    if _contains_any(value, ("慢查询", "slow_query", "slowquery")):
        return "slow_query"
    if _contains_any(value, ("explain", "索引校验", "索引", "index", "检查清单")):
        return "index_validation" if _contains_any(value, ("校验", "确认", "复跑", "explain", "验证", "检查清单", "前置")) else "index_action"
    if _contains_any(value, ("幂等", "idempot", "重复回调", "重复处理", "去重")):
        return "idempotency"
    if _contains_any(value, ("锁等待", "锁竞争", "lock_wait", "lock_contention", "lock", "长尾", "tail_latency")):
        return "database_lock_tail"
    if _contains_any(value, ("response_timeout", "timeout", "超时")):
        return "gateway_timeout"
    if _contains_any(value, ("确认", "观察", "验证", "检查", "监控", "复核", "回归", "复跑", "测试", "校验", "observe", "verify", "confirm")):
        return "verification"
    return "generic"


def _matches_requirement(requirement: str, answer: str) -> bool:
    """以行为语义匹配修复要求，同时保留未知题库的精确兜底。"""

    requirement_text = str(requirement).strip()
    answer_text = str(answer).strip()
    if not requirement_text or not answer_text:
        return False
    requirement_norm = _normalize_solution_text(requirement_text)
    answer_norm = _normalize_solution_text(answer_text)
    behavior = _classify_solution_requirement(requirement_text)
    target_terms = _behavior_targets(behavior)
    if requirement_norm in answer_norm and not _clause_has_negation(answer_norm, target_terms or (requirement_norm,)):
        return True

    if behavior == "gateway_timeout":
        return _matches_verification_requirement(requirement_norm, answer_norm) if _requirement_is_verification(requirement_text) else _matches_action(answer_norm, target_terms)
    if behavior == "database_lock_tail":
        return _matches_verification_requirement(requirement_norm, answer_norm) if _requirement_is_verification(requirement_text) else _matches_action(answer_norm, target_terms)
    if behavior == "idempotency":
        return _matches_verification_requirement(requirement_norm, answer_norm) if _requirement_is_verification(requirement_text) else _matches_action(answer_norm, target_terms)
    if behavior == "index_action":
        return _matches_action(answer_norm, target_terms)
    if behavior == "index_validation":
        return _matches_action(answer_norm, target_terms) or _matches_verification_requirement(requirement_norm, answer_norm)
    if behavior == "cache_key":
        return _matches_action(answer_norm, target_terms)
    if behavior == "cache_validation":
        return _matches_verification_requirement(requirement_norm, answer_norm)
    if behavior == "temp_storage":
        return _matches_action(answer_norm, target_terms)
    if behavior == "eviction_monitoring":
        return _matches_action(answer_norm, target_terms) or _matches_verification_requirement(requirement_norm, answer_norm)
    if behavior == "slow_query":
        return _matches_action(answer_norm, target_terms) or _matches_verification_requirement(requirement_norm, answer_norm)
    if behavior == "verification":
        return _matches_verification_requirement(requirement_norm, answer_norm)
    return False


def _behavior_targets(behavior: SolutionBehavior) -> tuple[str, ...]:
    return {
        "gateway_timeout": ("timeout", "超时", "504"),
        "database_lock_tail": ("锁等待", "锁竞争", "lock_wait", "lock_contention", "锁", "长尾", "尾延迟", "tail_latency"),
        "idempotency": ("幂等", "idempot", "重复回调", "重复处理", "去重", "唯一键"),
        "index_action": ("索引", "index"),
        "index_validation": ("索引", "index", "explain", "发布前", "检查清单"),
        "cache_key": ("tenant_id", "租户", "cache_key", "缓存键", "键隔离"),
        "cache_validation": ("tenant_id", "租户", "cache_key", "缓存键", "键隔离", "串数据"),
        "temp_storage": ("临时存储", "临时盘", "临时文件", "ephemeral", "emptydir"),
        "eviction_monitoring": ("eviction", "驱逐", "threshold", "使用率", "使用量", "中间文件", "日志目录", "临时盘", "临时存储"),
        "slow_query": ("慢查询", "slow_query", "slowquery"),
        "verification": (),
        "generic": (),
    }.get(behavior, ())


def _matches_verification_requirement(requirement: str, answer: str) -> bool:
    clauses = _clauses(answer)
    if not any(_contains_any(clause, _VERIFICATION_MARKERS) for clause in clauses):
        return False

    groups: list[tuple[str, ...]] = []
    if _contains_any(requirement, ("网关", "gateway", "vip")):
        groups.append(("网关", "gateway", "vip"))
    if _contains_any(requirement, ("504", "超时", "timeout")):
        groups.append(("504", "超时", "timeout"))
    if _contains_any(requirement, ("重试", "retry")):
        groups.append(("重试", "retry"))
    if _contains_any(requirement, ("p99", "延迟", "latency")):
        groups.append(("p99", "延迟", "latency"))
    if _contains_any(requirement, ("锁等待", "lock_wait", "长尾", "tail_latency")):
        groups.append(("锁等待", "lock_wait", "长尾", "tail_latency"))
    if _contains_any(requirement, ("幂等", "idempot", "重复回调", "重复处理")):
        groups.append(("幂等", "idempot", "重复回调", "重复处理", "去重", "唯一键"))
    if _contains_any(requirement, ("业务结果", "订单", "支付", "结果")):
        groups.append(("业务结果", "订单", "支付", "结果"))
    if _contains_any(requirement, ("索引", "index", "explain", "执行计划")):
        groups.append(("索引", "index", "explain", "执行计划"))
    if _contains_any(requirement, ("慢查询", "slow_query", "slowquery")):
        groups.append(("慢查询", "slow_query", "slowquery"))
    if _contains_any(requirement, ("租户", "tenant_id", "cache_key", "缓存键", "键隔离", "串数据")):
        groups.append(("租户", "tenant_id", "cache_key", "缓存键", "键隔离", "串数据"))
    if _contains_any(requirement, ("驱逐", "eviction", "threshold", "使用率", "使用量", "中间文件", "日志目录", "临时盘", "临时存储")):
        groups.append(("驱逐", "eviction", "threshold", "使用率", "使用量", "中间文件", "日志目录", "临时盘", "临时存储"))
    if not groups:
        return requirement in answer and not _clause_has_negation(answer, (requirement,))
    for group in groups:
        if not any(_contains_any(clause, group) and not _clause_has_negation(clause, group) for clause in clauses):
            return False
    return True


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
        matched_requirements = [item for item in requirements if _matches_requirement(item, attempt.text)]
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
