"""共享测试夹具。

这里构造的 HiddenWorld 是一个**测试用最小世界**，不是题库内容。
固定题库在阶段 1 单独落盘，两者不要互相引用：题库内容会随出题迭代变化，
而契约测试需要一个永远不变的形状。
"""

from __future__ import annotations

import os
import sys
from pathlib import Path

import pytest

# 单元测试默认禁止任何真实模型请求。live 分组的用例会在自己的 fixture 里显式放开。
os.environ.setdefault("HIDDENWORLD_ALLOW_MODEL_REQUESTS", "0")

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from hiddenworld.contracts import (  # noqa: E402
    ConstraintFacts,
    EvidenceNode,
    HiddenWorld,
    Hypothesis,
    LearnerState,
    MisconceptionRule,
    Observation,
    PublicScenario,
    RootCause,
    SolutionRubric,
    TeachingConstraints,
)


@pytest.fixture
def public_scenario() -> PublicScenario:
    return PublicScenario(
        title="订单查询接口在下午突然变慢",
        description="订单列表接口 P99 从 120ms 涨到 4s，错误率没有上升。",
        environment="Spring Boot 服务 + MySQL 8 主从 + Redis 缓存",
        initial_symptoms=["接口变慢", "错误率正常", "下午两点后开始"],
    )


@pytest.fixture
def hidden_world() -> HiddenWorld:
    """一个索引失效的世界。两条充分证据路径，一个阴性观察。"""
    return HiddenWorld(
        root_cause=RootCause(
            id="RC_INDEX_DROPPED",
            category="data",
            component="orders 表",
            description="上午的发布脚本重建 orders 表时漏掉了 idx_user_created 索引，导致列表查询全表扫描。",
            sufficient_evidence_sets=[
                ["E_SLOW_SQL", "E_EXPLAIN_FULLSCAN"],
                ["E_RELEASE_LOG", "E_DDL_DIFF"],
            ],
            accepted_hypotheses=["H_INDEX"],
            solution_requirements=["重建 idx_user_created 索引", "补充上线前的索引校验"],
        ),
        hypotheses=[
            Hypothesis(hypothesis_id="H_INDEX", label="索引问题"),
            Hypothesis(hypothesis_id="H_CPU_BOUND", label="CPU 打满"),
            Hypothesis(hypothesis_id="H_POOL", label="连接池打满"),
            Hypothesis(hypothesis_id="H_CACHE", label="缓存失效"),
            Hypothesis(hypothesis_id="H_OTHER", label="其他"),
        ],
        evidence_graph=[
            EvidenceNode(
                evidence_id="E_SLOW_SQL",
                content="慢查询日志里 SELECT * FROM orders WHERE user_id=? 平均 3.8s",
                category="logs",
                obtained_by=["inspect:logs.slow_query"],
            ),
            EvidenceNode(
                evidence_id="E_EXPLAIN_FULLSCAN",
                content="EXPLAIN 显示 type=ALL，rows_examined 约 240 万",
                category="data",
                prerequisites=["E_SLOW_SQL"],
                obtained_by=["inspect:data.explain"],
            ),
            EvidenceNode(
                evidence_id="E_RELEASE_LOG",
                content="上午 10:12 有一次 orders 表结构变更发布",
                category="change",
                obtained_by=["inspect:change.release_log"],
            ),
            EvidenceNode(
                evidence_id="E_DDL_DIFF",
                content="发布脚本重建表时未包含 idx_user_created",
                category="change",
                prerequisites=["E_RELEASE_LOG"],
                obtained_by=["inspect:change.ddl_diff"],
            ),
            EvidenceNode(
                evidence_id="E_CPU_NORMAL",
                content="数据库 CPU 使用率 35%，无异常",
                category="metrics",
                obtained_by=["inspect:metrics.cpu"],
            ),
            EvidenceNode(
                evidence_id="E_POOL_NORMAL",
                content="连接池活跃连接 12/50，等待队列为空",
                category="resource",
                obtained_by=["inspect:resource.pool"],
            ),
        ],
        observations=[
            Observation(
                action="inspect:logs.slow_query",
                result="慢查询日志里 SELECT * FROM orders WHERE user_id=? 平均 3.8s，占比 92%。",
                is_negative=False,
                yields_evidence=["E_SLOW_SQL"],
            ),
            Observation(
                action="inspect:data.explain",
                result="type=ALL，key 为 NULL，rows 约 2,400,000。",
                is_negative=False,
                yields_evidence=["E_EXPLAIN_FULLSCAN"],
            ),
            Observation(
                action="inspect:change.release_log",
                result="上午 10:12 有一次针对 orders 表的结构变更发布。",
                is_negative=False,
                yields_evidence=["E_RELEASE_LOG"],
            ),
            Observation(
                action="inspect:change.ddl_diff",
                result="对比建表语句，重建后的 orders 表少了 idx_user_created。",
                is_negative=False,
                yields_evidence=["E_DDL_DIFF"],
            ),
            Observation(
                action="inspect:metrics.cpu",
                result="数据库 CPU 使用率 35%，内存和 IO 都在正常区间。",
                is_negative=True,
                yields_evidence=["E_CPU_NORMAL"],
                rules_out=["H_CPU_BOUND"],
            ),
            Observation(
                action="inspect:resource.pool",
                result="连接池活跃连接 12/50，等待队列为空。",
                is_negative=True,
                yields_evidence=["E_POOL_NORMAL"],
                rules_out=["H_POOL"],
            ),
        ],
        solution_rubric=SolutionRubric(
            required_actions=["重建 idx_user_created 索引"],
            verification_steps=["重建后复跑 EXPLAIN 确认走索引", "观察 P99 回落"],
            rollback_notes=["索引重建期间注意锁表窗口"],
        ),
        misconception_rules=[
            MisconceptionRule(
                misconception_id="M_BLAME_CACHE",
                pattern_hypotheses=["H_CACHE"],
                why_wrong="缓存命中率并未下降，慢的是回源查询本身。",
            ),
        ],
    )


@pytest.fixture
def learner_state() -> LearnerState:
    return LearnerState()


@pytest.fixture
def teaching_constraints() -> TeachingConstraints:
    return TeachingConstraints(
        must_not=["confirm_hypothesis", "reveal_unreleased"],
        may_release=[],
        allowed_direction=None,
        facts=ConstraintFacts(
            hypothesis_supported=False,
            evidence_coverage="0/2",
            stalled_turns=0,
            contradictions=[],
            student_affect="engaged",
        ),
    )
