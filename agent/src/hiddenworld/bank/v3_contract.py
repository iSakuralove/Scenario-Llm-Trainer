"""Scenario V3 题目契约的确定性归一化与导出。

旧固定题仍以 ``public_scenario + hidden_world`` 保存，运行时需要继续兼容
这份快照。这个模块只做纯数据迁移：把旧形状拆成 scenario.v3 顶层字段，
并从 EvidenceGraph 编译工具依赖图；它不会生成或改写任何业务事实。
"""

from __future__ import annotations

import hashlib
import json
from pathlib import Path
from typing import Any, Iterable, Literal

from pydantic import BaseModel, ConfigDict, Field

from ..contracts import (
    CanonicalAnswer,
    ConceptDefinition,
    EvidenceNode,
    Hypothesis,
    HintStep,
    MisconceptionRule,
    Observation,
    PublicScenario,
    RootCause,
    SolutionRubric,
    TeachingModel,
    VirtualTool,
)


class ScenarioV3Metadata(BaseModel):
    model_config = ConfigDict(extra="forbid")

    stable_code: str
    domain: str
    difficulty: str
    scenario_type: str
    tags: list[str] = Field(default_factory=list)
    source: str
    version: int
    status: str
    legacy_model_version: str


class ToolDependencyEdge(BaseModel):
    model_config = ConfigDict(extra="forbid")

    action: str
    depends_on: list[str] = Field(default_factory=list)


class ScenarioV3Contract(BaseModel):
    """V3 顶层形状；CanonicalAnswer 仍是 Runtime-only 数据。"""

    model_config = ConfigDict(extra="forbid")

    contract_version: Literal["scenario.v3"] = "scenario.v3"
    metadata: ScenarioV3Metadata
    public_scenario: PublicScenario
    teaching_model: TeachingModel
    concept_catalog: list[ConceptDefinition] = Field(default_factory=list)
    hypothesis_catalog: list[Hypothesis] = Field(default_factory=list)
    evidence_graph: list[EvidenceNode] = Field(default_factory=list)
    observation_catalog: list[Observation] = Field(default_factory=list)
    tool_catalog: list[VirtualTool] = Field(default_factory=list)
    tool_dependency_graph: list[ToolDependencyEdge] = Field(default_factory=list)
    hint_ladder: list[HintStep] = Field(default_factory=list)
    misconception_rules: list[MisconceptionRule] = Field(default_factory=list)
    solution_rubric: SolutionRubric
    root_cause: RootCause
    diagnostic_relations: list[str] = Field(default_factory=list)
    canonical_answer: CanonicalAnswer


def normalize_fixed_question(question: Any) -> ScenarioV3Contract:
    """把 loader 返回的固定题快照转换为严格的 scenario.v3。"""

    world = question.hidden_world
    if world.canonical_answer is None:
        raise ValueError(f"固定题 {question.question_id} 缺少 canonical_answer，不能迁移到 scenario.v3")
    tool_catalog = _tool_catalog_for_world(world)

    contract = ScenarioV3Contract(
        metadata=ScenarioV3Metadata(
            stable_code=question.question_id,
            domain=question.domain,
            difficulty=question.difficulty,
            scenario_type=question.scenario_type,
            tags=list(question.tags),
            source=question.source,
            version=question.version,
            status=question.status,
            legacy_model_version=question.model_version,
        ),
        public_scenario=question.public_scenario,
        teaching_model=world.teaching_model,
        concept_catalog=list(world.teaching_model.concepts),
        hypothesis_catalog=list(world.hypotheses),
        evidence_graph=list(world.evidence_graph),
        observation_catalog=list(world.observations),
        tool_catalog=tool_catalog,
        tool_dependency_graph=_compile_dependency_edges(world.evidence_graph, tool_catalog),
        hint_ladder=list(world.teaching_model.hint_ladder),
        misconception_rules=list(world.misconception_rules),
        solution_rubric=world.solution_rubric,
        root_cause=world.root_cause,
        diagnostic_relations=list(world.diagnostic_relations),
        canonical_answer=world.canonical_answer,
    )
    validate_scenario_v3(contract)
    return contract


