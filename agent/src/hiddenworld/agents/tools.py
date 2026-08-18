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
    contradictions: list[str] = field(default_factory=list)
    execution_count: int = field(default=0, init=False)
    internal_result: InternalAnswerComparison | None = field(default=None, init=False)
    _public_result: PublicAnswerComparison | None = field(default=None, init=False, repr=False)

    def execute(self, answer_attempt_id: str) -> PublicAnswerComparison:
        attempt = self.attempts.get(answer_attempt_id)
        if attempt is None or not self._is_bound_to_current_turn(attempt):
            raise CompareAnswerAuthorizationError("answer attempt is not bound to the current turn")
        if not self.analysis.contains_answer_attempt or attempt.text != self.analysis.answer_attempt_text:
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

    def _is_bound_to_current_turn(self, attempt: AnswerAttempt) -> bool:
        return (
            attempt.session_id == self.session_id
            and attempt.turn_id == self.turn_id
            and attempt.revision == self.revision
        )


def compare_answer(
    ctx: RunContext[CompareAnswerRuntime],
    answer_attempt_id: str,
) -> PublicAnswerComparison:
    """比较服务端绑定到当前轮的答案尝试；不得传入答案正文。"""

    return ctx.deps.execute(answer_attempt_id)


compare_answer_tool = Tool(
    compare_answer,
    takes_ctx=True,
    name="compare_answer",
    description="比较服务端绑定到当前轮的答案尝试，只接受 answer_attempt_id。",
    max_retries=1,
    strict=False,
)
