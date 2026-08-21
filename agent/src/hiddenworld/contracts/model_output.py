"""单 Agent 模型输出的严格判别联合。"""

from __future__ import annotations

from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field


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
