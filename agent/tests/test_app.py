import json

import pytest
from httpx import ASGITransport, AsyncClient
from pydantic_ai.models.test import TestModel

from hiddenworld.agents.interpreter import create_interpreter_agent
from hiddenworld.agents.mentor import create_mentor_agent
from hiddenworld.app import create_app
from hiddenworld.contracts import AgentTurnRequest
from hiddenworld.runtime import HiddenWorldRuntime, TurnDeadlineExceeded


@pytest.mark.asyncio
async def test_turn_endpoint_returns_typed_result(hidden_world, learner_state, public_scenario) -> None:
    interpreter = create_interpreter_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "public_summary": "你说你完全不知道从哪下手，希望先拿到一点方向。",
                    "actions": [],
                    "hypothesis_id": "",
                    "hypothesis_raw": "",
                    "made_claim": False,
                    "contains_answer_attempt": False,
                    "answer_attempt_text": "",
                    "established_facts": [],
                    "is_stuck": True,
                    "is_noise": False,
                    "student_affect": "confused",
                    "confidence": 0.9,
                },
                ensure_ascii=False,
            )
        )
    )
    mentor = create_mentor_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "reply": "先不用急着下结论，从最容易验证的一条现象开始就好。",
                    "rationale": "学生明确卡住，先降低本轮负担。",
                    "requested_releases": [],
                    "confirms_hypothesis": False,
                    "expected_effort": "quick",
                },
                ensure_ascii=False,
            )
        )
    )
    request = AgentTurnRequest(
        request_id="request-http-1",
        session_id="session-1",
        state_revision=4,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="不知道该从哪看起",
    )
    app = create_app(HiddenWorldRuntime(interpreter=interpreter, mentor=mentor))

    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
        response = await client.post("/turn", json=request.model_dump(mode="json"))

    assert response.status_code == 200
    payload = response.json()
    assert payload["contract_version"] == "hiddenworld.v1"
    assert payload["request_id"] == "request-http-1"
    assert payload["reply"].startswith("先不用急")
    assert payload["turn_analysis"]["is_stuck"] is True


@pytest.mark.asyncio
async def test_turn_stream_endpoint_emits_internal_analysis_public_trace_and_final_result_in_order(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    interpreter = create_interpreter_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "public_summary": "你说你完全不知道从哪下手，希望先拿到一点方向。",
                    "actions": [],
                    "hypothesis_id": "",
                    "hypothesis_raw": "",
                    "made_claim": False,
                    "contains_answer_attempt": False,
                    "answer_attempt_text": "",
                    "established_facts": [],
                    "is_stuck": True,
                    "is_noise": False,
                    "student_affect": "confused",
                    "confidence": 0.9,
                },
                ensure_ascii=False,
            )
        )
    )
    mentor = create_mentor_agent(
        TestModel(
            custom_output_text=json.dumps(
                {
                    "reply": "先从最容易验证的一条公开现象开始。",
                    "rationale": "学生卡住，先降低本轮负担。",
                    "requested_releases": [],
                    "confirms_hypothesis": False,
                    "expected_effort": "quick",
                },
                ensure_ascii=False,
            )
        )
    )
    request = AgentTurnRequest(
        request_id="request-http-stream",
        session_id="session-1",
        state_revision=6,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="不知道该从哪看起",
    )
    app = create_app(HiddenWorldRuntime(interpreter=interpreter, mentor=mentor))

    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
        response = await client.post("/turn/stream", json=request.model_dump(mode="json"))

    assert response.status_code == 200
    assert response.headers["content-type"].startswith("text/event-stream")
    blocks = [item for item in response.text.split("\n\n") if item.strip()]
    names = [block.splitlines()[0].removeprefix("event: ") for block in blocks]
    # 推理增量在 interpreter 生成 public_summary 期间就外发，排在 turn_analysis 之前。
    assert names[0] == "public_trace"
    assert "turn_analysis" in names
    assert names.index("turn_analysis") < len(names) - 1
    assert names[-1] == "result"
    result_block = blocks[-1]
    result_line = next(line for line in result_block.splitlines() if line.startswith("data: "))
    result_payload = json.loads(result_line[6:])
    assert result_payload["request_id"] == "request-http-stream"
    assert result_payload["reply"].startswith("先从最容易")


@pytest.mark.asyncio
async def test_turn_endpoint_returns_structured_deadline_error(
    hidden_world,
    learner_state,
    public_scenario,
) -> None:
    class TimeoutRuntime:
        async def run_turn(self, _request):
            raise TurnDeadlineExceeded("turn deadline exceeded")

    request = AgentTurnRequest(
        request_id="request-timeout-http",
        session_id="session-1",
        state_revision=5,
        public_scenario=public_scenario,
        hidden_world=hidden_world,
        learner_state=learner_state,
        user_message="先看看 CPU",
    )
    app = create_app(TimeoutRuntime())

    async with AsyncClient(
        transport=ASGITransport(app=app, raise_app_exceptions=False),
        base_url="http://test",
    ) as client:
        response = await client.post("/turn", json=request.model_dump(mode="json"))

    assert response.status_code == 504
    assert response.json()["detail"]["code"] == "turn_deadline_exceeded"
