"""用户动作授权契约。

Observation Tool 只能执行学生明确提出、或学生通过结构化动作点击授权的检查。
ScenarioAgent 可以建议下一步，但不能仅凭自己的规划读取新的场景事实。
"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field


AuthorizationSource = Literal["user_message", "structured_user_action"]


class UserActionAuthorization(BaseModel):
    """Runtime 签发的一次观察授权，不携带隐藏事实。"""

    model_config = ConfigDict(extra="forbid")

    source: AuthorizationSource
    action_ref: str
    tool_kind: str
    normalized_scope: str = ""
    state_revision: int
    authorization_id: str = Field(description="稳定、可审计的授权 ID")


class AuthorizedActionRef(BaseModel):
    """给 ScenarioAgent 的安全授权投影。"""

    model_config = ConfigDict(extra="forbid")

    authorization_id: str
    action_ref: str
    tool_kind: str
    normalized_scope: str = ""

    @classmethod
    def from_authorization(cls, value: UserActionAuthorization) -> "AuthorizedActionRef":
        return cls(
            authorization_id=value.authorization_id,
            action_ref=value.action_ref,
            tool_kind=value.tool_kind,
            normalized_scope=value.normalized_scope,
        )


class StructuredUserAction(BaseModel):
    """前端 QuickAction 点击产生的一等用户动作。"""

    model_config = ConfigDict(extra="forbid")

    action_id: str
    catalog_version: str
    state_revision: int
    normalized_scope: str = ""
