"""单 Agent 模型输出的严格判别联合。"""

from __future__ import annotations

from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator


class ToolCall(BaseModel):
    model_config = ConfigDict(extra="forbid")

    call_id: str
    tool_id: str
    arguments: dict[str, str] = Field(default_factory=dict)


class ToolCallsOutput(BaseModel):
    model_config = ConfigDict(extra="forbid")

    kind: Literal["tool_calls"]
    public_summary: str = Field(min_length=1)
    calls: list[ToolCall] = Field(min_length=1)


class FinalReplyOutput(BaseModel):
    model_config = ConfigDict(extra="forbid")

    kind: Literal["final_reply"]
    public_summary: str | None = None
    reply: str = Field(min_length=1)


AgentModelOutput = Annotated[ToolCallsOutput | FinalReplyOutput, Field(discriminator="kind")]


class AgentOutputEnvelope(BaseModel):
    """PydanticAI 结构化输出适配层。

    PydanticAI 2.31 的 prompted output 对 Annotated discriminated union 在不同
    provider 上的 text/tool 模式并不一致，因此模型层先接收一个扁平 envelope，
    再由 ``to_contract`` 收敛为严格的 AgentModelOutput。业务代码不能直接使用
    envelope 绕过二选一校验。
    """

    # 兼容不同 GLM/OpenAI 兼容端点偶尔附带的 reasoning/usage 字段；
    # 这些字段会在 envelope 解析时丢弃，最终仍只能通过 to_contract() 进入
    # 严格的 AgentModelOutput，不能扩大业务输出面。
    model_config = ConfigDict(extra="ignore")

    kind: Literal["tool_calls", "final_reply"]
    # reply 放在联合字段前面：纯 final_reply 的 JSON 流可以尽早进入正文，
    # 不必先等待 calls/public_summary 的可选字段全部生成。
    reply: str | None = None
    public_summary: str | None = None
    calls: list[ToolCall] = Field(default_factory=list)

    @model_validator(mode="after")
    def validate_shape(self) -> "AgentOutputEnvelope":
        if self.kind == "tool_calls":
            if not self.public_summary or not self.public_summary.strip() or not self.calls or self.reply is not None:
                raise ValueError("tool_calls requires public_summary and calls, without reply")
        elif self.reply is None or not self.reply.strip() or self.calls:
            raise ValueError("final_reply requires reply and cannot include calls")
        return self

    def to_contract(self) -> AgentModelOutput:
        if self.kind == "tool_calls":
            return ToolCallsOutput(
                kind="tool_calls",
                public_summary=self.public_summary or "",
                calls=self.calls,
            )
        return FinalReplyOutput(
            kind="final_reply",
            public_summary=self.public_summary,
            reply=self.reply or "",
        )
