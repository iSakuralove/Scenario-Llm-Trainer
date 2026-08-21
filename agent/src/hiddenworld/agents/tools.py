"""一期唯一业务工具 ``compare_answer`` 的类型化封装。"""

from __future__ import annotations

from dataclasses import dataclass, field

from pydantic_ai import RunContext, Tool

from hiddenworld.contracts import (
    AnswerAttempt,
    HiddenWorld,
    InternalAnswerComparison,
    LearnerState,
    PublicAnswerComparison,
    TurnAnalysis,
)
from hiddenworld.kernel.comparator import AnswerComparator


class CompareAnswerAuthorizationError(ValueError):
    """attempt 未由服务端绑定到当前轮，禁止执行。"""


@dataclass
class CompareAnswerRuntime:
    """服务端创建的一次工具执行上下文；同一实例最多真正比较一次。"""

    request_id: str
    session_id: str
    turn_id: str
    revision: int
    world: HiddenWorld
    learner_state: LearnerState
    analysis: TurnAnalysis
    attempts: dict[str, AnswerAttempt]
    # V2 必须由 Runtime 从 AgentTurnRequest.user_message 传入。默认空值只
    # 保留旧 v1 trace 的兼容执行；一旦提供，就逐字校验 AnswerAttempt。
    user_message: str = ""
    contradictions: list[str] = field(default_factory=list)
    execution_count: int = field(default=0, init=False)
    internal_result: InternalAnswerComparison | None = field(default=None, init=False)
    _public_result: PublicAnswerComparison | None = field(default=None, init=False, repr=False)

    def execute(self, answer_attempt_id: str) -> PublicAnswerComparison:
        attempt = self.attempts.get(answer_attempt_id)
        if attempt is None or not self._is_bound_to_current_turn(attempt):
            raise CompareAnswerAuthorizationError("answer attempt is not bound to the current turn")
        if self.analysis.is_low_confidence():
            raise CompareAnswerAuthorizationError("low-confidence analysis cannot enter answer comparison")
        if not self.analysis.contains_answer_attempt:
            raise CompareAnswerAuthorizationError("current turn does not authorize answer comparison")
        if self.user_message:
            if not attempt.matches_user_message(self.user_message):
                raise CompareAnswerAuthorizationError("answer attempt is not bound to request.user_message")
        elif attempt.source_user_message:
            # 当旧调用方未传 user_message 时，仍不允许模型分析文本覆盖服务端
            # 已绑定的原文；只接受分析对同一原文的确认。
            if attempt.text != self.analysis.answer_attempt_text:
                raise CompareAnswerAuthorizationError("analysis does not match bound answer attempt")
        elif attempt.text != self.analysis.answer_attempt_text:
            # v1 compatibility path. 新 Runtime 应始终传 user_message，走上面
            # 的严格分支；保留该分支只是为了读取旧会话和旧单元测试。
            raise CompareAnswerAuthorizationError("current turn does not authorize answer comparison")
        if self._public_result is not None:
            return self._public_result.model_copy(deep=True)

        self.internal_result = AnswerComparator().compare(
            self.world,
            learner_state=self.learner_state,
            attempt=attempt,
            hypothesis_id=self.analysis.hypothesis_id,
            contradictions=self.contradictions,
        )
        self._public_result = self.internal_result.to_public()
        self.execution_count += 1
        return self._public_result.model_copy(deep=True)

    def execute_bound(self) -> PublicAnswerComparison:
        """比较 Runtime 绑定的唯一 AnswerAttempt，不接受模型传入 ID。"""
        if len(self.attempts) != 1:
            raise CompareAnswerAuthorizationError("current turn must have exactly one bound answer attempt")
        return self.execute(next(iter(self.attempts)))

    def _is_bound_to_current_turn(self, attempt: AnswerAttempt) -> bool:
        return (
            attempt.session_id == self.session_id
            and attempt.turn_id == self.turn_id
            and attempt.revision == self.revision
        )

    @classmethod
    def bind_user_message(
        cls,
        *,
        request_id: str,
        session_id: str,
        turn_id: str,
        revision: int,
        world: HiddenWorld,
        learner_state: LearnerState,
        analysis: TurnAnalysis,
        user_message: str,
        contradictions: list[str] | None = None,
    ) -> "CompareAnswerRuntime":
        """从原始请求绑定唯一 AnswerAttempt 的 V2 构造入口。

        调用方无需把模型的 ``answer_attempt_text`` 复制进尝试；它只负责
        决定本轮是否值得比较，真正比较的文本始终是 ``user_message``。
        """

        attempt = AnswerAttempt.from_user_message(
            answer_attempt_id=f"{request_id}:answer",
            session_id=session_id,
            turn_id=turn_id,
            revision=revision,
            user_message=user_message,
        )
        return cls(
            request_id=request_id,
            session_id=session_id,
            turn_id=turn_id,
            revision=revision,
            world=world,
            learner_state=learner_state,
            analysis=analysis,
            attempts={attempt.answer_attempt_id: attempt},
            user_message=user_message,
            contradictions=list(contradictions or []),
        )


def compare_answer(
    ctx: RunContext[CompareAnswerRuntime],
) -> PublicAnswerComparison:
    """比较 Runtime 绑定的当前轮答案尝试；工具没有可由模型构造的参数。"""

    return ctx.deps.execute_bound()


compare_answer_tool = Tool(
    compare_answer,
    takes_ctx=True,
    name="compare_answer",
    description="比较 Runtime 绑定到当前轮的唯一答案尝试，不接受参数。",
    max_retries=1,
    strict=False,
)
