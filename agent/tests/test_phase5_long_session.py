"""Phase 5 长会话、重放与教学状态回归。

这些断言只检查安全投影和教学动作，不锁死模型正文，也不把题目的隐藏答案
复制到评测夹具中。它们覆盖长期计划要求的上下文窗口、解释递减和提示回落。
"""

import json
from types import SimpleNamespace

import pytest

from hiddenworld.contracts import (
    AgentTurnRequest,
    ConceptDefinition,
    GuidanceState,
    HintStep,
    LearnerState,
    Turn,
    TurnAssessment,
)
from hiddenworld.bank.loader import load_fixed_question
from hiddenworld.evals.matrix import TrajectoryCase, run_trajectory
from hiddenworld.scenario_runtime.context import project_agent_context
from hiddenworld.scenario_runtime.response_brief import ResponseBriefBuilder


def _request_with_transcript(*, hidden_world, public_scenario, learner_state, transcript):
    return AgentTurnRequest(
        request_id="phase5-replay-1",
        session_id="phase5-session-1",
        state_revision=18,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        transcript=transcript,
        user_message="第 19 轮：保留当前消息，不要只依赖历史摘要。",
    )


def test_phase5_context_window_keeps_recent_four_turns_and_current_message(
    hidden_world,
    public_scenario,
    learner_state,
) -> None:
    transcript: list[Turn] = []
    for turn_number in range(1, 7):
        transcript.extend(
            [
                Turn(role="user", content=f"学生第 {turn_number} 轮", turn_number=turn_number),
                Turn(role="mentor", content=f"导师第 {turn_number} 轮", turn_number=turn_number),
            ]
        )

    request = _request_with_transcript(
        hidden_world=hidden_world,
        public_scenario=public_scenario,
        learner_state=learner_state,
        transcript=transcript,
    )
    context = project_agent_context(request)

    # 最近四个完整回合 = 第 3～6 轮；第 19 轮消息必须单独保留。
    assert len(context.transcript) == 8
    assert [item.turn_number for item in context.transcript] == [3, 3, 4, 4, 5, 5, 6, 6]
    assert context.current_user_message == "第 19 轮：保留当前消息，不要只依赖历史摘要。"

    dumped = json.dumps(context.model_dump(mode="json"), ensure_ascii=False)
    assert "canonical_answer" not in dumped
    assert "root_cause" not in dumped
    assert "accepted_hypotheses" not in dumped


def test_phase5_context_projection_is_deterministic_and_does_not_mutate_replay_input(
    hidden_world,
    public_scenario,
    learner_state,
) -> None:
    transcript = [
        Turn(role="user", content="第 1 轮", turn_number=1),
        Turn(role="mentor", content="先看公开现象。", turn_number=1),
    ]
    request = _request_with_transcript(
        hidden_world=hidden_world,
        public_scenario=public_scenario,
        learner_state=learner_state,
        transcript=transcript,
    )

    first = project_agent_context(request)
    second = project_agent_context(request)

    assert first.model_dump(mode="json") == second.model_dump(mode="json")
    first.transcript[0].content = "只改动本次投影"
    replay = project_agent_context(request)
    assert replay.transcript[0].content == "第 1 轮"
    assert request.transcript[0].content == "第 1 轮"


@pytest.mark.asyncio
async def test_phase5_eighteen_turn_runner_replays_ids_and_keeps_agent_context_bounded(
    monkeypatch,
) -> None:
    """长轨迹每轮都带稳定幂等元数据，但模型只拿到最近窗口。"""

    import hiddenworld.evals.matrix as matrix

    # 本测试聚焦 runner 的请求/上下文编排；公开结果硬契约由其它矩阵测试覆盖。
    monkeypatch.setattr(matrix, "check_result_hard_contract", lambda *args, **kwargs: [])

    question = load_fixed_question("hw-network-vip-001")
    case = TrajectoryCase(
        case_id="phase5-eighteen",
        kind="adaptive",
        messages=tuple(f"第 {turn} 轮继续核对公开现象" for turn in range(1, 19)),
        description="Phase 5 长会话回放",
    )

    class RecordingRuntime:
        def __init__(self) -> None:
            self.requests = []
            self.contexts = []

        async def run_turn(self, request):
            self.requests.append(request)
            self.contexts.append(project_agent_context(request))
            return SimpleNamespace(
                contract_version="hiddenworld.v1",
                public_trace=[],
                reply="基于当前公开信息继续核对。",
                proposals=[],
                internal_verification=SimpleNamespace(completion_allowed=False),
            )

    runtime = RecordingRuntime()
    report = await run_trajectory(
        runtime,
        question,
        case,
        provider="test",
        request_prefix="phase5-replay",
    )

    assert report.turns == 18
    assert [item.request_id for item in runtime.requests] == [
        f"phase5-replay-phase5-eighteen-{turn}" for turn in range(1, 19)
    ]
    assert [item.state_revision for item in runtime.requests] == list(range(18))
    assert [len(item.transcript) for item in runtime.contexts] == [
        0,
        2,
        4,
        6,
        8,
        8,
        8,
        8,
        8,
        8,
        8,
        8,
        8,
        8,
        8,
        8,
        8,
        8,
    ]
    assert runtime.contexts[-1].current_user_message == "第 18 轮继续核对公开现象"


