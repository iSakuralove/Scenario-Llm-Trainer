"""HiddenWorld 阶段 5 的离线金标与多轮轨迹评测。"""

from .goldens import (
    GoldenCase,
    GoldenReport,
    build_interpreter_dataset,
    load_golden_cases,
    run_interpreter_goldens,
)
from .matrix import (
    ADAPTIVE_TRAJECTORIES,
    ALL_TRAJECTORIES,
    FIXED_TRAJECTORIES,
    BehaviorEquivalenceThresholds,
    BehaviorMetrics,
    MatrixReport,
    ProviderComparisonReport,
    TrajectoryCase,
    TrajectoryComparison,
    TrajectoryReport,
    apply_proposals_for_eval,
    check_result_hard_contract,
    compare_provider_matrices,
    run_matrix,
    run_trajectory,
)

__all__ = [
    "ADAPTIVE_TRAJECTORIES",
    "ALL_TRAJECTORIES",
    "BehaviorEquivalenceThresholds",
    "BehaviorMetrics",
    "FIXED_TRAJECTORIES",
    "GoldenCase",
    "GoldenReport",
    "MatrixReport",
    "ProviderComparisonReport",
    "TrajectoryCase",
    "TrajectoryComparison",
    "TrajectoryReport",
    "apply_proposals_for_eval",
    "build_interpreter_dataset",
    "check_result_hard_contract",
    "compare_provider_matrices",
    "load_golden_cases",
    "run_interpreter_goldens",
    "run_matrix",
    "run_trajectory",
]
