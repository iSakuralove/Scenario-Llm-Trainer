"""Mentor 的输出契约。

Mentor 是唯一与学生对话的组件，也是唯一看不到答案的 LLM。
"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from .version import ReplyMode

# 取代"每轮只问一个问题"。
#
# 初版把问题个数写成硬约束（8 处 + 3 条验收标准），是最典型的按部就班。
# 但即使降为软约束，"问题个数"仍是错的度量——一个需要综合四条信息的问题，
# 比三个快速确认重得多。真正该控制的是**认知负荷**。
ExpectedEffort = Literal["quick", "moderate", "heavy"]


class MentorAction(BaseModel):
    """Mentor 一轮的完整产出。

    schema 保持扁平、非 strict：与 TurnAnalysis 同理，共同基线只有
    JSON Object + Pydantic 校验。
    """

    model_config = ConfigDict(extra="forbid")

    # rationale 必须排在 reply 前面。模型按 schema 顺序生成 JSON——放在 reply
    # 后面时它就成了事后追认：正文已经说完了，这里写什么都不影响输出，模型自然
    # 敷衍成「用户需要信息」这类空话。排在前面才是真正的先想后说。
    #
    # 代价是正文开始流出的时间变晚，这段空白由 TurnAnalysis.public_summary 的
    # 流式推理摘要填补。
    rationale: str = Field(
        description=(
            "先写这里再写 reply。你这一轮为什么这么做：学生现在卡在哪、"
            "你打算用什么方式接住他、你考虑过但否决了哪些做法、否决的理由是什么。\n"
            "写完整的话，不要写标签或短语。这段不会给学生看，写给你自己想清楚用。\n"
            "刻意不设成枚举：一旦这里是 teaching_state 之类的固定选项，"
            "你就会开始「为了填对枚举而思考」，分类查表会从后门溜回来。"
        ),
    )
    reply: str = Field(description="给学生看的正文。这是全系统唯一会被学生读到的自然语言。")
    reply_mode: ReplyMode | None = Field(
        default=None,
        description="这段正文的行为模式；必须与本轮工具状态一致，不能把失败动作写成已得到观察。",
    )
    requested_releases: list[str] = Field(
        description="你希望本轮释放的 evidence id。必须是 may_release 的子集，越权会被拒绝并要求重写。",
    )
    confirms_hypothesis: bool = Field(
        description="你这段回复是否确认了学生的假设成立。如实自报——系统会独立核对，不一致会被打回。",
    )
    expected_effort: ExpectedEffort = Field(
        description="你这轮给学生施加的认知负荷。不是问题个数，是想清楚它需要多少力气。",
    )
