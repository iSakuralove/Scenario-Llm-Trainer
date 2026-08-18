import httpx
import pytest
from pydantic_ai.exceptions import ModelHTTPError

from hiddenworld.retry import run_with_network_retries


@pytest.mark.asyncio
async def test_retry_replays_retryable_connection_error_at_most_twice() -> None:
    attempts = 0
    delays: list[float] = []

    async def operation() -> str:
        nonlocal attempts
        attempts += 1
        if attempts < 3:
            raise httpx.ConnectError("temporary disconnect")
        return "ok"

    async def fake_sleep(delay: float) -> None:
        delays.append(delay)

    result = await run_with_network_retries(operation, sleep=fake_sleep, jitter=0)

    assert result == "ok"
    assert attempts == 3
    assert delays == [0.1, 0.2]


@pytest.mark.asyncio
async def test_retry_does_not_replay_non_retryable_http_error() -> None:
    attempts = 0

    async def operation() -> None:
        nonlocal attempts
        attempts += 1
        raise ModelHTTPError(400, "test-model", {"error": "invalid request"})

    with pytest.raises(ModelHTTPError):
        await run_with_network_retries(operation)

    assert attempts == 1
