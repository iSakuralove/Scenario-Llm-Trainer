"""排查工坊 HiddenWorld Agent。

架构约束（不可协商）：

1. **只有两个组件使用 LLM**：TurnInterpreter 和 Mentor。
   HiddenWorld、EvidenceEngine、RootCauseVerifier、AntiGuess、ClueGate、
   TeachingPolicy、AnswerComparator 和 Guard 全部是确定性代码。

2. **知道答案的组件不说话，说话的组件不知道答案。**
   根因原文从头到尾不进入任何一个 LLM prompt。

3. **决定"能不能做"的组件，不决定"该怎么说"。**
   TeachingPolicy 输出约束，不输出言语行为。

4. Python 无状态。业务状态、revision、幂等、提议审批和持久化都在 Go。
"""

from __future__ import annotations

from .contracts import CONTRACT_VERSION

__all__ = ["CONTRACT_VERSION"]
