"""TurnInterpreter 单轮金标集评测。"""

from __future__ import annotations

import asyncio
import json
from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from pydantic_evals import Case, Dataset
from pydantic_evals.evaluators import Evaluator, EvaluatorContext

from hiddenworld.agents.interpreter import create_interpreter_agent
from hiddenworld.bank.loader import load_fixed_question
from hiddenworld.contracts import InterpreterDeps, TurnAnalysis
from hiddenworld.evals.matrix import classify_provider_error

DEFAULT_GOLDEN_PATH = Path(__file__).resolve().parents[3] / "evals" / "goldens" / "interpreter_seed.jsonl"


@dataclass(frozen=True)
class GoldenCase:
    case_id: str
    input: str
    expected_actions: tuple[str, ...] = ()
    forbidden_actions: tuple[str, ...] = ()
    hypothesis_id: str = ""
    contains_answer_attempt: bool | None = None
    is_stuck: bool | None = None
    is_noise: bool | None = None


@dataclass(frozen=True)
class GoldenFailure:
    case_id: str
    codes: tuple[str, ...]


@dataclass
class GoldenReport:
    total: int
    completed: int
    passed: int
    failures: list[GoldenFailure]
    error_code: str = ""

    def public_dict(self) -> dict[str, Any]:
        return {
            "total": self.total,
            "completed": self.completed,
            "passed": self.passed,
            "completion_rate": (self.completed / self.total) if self.total else 0.0,
            "pass_rate": (self.passed / self.completed) if self.completed else 0.0,
            "failures": [{"case_id": item.case_id, "codes": list(item.codes)} for item in self.failures],
            "error_code": self.error_code,
        }


@dataclass
class GoldenEvaluator(Evaluator):
    """把 HiddenWorld 金标断言接入 Pydantic Evals。"""

    def evaluate(self, ctx: EvaluatorContext[GoldenCase, TurnAnalysis, None]) -> dict[str, Any]:
        if ctx.output is None:
            return {"completed": False, "golden_match": False}
        codes = _check_golden(ctx.inputs, ctx.output)
        return {
            "completed": True,
            "golden_match": not codes,
            "failure_count": len(codes),
        }


def build_interpreter_dataset(
    cases: Sequence[GoldenCase] | None = None,
) -> Dataset[GoldenCase, TurnAnalysis, None]:
    """构造可交给 ``Dataset.evaluate`` 的单轮金标集。"""

    selected = list(cases if cases is not None else load_golden_cases())
    return Dataset(
        name="hiddenworld_interpreter_goldens",
        cases=[Case(name=case.case_id, inputs=case) for case in selected],
        evaluators=[GoldenEvaluator()],
    )


def load_golden_cases(path: Path = DEFAULT_GOLDEN_PATH) -> list[GoldenCase]:
    cases: list[GoldenCase] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        raw = json.loads(line)
        cases.append(
            GoldenCase(
                case_id=raw["case_id"],
                input=raw["input"],
                expected_actions=tuple(raw.get("expected_actions", [])),
                forbidden_actions=tuple(raw.get("forbidden_actions", [])),
                hypothesis_id=raw.get("hypothesis_id", ""),
                contains_answer_attempt=raw.get("contains_answer_attempt"),
                is_stuck=raw.get("is_stuck"),
                is_noise=raw.get("is_noise"),
            )
        )
    return cases


async def run_interpreter_goldens(
    model: Any,
    *,
    question_id: str = "hw-db-index-001",
    cases: Sequence[GoldenCase] | None = None,
    deadline_seconds: float = 15.0,
) -> GoldenReport:
    question = load_fixed_question(question_id)
    agent = create_interpreter_agent(model)
    selected = list(cases if cases is not None else load_golden_cases())
    failures: list[GoldenFailure] = []
    completed = 0
    passed = 0
    error_code = ""
    for case in selected:
        try:
            result = await asyncio.wait_for(
                agent.run(
                    case.input,
                    deps=InterpreterDeps(
                        public_scenario=question.public_scenario,
                        hypotheses=question.hidden_world.hypotheses,
                        known_actions=[item.action for item in question.hidden_world.observations],
                    ),
                ),
                timeout=max(deadline_seconds, 0.001),
            )
        except Exception as exc:  # noqa: BLE001 - provider 错误只输出脱敏类别
            error_code = classify_provider_error(exc)
            break
        completed += 1
        codes = _check_golden(case, result.output)
        if codes:
            failures.append(GoldenFailure(case.case_id, tuple(codes)))
        else:
            passed += 1
    return GoldenReport(len(selected), completed, passed, failures, error_code)


def _check_golden(case: GoldenCase, analysis: TurnAnalysis) -> list[str]:
    codes: list[str] = []
    for action in case.expected_actions:
        if action not in analysis.actions:
            codes.append(f"missing_action:{action}")
    for action in case.forbidden_actions:
        if action in analysis.actions:
            codes.append(f"forbidden_action:{action}")
    if case.hypothesis_id and analysis.hypothesis_id != case.hypothesis_id:
        codes.append("hypothesis_id")
    if (
        case.contains_answer_attempt is not None
        and analysis.contains_answer_attempt != case.contains_answer_attempt
    ):
        codes.append("contains_answer_attempt")
    if case.is_stuck is not None and analysis.is_stuck != case.is_stuck:
        codes.append("is_stuck")
    if case.is_noise is not None and analysis.is_noise != case.is_noise:
        codes.append("is_noise")
    return codes
