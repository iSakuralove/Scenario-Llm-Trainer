from hiddenworld.kernel.world import HiddenWorldEngine


def test_world_returns_configured_observation_for_available_action(hidden_world) -> None:
    observation = HiddenWorldEngine().observe(
        hidden_world,
        action="inspect:metrics.cpu",
        collected_evidence=[],
    )

    assert observation.action == "inspect:metrics.cpu"
    assert observation.result == "数据库 CPU 使用率 35%，内存和 IO 都在正常区间。"
    assert observation.is_negative is True
    assert observation.yields_evidence == ["E_CPU_NORMAL"]
    assert observation.rules_out == ["H_CPU_BOUND"]


def test_world_uses_natural_response_when_prerequisite_is_missing(hidden_world) -> None:
    world = hidden_world.model_copy(deep=True)
    configured = next(item for item in world.observations if item.action == "inspect:data.explain")
    configured.unmet_prerequisite_result = "你要看哪条 SQL 的执行计划？"

    observation = HiddenWorldEngine().observe(
        world,
        action="inspect:data.explain",
        collected_evidence=[],
    )

    assert observation.result == "你要看哪条 SQL 的执行计划？"
    assert observation.yields_evidence == []
    assert observation.rules_out == []


def test_world_returns_neutral_observation_for_unknown_action(hidden_world) -> None:
    observation = HiddenWorldEngine().observe(
        hidden_world,
        action="inspect:runtime.thread_dump",
        collected_evidence=[],
    )

    assert observation.action == "inspect:runtime.thread_dump"
    assert observation.result == "这个动作暂时没有返回可用的新观察。"
    assert observation.is_negative is False
    assert observation.yields_evidence == []
    assert observation.rules_out == []
    assert hidden_world.root_cause.description not in observation.result
