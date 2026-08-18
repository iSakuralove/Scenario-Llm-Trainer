"""模型网络错误的有界应用层重试。"""

from __future__ import annotations

import asyncio
import random
from collections.abc import Awaitable, Callable
from typing import TypeVar

import httpx
from pydantic_ai.exceptions import ModelHTTPError

T = TypeVar("T")
_RETRYABLE_HTTP_STATUS = {500, 503}
_RETRYABLE_429_CODES = {
    "rate_limit_exceeded",
    "too_many_requests",
    "server_busy",
    "overloaded",
}


async def run_with_network_retries(
    operation: Callable[[], Awaitable[T]],
    *,
    max_retries: int = 2,
    base_delay: float = 0.1,
    jitter: float = 0.05,
    sleep: Callable[[float], Awaitable[None]] = asyncio.sleep,
) -> T:
    """初次调用外最多重试两次；不重试 schema、权限、余额或参数错误。"""

    attempt = 0
    while True:
        try:
            return await operation()
        except Exception as exc:
            if attempt >= max_retries or not _is_retryable(exc):
                raise
            delay = base_delay * (2**attempt)
            if jitter > 0:
                delay += random.uniform(0, jitter)
            attempt += 1
            await sleep(delay)


def _is_retryable(exc: Exception) -> bool:
    if isinstance(exc, (httpx.ConnectError, httpx.TimeoutException)):
        return True
    if not isinstance(exc, ModelHTTPError):
        return False
    if exc.status_code in _RETRYABLE_HTTP_STATUS:
        return True
    if exc.status_code != 429:
        return False
    return _retryable_429_body(exc.body)


def _retryable_429_body(body: object | None) -> bool:
    if not isinstance(body, dict):
        return False
    values = _flatten_strings(body)
    return any(value.casefold() in _RETRYABLE_429_CODES for value in values)


def _flatten_strings(value: object) -> list[str]:
    if isinstance(value, str):
        return [value]
    if isinstance(value, dict):
        result: list[str] = []
        for nested in value.values():
            result.extend(_flatten_strings(nested))
        return result
    if isinstance(value, list):
        result = []
        for nested in value:
            result.extend(_flatten_strings(nested))
        return result
    return []
