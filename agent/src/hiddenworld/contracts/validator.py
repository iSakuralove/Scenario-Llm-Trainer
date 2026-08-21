"""ScenarioContract 的生成/加载校验。"""

from __future__ import annotations

from pydantic import BaseModel

from .world import HiddenWorld


class ScenarioContractValidationError(ValueError):
    """题目契约不满足唯一答案或引用一致性。"""


class ScenarioContractValidator:
    """在题目入库和 Runtime 加载前执行的确定性校验。"""

    def validate(self, world: HiddenWorld) -> HiddenWorld:
        answer = world.canonical_answer
        if answer is None:
            raise ScenarioContractValidationError("canonical_answer is required for a v2 scenario")
        if not answer.canonical_conclusion.strip():
            raise ScenarioContractValidationError("canonical_conclusion must not be empty")
        if not answer.answer_version.strip():
            raise ScenarioContractValidationError("answer_version must not be empty")
        if answer.root_cause_id != world.root_cause.id:
            raise ScenarioContractValidationError("canonical_answer root_cause_id does not match root_cause")

        evidence_ids = {item.evidence_id for item in world.evidence_graph}
        missing_evidence = set(answer.required_evidence_ids) - evidence_ids
        if missing_evidence:
            raise ScenarioContractValidationError(
                f"canonical_answer references missing evidence: {sorted(missing_evidence)}"
            )

        relation_ids = set(world.diagnostic_relations)
        missing_relations = set(answer.required_causal_relations) - relation_ids
        if missing_relations:
            raise ScenarioContractValidationError(
                f"canonical_answer references missing causal relations: {sorted(missing_relations)}"
            )

        if set(answer.solution_requirements) != set(world.root_cause.solution_requirements):
            raise ScenarioContractValidationError(
                "canonical_answer solution_requirements drift from root_cause"
            )

        rubric_terms = set(world.solution_rubric.required_actions) | set(world.solution_rubric.verification_steps)
        if not set(answer.solution_requirements).issubset(rubric_terms):
            raise ScenarioContractValidationError(
                "canonical_answer solution_requirements are not covered by solution_rubric"
            )

        canonical_identity = answer.canonical_conclusion.strip().casefold()
        if any(item.strip().casefold() == canonical_identity for item in answer.accepted_equivalents):
            raise ScenarioContractValidationError(
                "accepted_equivalents must not duplicate canonical_conclusion"
            )
        if len({item.strip().casefold() for item in answer.accepted_equivalents if item.strip()}) != len(
            [item for item in answer.accepted_equivalents if item.strip()]
        ):
            raise ScenarioContractValidationError("accepted_equivalents contain duplicates")
        return world


def validate_scenario_contract(world: HiddenWorld) -> HiddenWorld:
    return ScenarioContractValidator().validate(world)
