"""hiddenworld.v1 契约的版本常量与跨模块共享枚举。

单独成文件是为了避免 contracts 包内部的循环导入：world / teaching / transport
都要引用这里的枚举，但它们之间不应互相依赖。
"""

from __future__ import annotations

from typing import Final, Literal

# Go ↔ Python 之间每一次请求和响应都必须携带并校验它。
# 版本不匹配时整轮拒绝，不做字段级兼容猜测——猜错的代价是状态和对话脱节。
CONTRACT_VERSION: Final[str] = "hiddenworld.v1"

# 证据节点的七个维度。沿用 Go 侧 diagnosticFocus() 的划分：
# 那套划分本身是合理的，出问题的是用关键词去匹配它（审查报告 H1）。
# 这里只迁移分类法，不迁移匹配方式——归类由出题时确定，不在运行时猜。
EvidenceCategory = Literal[
    "logs",
    "metrics",
    "config",
    "change",
    "dependency",
    "data",
    "resource",
]

# TeachingPolicy 允许告诉 Mentor 的方向，粒度被当作安全参数审计。
#
# 方向本身就是信息：第 2 轮学生还没提数据库，系统就给出 data_inspection，
# 等于替他砍掉了大半搜索空间。这是可接受的教学信号，但**只能取这一层粗类别**，
# 永远不得下钻到节点级描述——"一个关于连接资源上限的观察"这种句子，
# 模型看到就能直接问出"最大连接数是多少"。
AllowedDirection = Literal[
    "log_inspection",
    "metric_observation",
    "config_review",
    "change_history",
    "dependency_chain",
    "data_inspection",
    "resource_check",
]

# category → direction 的固定映射。一一对应，不做启发式推断。
_CATEGORY_TO_DIRECTION: Final[dict[str, str]] = {
    "logs": "log_inspection",
    "metrics": "metric_observation",
    "config": "config_review",
    "change": "change_history",
    "dependency": "dependency_chain",
    "data": "data_inspection",
    "resource": "resource_check",
}


def direction_for_category(category: str) -> AllowedDirection | None:
    """把证据类别翻译成允许暴露给 Mentor 的粗粒度方向。

    未知类别返回 None 而不是兜底到某个方向：宁可这一轮不给方向，
    也不能因为映射表漏了一项就随便指一个地方。
    """
    return _CATEGORY_TO_DIRECTION.get(category)  # type: ignore[return-value]


# RootCauseVerifier 对"学生提出的假设与真相是什么关系"的判定。
#
# unknown 与 unrelated 必须分开：候选表之外的假设（H_OTHER）返回 unknown，
# 表示"我们没法判断"；unrelated 表示"确定无关"。混为一谈会让 Mentor
# 去纠一个可能并没错的偏——真实学生一定会提出出题人没想到的东西。
HypothesisRelation = Literal[
    "target",
    "contributing",
    "ruled_out",
    "unrelated",
    "unknown",
]

# 学生当前的情绪状态，决定 Mentor 的语气。
# 统一施加"闯关/解锁/副本"的语气本身就是机械感的来源：
# 卡了三轮开始烦躁的学生，面对还在喊"快解锁啦"的 Agent 只会更烦。
StudentAffect = Literal["engaged", "confused", "frustrated", "disengaged"]

# TeachingConstraints.must_not 的取值域。由 Guard 强制执行，不是给模型的建议。
MustNotConstraint = Literal[
    "confirm_hypothesis",
    "reveal_unreleased",
    "start_debrief",
]
