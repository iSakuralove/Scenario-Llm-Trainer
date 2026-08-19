"""运行阶段 5 评测：``python -m hiddenworld.evals``。"""

from __future__ import annotations

import argparse
import asyncio
import json

from pydantic_ai import models

from hiddenworld.agents.models import build_deepseek_model, build_glm_model
from hiddenworld.evals.goldens import run_interpreter_goldens
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
    parser.add_argument("--provider", choices=("deepseek", "glm", "all"), default="all")
    parser.add_argument("--suite", choices=("goldens", "matrix", "all"), default="all")
    parser.add_argument("--tracks", choices=("fixed", "adaptive", "all"), default="all")
    parser.add_argument("--deadline-seconds", type=float, default=15.0)
    return parser


async def _run(args: argparse.Namespace) -> None:
    models.ALLOW_MODEL_REQUESTS = True
    providers = ("deepseek", "glm") if args.provider == "all" else (args.provider,)
    tracks = {
        "fixed": FIXED_TRAJECTORIES,
        "adaptive": ADAPTIVE_TRAJECTORIES,
        "all": ALL_TRAJECTORIES,
    }[args.tracks]
    output: dict[str, object] = {"suite": args.suite, "providers": {}}
    matrix_reports: dict[str, MatrixReport] = {}
    for provider in providers:
        try:
            model = build_deepseek_model() if provider == "deepseek" else build_glm_model()
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
        if args.suite in ("matrix", "all") and not (
            golden_report is not None and golden_report.error_code
        ):
            matrix_report = await run_matrix(provider, model, trajectories=tracks)
            matrix_reports[provider] = matrix_report
            provider_output["matrix"] = matrix_report.public_dict()
        output["providers"][provider] = provider_output
    if args.provider == "all" and args.suite in ("matrix", "all"):
        output["comparison"] = compare_provider_matrices(
            matrix_reports.get("deepseek"),
            matrix_reports.get("glm"),
            trajectories=tracks,
        ).public_dict()
    print(json.dumps(output, ensure_ascii=False, indent=2))


def main() -> None:
    asyncio.run(_run(_parser().parse_args()))


if __name__ == "__main__":
    main()
