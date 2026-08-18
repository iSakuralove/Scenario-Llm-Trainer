"""固定题库与题目校验。

题目内容是本架构真正的代价——HiddenWorld 要求每题写出证据图、假设集、
观察响应、排除规则和误解规则，比旧的 RevealStrategy{Surface/Deep/Distractors}
重一个量级。这个代价不在代码里，在内容里。

正因如此，校验必须自动化且分三层：任何一层失败都不得进入 active。
"""

from __future__ import annotations

from .loader import (
    FIXED_BANK_IDS,
    FixedQuestion,
    list_fixed_questions,
    load_fixed_question,
)
from .validation import (
    ValidationError,
    ValidationReport,
    validate_question,
)

__all__ = [
    "FIXED_BANK_IDS",
    "FixedQuestion",
    "ValidationError",
    "ValidationReport",
    "list_fixed_questions",
    "load_fixed_question",
    "validate_question",
]
