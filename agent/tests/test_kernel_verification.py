from hiddenworld.kernel.antiguess import AntiGuess
from hiddenworld.kernel.verifier import RootCauseVerifier


def test_verifier_keeps_out_of_catalog_hypothesis_unknown(hidden_world, learner_state) -> None:
    relation = RootCauseVerifier().relation(
        hidden_world,
        hypothesis_id="H_OTHER",
        learner_state=learner_state,
    )

    assert relation == "unknown"


def test_antiguess_uses_best_sufficient_path_and_blocks_unproven_target(hidden_world) -> None:
    decision = AntiGuess().evaluate(
        hidden_world,
        collected_evidence=["E_RELEASE_LOG"],
        relation="target",
    )

    assert decision.coverage == 0.5
    assert decision.best_evidence_set == ["E_RELEASE_LOG", "E_DDL_DIFF"]
    assert decision.missing_evidence == ["E_DDL_DIFF"]
    assert decision.completion_allowed is False
