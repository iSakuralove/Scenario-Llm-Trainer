from __future__ import annotations

import json

from hiddenworld.bank import list_fixed_questions, load_fixed_question
from hiddenworld.bank.v3_contract import (
    export_fixed_v3_bank,
    normalize_fixed_question,
    scenario_v3_checksum,
    validate_scenario_v3,
)


def test_all_fixed_questions_normalize_to_scenario_v3() -> None:
    questions = list_fixed_questions()

    assert len(questions) == 4
    for question in questions:
        contract = normalize_fixed_question(question)
        assert contract.contract_version == "scenario.v3"
        assert contract.metadata.stable_code == question.question_id
        assert contract.public_scenario.title == question.public_scenario.title
        assert contract.concept_catalog == contract.teaching_model.concepts
        assert contract.hypothesis_catalog == question.hidden_world.hypotheses
        assert contract.observation_catalog == question.hidden_world.observations
        assert {
            item.observation_action for item in contract.tool_catalog
        } == {item.action for item in contract.observation_catalog}
        assert {
            item.observation_action for item in question.hidden_world.virtual_tools
        } <= {item.observation_action for item in contract.tool_catalog}
        assert contract.canonical_answer == question.hidden_world.canonical_answer
        validate_scenario_v3(contract)
        assert len(scenario_v3_checksum(contract)) == 64


def test_network_v3_dependency_graph_is_compiled_from_evidence_graph() -> None:
    contract = normalize_fixed_question(load_fixed_question("hw-network-vip-001"))
    dependencies = {
        item.action: item.depends_on for item in contract.tool_dependency_graph
    }

    assert dependencies["inspect:logs.nginx_callback"] == [
        "inspect:logs.callback_timeout",
    ]
    assert dependencies["inspect:logs.service_callback"] == [
        "inspect:logs.nginx_callback",
    ]
    assert dependencies["inspect:database.lock_wait"] == [
        "inspect:logs.service_callback",
    ]


def test_export_fixed_v3_bank_writes_manifest_and_four_contracts(tmp_path) -> None:
    manifest = export_fixed_v3_bank(tmp_path)

    assert manifest["contract_version"] == "scenario.v3"
    assert manifest["artifact_count"] == 4
    assert len(manifest["artifacts"]) == 4
    for item in manifest["artifacts"]:
        payload = json.loads((tmp_path / item["file"]).read_text(encoding="utf-8"))
        assert payload["contract_version"] == "scenario.v3"
        assert payload["metadata"]["stable_code"] == item["stable_code"]
        assert "hidden_world" not in payload
        assert item["checksum"] == scenario_v3_checksum(
            normalize_fixed_question(load_fixed_question(item["stable_code"]))
        )

    assert (tmp_path / "manifest.json").is_file()
