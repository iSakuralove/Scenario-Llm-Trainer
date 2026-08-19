"""运行阶段 5 评测：``python -m hiddenworld.evals``。"""

from __future__ import annotations

import argparse
import asyncio
import json

from pydantic_ai import models

from hiddenworld.agents.models import build_deepseek_model, build_glm_model, build_xuan_model
from hiddenworld.bank.loader import FIXED_BANK_IDS
from hiddenworld.evals.goldens import run_interpreter_goldens
from hiddenworld.evals.judge import run_transcript_judge
from hiddenworld.evals.matrix import (
    ADAPTIVE_TRAJECTORIES,
    ALL_TRAJECTORIES,
    FIXED_TRAJECTORIES,
    MatrixReport,
    compare_provider_matrices,
    run_matrix,
)


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="HiddenWorld 阶段 5 评测入口")
    parser.add_argument("--provider", choices=("deepseek", "glm", "xuan", "all"), default="all")
    parser.add_argument("--suite", choices=("goldens", "matrix", "all"), default="all")
    parser.add_argument("--tracks", choices=("fixed", "adaptive", "all"), default="all")
    parser.add_argument("--question-id", action="append", choices=FIXED_BANK_IDS)
    parser.add_argument(
        "--case-id",
        action="append",
        choices=sorted(item.case_id for item in ALL_TRAJECTORIES),
    )
    parser.add_argument(
        "--judge-provider",
        choices=("none", "same", "deepseek", "glm", "xuan"),
        default="none",
    )
    parser.add_argument("--xuan-model", choices=("grok-4.5", "deepseek-v4-flash-0731"))
    parser.add_argument("--deadline-seconds", type=float, default=15.0)
    return parser


async def _run(args: argparse.Namespace) -> None:
    models.ALLOW_MODEL_REQUESTS = True
    providers = ("deepseek", "glm") if args.provider == "all" else (args.provider,)
    track_cases = {
        "fixed": FIXED_TRAJECTORIES,
        "adaptive": ADAPTIVE_TRAJECTORIES,
        "all": ALL_TRAJECTORIES,
    }[args.tracks]
    selected_case_ids = set(args.case_id or ())
    tracks = tuple(item for item in track_cases if not selected_case_ids or item.case_id in selected_case_ids)
    if not tracks:
        raise SystemExit("--case-id 与 --tracks 没有交集")
    question_ids = tuple(args.question_id or FIXED_BANK_IDS)
    output: dict[str, object] = {
        "suite": args.suite,
        "question_ids": list(question_ids),
        "case_ids": [item.case_id for item in tracks],
        "providers": {},
    }
    matrix_reports: dict[str, MatrixReport] = {}
    for provider in providers:
        try:
            if provider == "deepseek":
                model = build_deepseek_model()
            elif provider == "glm":
                model = build_glm_model()
            else:
                model = build_xuan_model(model=args.xuan_model)
        except Exception as exc:  # credentials are intentionally summarized
            output["providers"][provider] = {"error_code": type(exc).__name__.lower()}
            continue
        provider_output: dict[str, object] = {}
        golden_report = None
        if args.suite in ("goldens", "all"):
            golden_report = await run_interpreter_goldens(
                model,
                deadline_seconds=args.deadline_seconds,
            )
            provider_output["goldens"] = golden_report.public_dict()
        if args.suite in ("matrix", "all") and not (golden_report is not None and golden_report.error_code):
            matrix_report = await run_matrix(
                provider,
                model,
                question_ids=question_ids,
                trajectories=tracks,
                deadline_seconds=args.deadline_seconds,
            )
            matrix_reports[provider] = matrix_report
            provider_output["matrix"] = matrix_report.public_dict()
            if args.judge_provider != "none":
                judge_provider = provider if args.judge_provider == "same" else args.judge_provider
                try:
                    if judge_provider == provider:
                        judge_model = model
                    elif judge_provider == "deepseek":
                        judge_model = build_deepseek_model()
                    elif judge_provider == "glm":
                        judge_model = build_glm_model()
                    else:
                        judge_model = build_xuan_model(model=args.xuan_model)
                    provider_output["judge"] = (
                        await run_transcript_judge(
                            matrix_report,
                            judge_model,
                            judge_provider=judge_provider,
                            deadline_seconds=args.deadline_seconds,
                        )
                    ).public_dict()
                except Exception as exc:  # credentials are intentionally summarized
                    provider_output["judge"] = {"error_code": type(exc).__name__.lower()}
        output["providers"][provider] = provider_output
    if args.provider == "all" and args.suite in ("matrix", "all"):
        output["comparison"] = compare_provider_matrices(
            matrix_reports.get("deepseek"),
            matrix_reports.get("glm"),
            question_ids=question_ids,
            trajectories=tracks,
        ).public_dict()
    print(json.dumps(output, ensure_ascii=False, indent=2))


def main() -> None:
    asyncio.run(_run(_parser().parse_args()))


if __name__ == "__main__":
    main()