def validate_scenario_v3(contract: ScenarioV3Contract) -> None:
    """校验 V3 字段映射、工具/观察一一对应和依赖图派生一致性。"""

    if contract.metadata.stable_code.strip() == "":
        raise ValueError("scenario.v3 metadata.stable_code 不能为空")
    if contract.canonical_answer.answer_version.strip() == "":
        raise ValueError("scenario.v3 canonical_answer.answer_version 不能为空")

    tool_actions = [item.observation_action for item in contract.tool_catalog]
    observation_actions = [item.action for item in contract.observation_catalog]
    if len(tool_actions) != len(set(tool_actions)):
        raise ValueError("scenario.v3 tool_catalog.observation_action 存在重复")
    if len(observation_actions) != len(set(observation_actions)):
        raise ValueError("scenario.v3 observation_catalog.action 存在重复")
    if set(tool_actions) != set(observation_actions):
        raise ValueError("scenario.v3 tool_catalog 与 observation_catalog 动作集合不一致")

    expected = _compile_dependency_edges(contract.evidence_graph, contract.tool_catalog)
    if contract.tool_dependency_graph != expected:
        raise ValueError("scenario.v3 tool_dependency_graph 不是 EvidenceGraph 的确定性编译结果")

    evidence_ids = {item.evidence_id for item in contract.evidence_graph}
    for tool in contract.tool_catalog:
        unknown = set(tool.evidence_ids) - evidence_ids
        if unknown:
            raise ValueError(
                f"scenario.v3 tool_catalog {tool.tool_id} 引用了不存在的证据：{sorted(unknown)}"
            )


def scenario_v3_checksum(contract: ScenarioV3Contract) -> str:
    """计算包含隐藏契约的稳定 SHA-256，用于版本导入和回滚核对。"""

    payload = json.dumps(
        contract.model_dump(mode="json"),
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    return hashlib.sha256(payload).hexdigest()


def export_fixed_v3_bank(
    destination: str | Path,
    questions: Iterable[Any] | None = None,
) -> dict[str, Any]:
    """导出固定题 V3 文件和迁移清单；默认覆盖当前四道固定题。"""

    if questions is None:
        from .loader import list_fixed_questions

        questions = list_fixed_questions()

    destination_path = Path(destination)
    destination_path.mkdir(parents=True, exist_ok=True)
    artifacts: list[dict[str, str]] = []
    for question in questions:
        contract = normalize_fixed_question(question)
        filename = f"{contract.metadata.stable_code}.json"
        (destination_path / filename).write_text(
            json.dumps(contract.model_dump(mode="json"), ensure_ascii=False, indent=2) + "\n",
            encoding="utf-8",
        )
        artifacts.append(
            {
                "stable_code": contract.metadata.stable_code,
                "file": filename,
                "checksum": scenario_v3_checksum(contract),
            }
        )

    manifest = {
        "contract_version": "scenario.v3",
        "artifact_count": len(artifacts),
        "artifacts": artifacts,
    }
    (destination_path / "manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    return manifest


def _compile_dependency_edges(
    evidence_graph: Iterable[EvidenceNode],
    tool_catalog: Iterable[VirtualTool],
) -> list[ToolDependencyEdge]:
    public_actions = {
        item.observation_action
        for item in tool_catalog
        if item.observation_action and "compare_answer" not in item.observation_action
    }
    evidence_by_id = {item.evidence_id: item for item in evidence_graph}
    dependencies: dict[str, set[str]] = {action: set() for action in public_actions}
    for node in evidence_by_id.values():
        targets = [action for action in node.obtained_by if action in public_actions]
        prerequisites = {
            action
            for prerequisite_id in node.prerequisites
            for action in getattr(evidence_by_id.get(prerequisite_id), "obtained_by", [])
            if action in public_actions
        }
        for target in targets:
            dependencies[target].update(prerequisites)
    return [
        ToolDependencyEdge(action=action, depends_on=sorted(dependencies[action]))
        for action in sorted(dependencies)
        if dependencies[action]
    ]


def _tool_catalog_for_world(world: Any) -> list[VirtualTool]:
    """把旧题的 observation-only/部分目录形状显式补成 V3 tool_catalog。"""

    tools = list(world.virtual_tools)
    known_actions = {item.observation_action for item in tools}
    for observation in world.observations:
        if observation.action in known_actions:
            continue
        tools.append(
            VirtualTool(
                tool_id=f"tool.legacy.{_legacy_action_suffix(observation.action)}",
                kind=_legacy_action_kind(observation.action),
                target=observation.action,
                aliases=[],
                query_patterns=[],
                redacted_parameters=[],
                simulated_output=observation.result,
                observation_action=observation.action,
                evidence_ids=list(observation.yields_evidence),
            )
        )
        known_actions.add(observation.action)
    return tools


def _legacy_action_suffix(action: str) -> str:
    return action.replace(":", ".").replace(".", "_")


def _legacy_action_kind(action: str) -> str:
    _, separator, remainder = action.partition(":")
    if not separator:
        return "observation"
    kind, _, _ = remainder.partition(".")
    return kind or "observation"
