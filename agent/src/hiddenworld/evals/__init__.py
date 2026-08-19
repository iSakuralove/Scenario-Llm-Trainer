"""HiddenWorld 阶段 5 的离线金标与多轮轨迹评测。"""

from .goldens import GoldenCase, GoldenReport, load_golden_cases, run_interpreter_goldens
from .matrix import (
    ADAPTIVE_TRAJECTORIES,
    ALL_TRAJECTORIES,
    FIXED_TRAJECTORIES,
    MatrixReport,
    TrajectoryCase,
    apply_proposals_for_eval,
    check_result_hard_contract,
    run_matrix,
    run_trajectory,
)

__all__ = [
    "ADAPTIVE_TRAJECTORIES",
    "ALL_TRAJECTORIES",
    "FIXED_TRAJECTORIES",
    "GoldenCase",
    "GoldenReport",
    "MatrixReport",
    "TrajectoryCase",
    "apply_proposals_for_eval",
    "check_result_hard_contract",
    "load_golden_cases",
    "run_interpreter_goldens",
    "run_matrix",
    "run_trajectory",
]
