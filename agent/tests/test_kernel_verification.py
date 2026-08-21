from hiddenworld.kernel.antiguess import AntiGuess
from hiddenworld.kernel.verifier import RootCauseVerifier
from hiddenworld.contracts import CanonicalAnswer


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


def test_antiguess_prefers_canonical_required_evidence(hidden_world) -> None:
    canonical = CanonicalAnswer(
        canonical_conclusion="索引缺失导致全表扫描",
        root_cause_id=hidden_world.root_cause.id,
        required_evidence_ids=["E_SLOW_SQL"],
        solution_requirements=list(hidden_world.root_cause.solution_requirements),
        answer_version="answer-v2",
    )
    world = hidden_world.model_copy(update={"canonical_answer": canonical})

    decision = AntiGuess().evaluate(
        world,
        collected_evidence=["E_SLOW_SQL"],
        relation="target",
    )

    assert decision.best_evidence_set == ["E_SLOW_SQL"]
    assert decision.coverage == 1.0
    assert decision.completion_allowed is True


def test_verifier_answer_text_wins_over_model_hypothesis_id(hidden_world, learner_state) -> None:
    canonical = CanonicalAnswer(
        canonical_conclusion="索引缺失导致全表扫描",
        root_cause_id=hidden_world.root_cause.id,
        accepted_equivalents=["索引问题"],
        answer_version="answer-v2",
    )
    world = hidden_world.model_copy(update={"canonical_answer": canonical})

    relation = RootCauseVerifier().relation(
        world,
        hypothesis_id="H_INDEX",
        answer_text="我认为是连接池打满。",
        learner_state=learner_state,
    )

    assert relation == "unrelated"
