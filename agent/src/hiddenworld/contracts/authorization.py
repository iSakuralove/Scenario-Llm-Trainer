"""用户动作授权契约。

Observation Tool 只能执行学生明确提出、或学生通过结构化动作点击授权的检查。
ScenarioAgent 可以建议下一步，但不能仅凭自己的规划读取新的场景事实。
"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field


AuthorizationSource = Literal["user_message", "structured_user_action"]
InvestigationIntent = Literal["trace_request_latency", "discover_request_latency"]
InvestigationSubjectType = Literal["request", "request_collection"]
AllowedFollowupPolicy = Literal["none", "declared_chain"]


class InvestigationScope(BaseModel):
    """Runtime 为一次明确调查范围签发的有限补查授权。"""

    model_config = ConfigDict(extra="forbid")

    scope_id: str
    source: AuthorizationSource
    intent: InvestigationIntent
    subject_type: InvestigationSubjectType
    subject_id: str
    entry_action_ids: list[str] = Field(default_factory=list)
    allowed_action_ids: list[str] = Field(default_factory=list)
    max_depth: int = Field(default=0, ge=0)
    max_tool_calls: int = Field(default=0, ge=0)
    parameter_bindings: dict[str, str] = Field(default_factory=dict)
    expires_at_turn: int = Field(default=0, ge=0)
    allowed_followup_policy: AllowedFollowupPolicy = "none"
    dependency_map: dict[str, list[str]] = Field(default_factory=dict)


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
