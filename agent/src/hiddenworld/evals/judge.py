"""用 Pydantic Evals LLMJudge 评估多轮导师会话现象。"""

from __future__ import annotations

import asyncio
from dataclasses import dataclass
from typing import Any

from pydantic_evals import Case, Dataset
from pydantic_evals.evaluators import LLMJudge

from hiddenworld.evals.matrix import ALL_TRAJECTORIES, MatrixReport, classify_provider_error

_BEHAVIOR_RUBRIC = """
评估这段纯文本排查教学对话是否达到行为等价门槛。不要要求固定文案、固定问题数量、
固定工具顺序或固定步骤数。只有同时满足以下条件才通过：
1. Mentor 能跟随学生已经采取的动作和新观察推进，不把阴性结果当作没有进展；
2. Mentor 的引导能适应学生的新手、偏航、专家或受挫状态，没有机械地把所有人赶向同一路径；
3. 多轮之间没有明显复读同一句式，也没有持续输出僵硬检查表；
4. Mentor 推动学生补充证据、排除假设或形成验证闭环，不直接宣布标准答案；
5. 回复之间不存在明显自相矛盾或遗忘学生已确认事实的现象。
""".strip()


@dataclass(frozen=True)
class TranscriptJudgeFailure:
    question_id: str
    case_id: str
    code: str


@dataclass
class TranscriptJudgeReport:
    provider: str
    judge_provider: str
    total: int
    completed: int
    passed: int
    failures: list[TranscriptJudgeFailure]
    error_code: str = ""

    def public_dict(self) -> dict[str, Any]:
        return {
            "provider": self.provider,
            "judge_provider": self.judge_provider,
            "total": self.total,
            "completed": self.completed,
            "passed": self.passed,
            "completion_rate": (self.completed / self.total) if self.total else 0.0,
            "pass_rate": (self.passed / self.completed) if self.completed else 0.0,
            "failures": [
                {
                    "question_id": item.question_id,
                    "case_id": item.case_id,
                    "code": item.code,
                }
                for item in self.failures
            ],
            "error_code": self.error_code,
        }


async def run_transcript_judge(
    matrix: MatrixReport,
    judge_model: Any,
    *,
    judge_provider: str,
    deadline_seconds: float = 15.0,
) -> TranscriptJudgeReport:
    """逐条评多轮 transcript；公开报告不保留 transcript 或 Judge 理由。"""

    descriptions = {item.case_id: item.description for item in ALL_TRAJECTORIES}
    adaptive_reports = [item for item in matrix.trajectories if item.kind == "adaptive"]
    failures: list[TranscriptJudgeFailure] = []
    completed = 0
    passed = 0
    error_code = ""
    for item in adaptive_reports:
        if not item.passed or len(item._judge_transcript) != item.turns * 2:
            failures.append(TranscriptJudgeFailure(item.question_id, item.case_id, "matrix_not_judgeable"))
            continue
        output = {
            "persona": descriptions.get(item.case_id, item.case_id),
            "transcript": list(item._judge_transcript),
        }
        dataset = Dataset(
            name=f"hiddenworld_transcript_{item.question_id}_{item.case_id}",
            cases=[Case(name=item.case_id, inputs=item.case_id)],
            evaluators=[
                LLMJudge(
                    rubric=_BEHAVIOR_RUBRIC,
                    model=judge_model,
                    score=False,
                    assertion={
                        "evaluation_name": "behavior_equivalent",
                        "include_reason": False,
                    },
                )
            ],
        )
        try:
            evaluation = await asyncio.wait_for(
                dataset.evaluate(
                    _constant_task(output),
                    progress=False,
                    max_concurrency=1,
                ),
                timeout=max(deadline_seconds, 0.001),
            )
        except Exception as exc:  # noqa: BLE001 - 只输出 provider 错误类别
            error_code = classify_provider_error(exc)
            break
        completed += 1
        assertion = evaluation.cases[0].assertions.get("behavior_equivalent")
        if assertion is not None and assertion.value is True:
            passed += 1
        else:
            failures.append(TranscriptJudgeFailure(item.question_id, item.case_id, "judge_rejected"))
    return TranscriptJudgeReport(
        provider=matrix.provider,
        judge_provider=judge_provider,
        total=len(adaptive_reports),
        completed=completed,
        passed=passed,
        failures=failures,
        error_code=error_code,
    )


def _constant_task(output: dict[str, Any]):
    def task(_case_id: str) -> dict[str, Any]:
        return output

    return task
