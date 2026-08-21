"""非生产调试通道的独立事件协议。"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict


class DebugTraceEvent(BaseModel):
    model_config = ConfigDict(extra="forbid")

    request_id: str
    sequence: int
    kind: Literal["debug_trace_delta", "debug_trace_completed"]
    text: str = ""
