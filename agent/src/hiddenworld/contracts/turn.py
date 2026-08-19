"""TurnInterpreter 的输出契约，以及服务端签发的 AnswerAttempt。

TurnAnalysis 是 PromptedOutput 的目标类型，schema 必须保持**简单、扁平、非 strict**：
两个 provider（DeepSeek / Z.AI）的 Profile 都只声明 JSON Object，未声明 Native
JSON Schema，所以共同基线只能是"模型吐 JSON + Pydantic 校验"。为此这里全部使用
字符串、布尔、数字、字符串数组和固定枚举，不用递归、复杂联合和动态键。

所有字段都是 required（不给 default）：可选字段会让模型依赖 provider 的默认值行为，
而那部分行为在两家之间并不等价。
"""

from __future__ import annotations

from pydantic import BaseModel, ConfigDict, Field

from .version import StudentAffect

# 候选表之外的假设。真实学生一定会提出出题人没想到的东西；
# 若强行往已有 ID 上靠，"缓存穿透"会被塞成"Redis 问题"，后续全错。
HYPOTHESIS_OTHER = "H_OTHER"

# 低于此置信度时只收窄状态变更，不阻断整轮（设计 §11.4）。
LOW_CONFIDENCE_THRESHOLD = 0.45


class TurnAnalysis(BaseModel):
    """把当前这条用户消息翻译成动作和假设连接。

    TurnInterpreter **不生成用户回复**，也**读不到任何正确性标记**——
    它拿到的假设表是一份未标注的多选一。
    """

    model_config = ConfigDict(extra="forbid")

    # 声明顺序即生成顺序：模型按 schema 顺序吐 JSON，而 StreamingFieldExtractor
    # 是顺序扫描的。这个字段放末尾等于不流式——学生要等整个 analysis 生成完
    # 才看到第一个字。它必须是第一个。
    public_summary: str = Field(
        description=(
            "用第二人称向学生复述你从这条消息里读到了什么，一到两句自然中文。\n"
            "只能使用学生自己的措辞和公开题面里已有的信息；不得引入学生没有提到的"
            "组件、数值、日志字段，也不得给出任何结论或下一步建议。\n"
            "这是学生唯一会看到的解析过程，先写它再做后面的判断。"
        ),
    )
    actions: list[str] = Field(
        description='学生本轮想做的观察动作，如 ["inspect:metrics.cpu"]。没有就给空数组。',
    )
    hypothesis_id: str = Field(
        description=(
            f'学生本轮指向的假设 id。无法连接时给空字符串；'
            f'提出了候选表之外的想法时给 "{HYPOTHESIS_OTHER}"。'
        ),
    )
    hypothesis_raw: str = Field(
        description=f'当 hypothesis_id 为 "{HYPOTHESIS_OTHER}" 时，原样透传学生的说法；否则给空字符串。',
    )
    made_claim: bool = Field(description="学生这轮是否提出了一个断言，而不只是在提问")
    contains_answer_attempt: bool = Field(description="这条消息是否包含对根因的完整回答")
    answer_attempt_text: str = Field(
        description="仅当 contains_answer_attempt 为真时，给出学生表述的完整答案；否则给空字符串。",
    )
    established_facts: list[str] = Field(
        description="学生这轮自己确立或复述的事实。用他自己的话，不要补充他没说的内容。",
    )
    is_stuck: bool = Field(
        description=(
            "学生是否卡住了——不知道从哪下手、看不懂现象、明确求助。\n"
            "注意这与垃圾输入完全不同：「不知道该从哪看起」是卡住，不是噪声，"
            "而且这恰恰是最需要帮助的时刻。"
        ),
    )
    is_noise: bool = Field(
        description="是否是与排查无关的乱敲、灌水或纯闲聊。判为噪声时世界不响应，但不惩罚、不终止会话。",
    )
    student_affect: StudentAffect = Field(description="学生当前的状态，用于决定 Mentor 的语气")
    confidence: float = Field(
        ge=0.0,
        le=1.0,
        description="你对这次解析的把握。不确定时给低分，系统会相应收窄状态变更而不是阻断。",
    )

    def is_low_confidence(self) -> bool:
        return self.confidence < LOW_CONFIDENCE_THRESHOLD


class AnswerAttempt(BaseModel):
    """一次答案尝试。**由服务端签发，不是模型输出。**

    compare_answer 只接受这里的 answer_attempt_id。Agent 不能把答案正文、
    根因或任意自造字符串传给工具——否则它可以构造候选答案去枚举标准答案，
    把辅导工具变成答案探测器。
    """

    model_config = ConfigDict(extra="forbid")

    answer_attempt_id: str = Field(description="服务端生成，绑定当前轮")
    session_id: str
    turn_id: str
    revision: int
    text: str = Field(description="学生的答案原文，取自 TurnAnalysis.answer_attempt_text")
