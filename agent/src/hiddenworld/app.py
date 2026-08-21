"""HiddenWorld Agent 的 FastAPI 入口。"""

from __future__ import annotations

import logging
import os
from asyncio import Queue, create_task
from functools import lru_cache
from typing import Any

from fastapi import FastAPI, HTTPException
from fastapi.responses import StreamingResponse
from pydantic_ai import models

from hiddenworld.agents.interpreter import create_interpreter_agent
from hiddenworld.agents.mentor import create_mentor_agent
from hiddenworld.agents.models import (
    ModelConfigurationError,
    build_deepseek_model,
    build_glm_model,
    build_litellm_model,
    build_routed_model,
    build_xuan_model,
)
from hiddenworld.contracts import AgentTurnRequest, AgentTurnResult, ContractVersionMismatch
from hiddenworld.runtime import HiddenWorldRuntime, TurnDeadlineExceeded

logger = logging.getLogger("hiddenworld.app")


def create_app(runtime: HiddenWorldRuntime | None = None) -> FastAPI:
    app = FastAPI(title="HiddenWorld Agent", version="hiddenworld.v1")

    @app.get("/healthz")
    async def healthz() -> dict[str, str]:
        return {"status": "ok", "contract_version": "hiddenworld.v1"}

    @app.post("/turn", response_model=AgentTurnResult)
    async def run_turn(request: AgentTurnRequest) -> AgentTurnResult:
        try:
            active_runtime = runtime if runtime is not None else _runtime_from_env()
            return await active_runtime.run_turn(request)
        except ContractVersionMismatch as exc:
            raise HTTPException(
                status_code=409,
                detail={"code": "contract_version_mismatch", "message": str(exc)},
            ) from exc
        except ModelConfigurationError as exc:
            raise HTTPException(
                status_code=503,
                detail={"code": "model_not_configured", "message": str(exc)},
            ) from exc
        except TurnDeadlineExceeded as exc:
            raise HTTPException(
                status_code=504,
                detail={"code": "turn_deadline_exceeded", "message": str(exc)},
            ) from exc

    @app.post("/turn/stream")
    async def stream_turn(request: AgentTurnRequest) -> StreamingResponse:
        active_runtime = runtime if runtime is not None else _runtime_from_env()

        async def event_stream():
            queue: Queue[tuple[str, Any]] = Queue(maxsize=512)

            async def on_analysis(analysis) -> None:
                await queue.put(("turn_analysis", analysis.model_dump(mode="json")))

            async def on_trace(event) -> None:
                await queue.put(("public_trace", event.model_dump(mode="json")))

            async def on_reply_delta(piece: str) -> None:
                await queue.put(("reply_delta", {"text": piece}))

            async def produce() -> None:
                try:
                    result = await active_runtime.run_turn(
                        request,
                        on_turn_analysis=on_analysis,
                        on_public_trace=on_trace,
                        on_reply_delta=on_reply_delta,
                    )
                    await queue.put(("result", result.model_dump(mode="json")))
                except ContractVersionMismatch as exc:
                    await queue.put(("error", _stream_error("contract_version_mismatch", str(exc))))
                except ModelConfigurationError as exc:
                    await queue.put(("error", _stream_error("model_not_configured", str(exc))))
                except TurnDeadlineExceeded as exc:
                    await queue.put(("error", _stream_error("turn_deadline_exceeded", str(exc))))
                except Exception as exc:
                    logger.exception("agent turn failed: request_id=%s", request.request_id)
                    await queue.put(
                        (
                            "error",
                            _stream_error(
                                "agent_internal_error",
                                f"HiddenWorld Agent 本轮处理失败：{type(exc).__name__}",
                            ),
                        )
                    )
                finally:
                    await queue.put(("done", None))

            producer = create_task(produce())
            try:
                while True:
                    name, payload = await queue.get()
                    if name == "done":
                        break
                    yield _encode_sse(name, payload)
            finally:
                if not producer.done():
                    producer.cancel()
                try:
                    await producer
                except BaseException:
                    pass

        return StreamingResponse(
            event_stream(),
            media_type="text/event-stream",
            headers={
                "Cache-Control": "no-cache",
                "X-Accel-Buffering": "no",
                "Connection": "keep-alive",
            },
        )

    return app


def _stream_error(code: str, message: str) -> dict[str, str]:
    return {"code": code, "message": message}


def _encode_sse(name: str, payload: Any) -> str:
    from pydantic_core import to_json

    return f"event: {name}\ndata: {to_json(payload).decode('utf-8')}\n\n"


@lru_cache(maxsize=1)
def _runtime_from_env() -> HiddenWorldRuntime:
    if os.getenv("HIDDENWORLD_ALLOW_MODEL_REQUESTS", "0") != "1":
        raise ModelConfigurationError("real model requests are disabled")
    models.ALLOW_MODEL_REQUESTS = True
    return HiddenWorldRuntime(
        interpreter=create_interpreter_agent(_model_for_provider("HIDDENWORLD_INTERPRETER_PROVIDER")),
        mentor=create_mentor_agent(_model_for_provider("HIDDENWORLD_MENTOR_PROVIDER")),
    )


def _model_for_provider(env_name: str):
    provider = os.getenv(env_name, "glm").strip().lower()
    # litellm 网关支持按角色选别名：HIDDENWORLD_INTERPRETER_PROVIDER →
    # LITELLM_INTERPRETER_MODEL，HIDDENWORLD_MENTOR_PROVIDER →
    # LITELLM_MENTOR_MODEL（缺省落 LITELLM_MODEL → mentor-default）。
    role = env_name.removeprefix("HIDDENWORLD_").removesuffix("_PROVIDER")
    role_model = os.getenv(f"LITELLM_{role}_MODEL") or os.getenv("LITELLM_MODEL")
    if provider in {"routes", "router", "llm_routes"}:
        return build_routed_model()
    if provider == "deepseek":
        return build_deepseek_model()
    if provider in {"glm", "zai"}:
        return build_glm_model()
    if provider == "xuan":
        return build_xuan_model()
    if provider in {"litellm", "gateway"}:
        return build_litellm_model(model=role_model)
    raise ModelConfigurationError(f"unsupported provider configured in {env_name}")


app = create_app()
