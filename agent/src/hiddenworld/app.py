"""HiddenWorld Agent 的 FastAPI 入口。"""

from __future__ import annotations

import os
from functools import lru_cache

from fastapi import FastAPI, HTTPException
from pydantic_ai import models

from hiddenworld.agents.interpreter import create_interpreter_agent
from hiddenworld.agents.mentor import create_mentor_agent
from hiddenworld.agents.models import (
    ModelConfigurationError,
    build_deepseek_model,
    build_glm_model,
)
from hiddenworld.contracts import AgentTurnRequest, AgentTurnResult, ContractVersionMismatch
from hiddenworld.runtime import HiddenWorldRuntime, TurnDeadlineExceeded


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

    return app


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
    provider = os.getenv(env_name, "deepseek").strip().lower()
    if provider == "deepseek":
        return build_deepseek_model()
    if provider in {"glm", "zai"}:
        return build_glm_model()
    raise ModelConfigurationError(f"unsupported provider configured in {env_name}")


app = create_app()