def test_phase5_guidance_snapshot_survives_replay_without_completion_leak(
    hidden_world,
    public_scenario,
    learner_state,
) -> None:
    state = learner_state.model_copy(
        update={
            "stalled_turns": 2,
            "current_focus": "config",
            "hint_level": 2,
            "last_hint": "比较故障发生前后的入口配置。",
        }
    )
    guidance = GuidanceState(
        teaching_state="guided_inquiry",
        progress_assessment="no_progress",
        stalled_turns=2,
        current_focus="config",
    )
    request = _request_with_transcript(
        hidden_world=hidden_world,
        public_scenario=public_scenario,
        learner_state=state,
        transcript=[],
    )

    context = project_agent_context(request, prior_guidance_state=guidance)

    assert context.guidance_state.teaching_state == "guided_inquiry"
    assert context.guidance_state.stalled_turns == 2
    assert context.guidance_state.current_focus == "config"
    assert context.learner_summary.hint_level == 2
    assert context.learner_summary.last_hint == "比较故障发生前后的入口配置。"
    assert context.turn_control.terminal is False
    dumped = json.dumps(context.model_dump(mode="json"), ensure_ascii=False)
    assert "completion_allowed" not in dumped
    assert "completion_ready" not in dumped


def test_phase5_eighteen_turn_projection_stops_repeating_mastered_concepts() -> None:
    concepts = [
        ConceptDefinition(
            concept_id="callback",
            label="Callback",
            summary="支付平台通知业务系统处理结果的请求。",
            aliases=["支付回调"],
        ),
        ConceptDefinition(
            concept_id="gateway",
            label="Gateway",
            summary="请求进入后端前的入口层，负责转发和超时控制。",
            aliases=["网关"],
        ),
    ]
    builder = ResponseBriefBuilder()
    state = LearnerState()
    first = builder.build(
        state,
        concept_catalog=concepts,
        required_concepts=["Callback", "Gateway"],
        turn_assessment=TurnAssessment(intent="explanation_request"),
    )
    assert first.primary_task == "explain_concept"
    assert first.explain_concepts == ["Callback", "Gateway"]

    state = state.model_copy(update={"concept_mastery": {"callback": 3, "gateway": 3}})
    for turn in range(2, 19):
        brief = builder.build(
            state,
            concept_catalog=concepts,
            required_concepts=["Callback", "Gateway"],
            public_observations=[f"第 {turn} 轮公开观察"],
            turn_assessment=TurnAssessment(
                intent="investigate",
                progress_assessment="progress",
            ),
        )
        assert brief.explain_concepts == []
        assert brief.known_concepts == ["Callback", "Gateway"]
        # 有公开观察时，证据翻译优先于泛化的“做得很好”；这正是长会话
        # 从概念教学切换到证据解释后的稳定节奏。
        assert brief.primary_task == "interpret_evidence"


def test_phase5_hint_ladder_escalates_to_level_four_and_recovers_on_progress() -> None:
    builder = ResponseBriefBuilder()
    hint_steps = [
        HintStep(level=1, public_hint="先对齐故障发生前后的入口变化。"),
        HintStep(level=2, public_hint="比较切换前后的 Gateway 配置。"),
        HintStep(level=3, public_hint="重点看 Gateway timeout。"),
        HintStep(level=4, public_hint="把 Gateway、Nginx 和 Callback 的时间线放在一起。"),
    ]

    levels: list[int] = []
    for stalled_turns in range(1, 5):
        brief = builder.build(
            LearnerState(stalled_turns=stalled_turns),
            hint_steps=hint_steps,
            turn_assessment=TurnAssessment(is_stuck=True, intent="help_request"),
        )
        levels.append(brief.hint_level)
        assert brief.primary_task == "release_hint"
        assert brief.hint_text == hint_steps[stalled_turns - 1].public_hint
        assert brief.investigation.discovered_clues == []

    assert levels == [1, 2, 3, 4]

    recovered = builder.build(
        LearnerState(
            stalled_turns=0,
            hint_level=4,
            last_hint=hint_steps[-1].public_hint,
        ),
        hint_steps=hint_steps,
        public_clues=["学生确认了 Gateway timeout 的前后变化"],
        turn_assessment=TurnAssessment(progress_assessment="progress"),
    )
    assert recovered.primary_task == "acknowledge_progress"
    assert recovered.hint_level == 0
    assert recovered.hint_text == ""
    assert recovered.investigation.discovered_clues == ["学生确认了 Gateway timeout 的前后变化"]
