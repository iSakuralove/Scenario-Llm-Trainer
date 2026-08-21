"""hiddenworld.v1 契约。

导出聚合。安全边界的可执行清单也放在这里：PUBLIC_MODELS 与 FORBIDDEN_PUBLIC_FIELDS
被契约测试用来递归扫描每一个公开 DTO 的 JSON Schema，确保秘密字段不会因为
某次"顺手加个字段"而漏出去。
"""

from __future__ import annotations

from .answer import CanonicalAnswer, InternalAnswerComparison, PublicAnswerComparison, SupportStatus
from .agent_context import (
    ActionCatalogEntry,
    AgentBudgetView,
    AgentContext,
    AgentToolResult,
    AgentTurnControlView,
)
from .authorization import AuthorizedActionRef, StructuredUserAction, UserActionAuthorization
from .debug_trace import DebugTraceEvent
from .dimensions import TeachingDimensionCategory, TeachingDimensionRef
from .model_output import AgentModelOutput, AgentOutputEnvelope, FinalReplyOutput, ToolCall, ToolCallsOutput
from .validator import ScenarioContractValidationError, ScenarioContractValidator, validate_scenario_contract
from .deps import GuardContext, InterpreterDeps, MentorDeps
from .events import (
    PublicObservation,
    PublicReasoningSummary,
    PublicTraceEvent,
    ReasoningStage,
    RunEvent,
    RunEventKind,
    RunEventStatus,
    ToolEventPayload,
)
from .learner import LearnerState, LearnerStateView, Turn
from .mentor import ExpectedEffort, MentorAction
from .teaching import ConstraintFacts, TeachingConstraints
from .transport import (
    AgentTurnRequest,
    AgentTurnResult,
    AuditTrace,
    Budget,
    ContractVersionMismatch,
    Proposal,
    ProposalKind,
    VerificationResult,
)
from .turn import (
    HYPOTHESIS_OTHER,
    LOW_CONFIDENCE_THRESHOLD,
    AnswerAttempt,
    TurnAnalysis,
)
from .version import (
    CONTRACT_VERSION,
    AllowedDirection,
    EvidenceCategory,
    HypothesisRelation,
    MustNotConstraint,
    StudentAffect,
    direction_for_category,
)
from .world import (
    EvidenceNode,
    HiddenWorld,
    Hypothesis,
    MisconceptionRule,
    Observation,
    PublicScenario,
    RootCause,
    SolutionRubric,
    VirtualTool,
)

# 会被学生的浏览器看到的每一个类型。契约测试逐个扫描它们的 schema。
PUBLIC_MODELS = (
    PublicAnswerComparison,
    PublicObservation,
    PublicReasoningSummary,
    PublicTraceEvent,
    RunEvent,
    ToolEventPayload,
    PublicScenario,
)

# 会进入 Mentor prompt 的类型。与 PUBLIC_MODELS 是两套边界，不能混为一谈：
# TeachingConstraints.may_release 是一串 evidence id，对 Mentor 合法（它要据此
# 决定请求释放什么），但绝不能出现在发给浏览器的事件里。
MENTOR_VISIBLE_MODELS = (
    PublicScenario,
    LearnerStateView,
    TeachingConstraints,
    ConstraintFacts,
    PublicAnswerComparison,
)

# 这些字段名一旦出现在公开 DTO 的 schema 里，就是一次泄露。
#
# 清单按"泄露什么"分组，改动时请连同理由一起改：
#   - 直接答案：root_cause / description(RootCause) / accepted_hypotheses
#   - 对错状态：correct / is_correct / target / completion_allowed / claim_alignment
#   - 结构信息：is_distractor / depth / prerequisites / sufficient_evidence_sets
#   - 内部裁判：relation / coverage / matched_* / missing_* / similarity
#   - 模型内部：reasoning_content / chain_of_thought / rationale
FORBIDDEN_PUBLIC_FIELDS = frozenset(
    {
        "root_cause",
        "accepted_hypotheses",
        "solution_requirements",
        "correct",
        "is_correct",
        "target",
        "completion_allowed",
        "claim_alignment",
        "is_distractor",
        "depth",
        "prerequisites",
        "sufficient_evidence_sets",
        "misconception_rules",
        "why_wrong",
        "relation",
        "coverage",
        "evidence_coverage_ratio",
        "matched_hypotheses",
        "matched_evidence",
        "best_evidence_set",
        "missing_evidence",
        "missing_points",
        "missing_solution_requirements",
        "similarity",
        "reasoning_content",
        "chain_of_thought",
        "rationale",
        "internal_verification",
        "internal_audit",
        "hidden_world",
        "forbidden_entities",
    }
)

__all__ = [
    "CONTRACT_VERSION",
    "FORBIDDEN_PUBLIC_FIELDS",
    "HYPOTHESIS_OTHER",
    "LOW_CONFIDENCE_THRESHOLD",
    "MENTOR_VISIBLE_MODELS",
    "PUBLIC_MODELS",
    "AgentTurnRequest",
    "AgentTurnResult",
    "AgentModelOutput",
    "AgentOutputEnvelope",
    "ActionCatalogEntry",
    "AgentBudgetView",
    "AgentContext",
    "AgentToolResult",
    "AgentTurnControlView",
    "AuthorizedActionRef",
    "StructuredUserAction",
    "AllowedDirection",
    "AnswerAttempt",
    "CanonicalAnswer",
    "AuditTrace",
    "Budget",
    "ConstraintFacts",
    "ContractVersionMismatch",
    "DebugTraceEvent",
    "EvidenceCategory",
    "EvidenceNode",
    "ExpectedEffort",
    "FinalReplyOutput",
    "GuardContext",
    "HiddenWorld",
    "Hypothesis",
    "HypothesisRelation",
    "InternalAnswerComparison",
    "InterpreterDeps",
    "LearnerState",
    "LearnerStateView",
    "MentorAction",
    "MentorDeps",
    "MisconceptionRule",
    "MustNotConstraint",
    "Observation",
    "VirtualTool",
    "Proposal",
    "ProposalKind",
    "PublicAnswerComparison",
    "PublicObservation",
    "PublicReasoningSummary",
    "PublicScenario",
    "PublicTraceEvent",
    "ReasoningStage",
    "RootCause",
    "RunEvent",
    "RunEventKind",
    "RunEventStatus",
    "SolutionRubric",
    "StudentAffect",
    "SupportStatus",
    "TeachingConstraints",
    "TeachingDimensionCategory",
    "TeachingDimensionRef",
    "ToolCall",
    "ToolCallsOutput",
    "ToolEventPayload",
    "Turn",
    "TurnAnalysis",
    "VerificationResult",
    "UserActionAuthorization",
    "ScenarioContractValidationError",
    "ScenarioContractValidator",
    "validate_scenario_contract",
    "direction_for_category",
]
