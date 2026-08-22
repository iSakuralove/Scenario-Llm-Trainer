"""HiddenWorld Agent 的 FastAPI 入口。"""

from __future__ import annotations

import logging
import os
from asyncio import Queue, create_task
from functools import lru_cache
from time import perf_counter
from typing import Any

from fastapi import FastAPI, HTTPException
from fastapi.responses import StreamingResponse
from pydantic_ai import models

from hiddenworld.agents.interpreter import create_interpreter_agent
from hiddenworld.agents.mentor import create_mentor_agent
from hiddenworld.agents.models import (
    GLM_FALLBACK_MODEL_ID,
    GLM_MODEL_ID,
    ModelConfigurationError,
    build_deepseek_model,
    build_glm_model,
    build_litellm_model,
    build_routed_model,
    build_xuan_model,
)
from hiddenworld.agents.scenario_agent import create_scenario_agent_runner
from hiddenworld.contracts import AgentTurnRequest, AgentTurnResult, ContractVersionMismatch
from hiddenworld.runtime import HiddenWorldRuntime, PublicBoundaryRejected, TurnDeadlineExceeded
from hiddenworld.scenario_runtime import SingleAgentRuntime

logger = logging.getLogger("hiddenworld.app")


def _structured_action_id(request: AgentTurnRequest) -> str:
    action = request.structured_user_action
    return action.action_id if action is not None else ""


def _quick_action(request: AgentTurnRequest) -> bool:
    return request.structured_user_action is not None and not request.user_message.strip()


def _log_turn_outcome(
    request: AgentTurnRequest,
    *,
    started_at: float,
    phase: str,
    outcome: str,
    error_code: str = "",
    error: BaseException | None = None,
    events_sent: int = 0,
    result_seen: bool = False,
    last_event: str = "",
    event_counts: dict[str, int] | None = None,
) -> None:
    detail = "" if error is None else str(error).replace("\n", " ")
    log_method = logger.info if outcome == "success" else logger.error
    log_method(
        "[hiddenworld-turn] request_id=%s session_id=%s state_revision=%s "
        "action_id=%s structured_action=%s quick_action=%s phase=%s outcome=%s "
        "elapsed_ms=%d budget_deadline_ms=%s events_sent=%d result_seen=%s "
        "last_event=%s event_counts=%s error_code=%s error_type=%s detail=%s",
        request.request_id,
        request.session_id,
        request.state_revision,
        _structured_action_id(request),
        request.structured_user_action is not None,
        _quick_action(request),
        phase,
        outcome,
        int((perf_counter() - started_at) * 1000),
        request.budget.deadline_ms,
        events_sent,
        result_seen,
        last_event,
        event_counts or {},
        error_code,
        type(error).__name__ if error is not None else "",
        detail,
        exc_info=(type(error), error, error.__traceback__) if error is not None else None,
    )


def create_app(runtime: HiddenWorldRuntime | None = None) -> FastAPI:
    app = FastAPI(title="HiddenWorld Agent", version="hiddenworld.v1")

    @app.get("/healthz")
    async def healthz() -> dict[str, str]:
        return {"status": "ok", "contract_version": "hiddenworld.v1"}

    @app.post("/turn", response_model=AgentTurnResult)
    async def run_turn(request: AgentTurnRequest) -> AgentTurnResult:
        started_at = perf_counter()
        phase = "runtime_init"
        try:
            active_runtime = runtime if runtime is not None else _runtime_from_env()
            phase = "runtime_run_turn"
            result = await active_runtime.run_turn(request)
            _log_turn_outcome(
                request,
                started_at=started_at,
                phase=phase,
                outcome="success",
                result_seen=True,
            )
            return result
        except ContractVersionMismatch as exc:
            _log_turn_outcome(
                request,
                started_at=started_at,
                phase=phase,
                outcome="failed",
                error_code="contract_version_mismatch",
                error=exc,
            )
            raise HTTPException(
                status_code=409,
                detail={"code": "contract_version_mismatch", "message": str(exc)},
            ) from exc
        except ModelConfigurationError as exc:
            _log_turn_outcome(
                request,
                started_at=started_at,
                phase=phase,
                outcome="failed",
                error_code="model_not_configured",
                error=exc,
            )
            raise HTTPException(
                status_code=503,
                detail={"code": "model_not_configured", "message": str(exc)},
            ) from exc
        except TurnDeadlineExceeded as exc:
            _log_turn_outcome(
                request,
                started_at=started_at,
                phase=phase,
                outcome="failed",
                error_code="turn_deadline_exceeded",
                error=exc,
            )
            raise HTTPException(
                status_code=504,
                detail={"code": "turn_deadline_exceeded", "message": str(exc)},
            ) from exc
        except PublicBoundaryRejected as exc:
            _log_turn_outcome(
                request,
                started_at=started_at,
                phase=phase,
                outcome="failed",
                error_code="public_boundary_rejected",
                error=exc,
            )
            raise HTTPException(
                status_code=502,
                detail={"code": "public_boundary_rejected", "message": str(exc)},
            ) from exc
        except Exception as exc:
            _log_turn_outcome(
                request,
                started_at=started_at,
                phase=phase,
                outcome="failed",
                error_code="agent_internal_error",
                error=exc,
            )
            raise

    @app.post("/turn/stream")
    async def stream_turn(request: AgentTurnRequest) -> StreamingResponse:
        active_runtime = runtime if runtime is not None else _runtime_from_env()

        async def event_stream():
            queue: Queue[tuple[str, Any]] = Queue(maxsize=512)
            started_at = perf_counter()
            phase = "stream_init"
            outcome = "cancelled"
            error_code = ""
            error: BaseException | None = None
            events_sent = 0
            result_seen = False
            last_event = ""
            event_counts: dict[str, int] = {}

            async def emit(name: str, payload: Any) -> None:
                nonlocal events_sent, last_event
                await queue.put((name, payload))
                events_sent += 1
                last_event = name
                event_counts[name] = event_counts.get(name, 0) + 1

            async def on_analysis(analysis) -> None:
                await emit("turn_analysis", analysis.model_dump(mode="json"))

            async def on_trace(event) -> None:
                await emit("public_trace", event.model_dump(mode="json"))

            async def on_reply_delta(piece: str) -> None:
                await emit("reply_delta", {"text": piece})

            async def on_reasoning_delta(piece: str) -> None:
                # 这是测试专用调试事件：Go 客户端会忽略它，Python 直连测试可以
                # 观察模型原始 ThinkingPartDelta；它不属于正式 RunEvent，也不会
                # 写入 AgentTurnResult.public_trace 或会话历史。
                if _raw_reasoning_stream_enabled() and piece:
                    await emit("reasoning_raw_delta", {"text": piece})

            async def produce() -> None:
                nonlocal error, error_code, outcome, phase, result_seen
                try:
                    phase = "runtime_run_turn"
                    run_kwargs = {
                        "on_turn_analysis": on_analysis,
                        "on_public_trace": on_trace,
                        "on_reply_delta": on_reply_delta,
                    }
                    if _raw_reasoning_stream_enabled():
                        run_kwargs["on_reasoning_delta"] = on_reasoning_delta
                    result = await active_runtime.run_turn(request, **run_kwargs)
                    result_seen = True
                    phase = "result_enqueued"
                    await emit("result", result.model_dump(mode="json"))
                    outcome = "success"
                except ContractVersionMismatch as exc:
                    error = exc
                    error_code = "contract_version_mismatch"
                    phase = "runtime_run_turn"
                    outcome = "failed"
                    await emit("error", _stream_error(error_code, str(exc)))
                except ModelConfigurationError as exc:
                    error = exc
                    error_code = "model_not_configured"
                    phase = "runtime_run_turn"
                    outcome = "failed"
                    await emit("error", _stream_error(error_code, str(exc)))
                except TurnDeadlineExceeded as exc:
                    error = exc
                    error_code = "turn_deadline_exceeded"
                    phase = "runtime_run_turn"
                    outcome = "failed"
                    await emit("error", _stream_error(error_code, str(exc)))
                except PublicBoundaryRejected as exc:
                    error = exc
                    error_code = "public_boundary_rejected"
                    phase = "runtime_run_turn"
                    outcome = "failed"
                    await emit("error", _stream_error(error_code, str(exc)))
                except Exception as exc:
                    error = exc
                    error_code = "agent_internal_error"
                    phase = "runtime_run_turn"
                    outcome = "failed"
                    await emit(
                        "error",
                        _stream_error(
                            error_code,
                            f"HiddenWorld Agent 本轮处理失败：{type(exc).__name__}",
                        ),
                    )
                finally:
                    _log_turn_outcome(
                        request,
                        started_at=started_at,
                        phase=phase,
                        outcome=outcome,
                        error_code=error_code,
                        error=error,
                        events_sent=events_sent,
                        result_seen=result_seen,
                        last_event=last_event,
                        event_counts=event_counts,
                    )
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


def _raw_reasoning_stream_enabled() -> bool:
    """仅在显式测试开关打开时允许原始思维增量走调试 SSE。"""

    return os.getenv("HIDDENWORLD_TEST_STREAM_RAW_REASONING", "0").strip() == "1"


def _encode_sse(name: str, payload: Any) -> str:
    from pydantic_core import to_json

    return f"event: {name}\ndata: {to_json(payload).decode('utf-8')}\n\n"


@lru_cache(maxsize=1)
def _runtime_from_env() -> HiddenWorldRuntime | SingleAgentRuntime:
    if os.getenv("HIDDENWORLD_ALLOW_MODEL_REQUESTS", "0") != "1":
        raise ModelConfigurationError("real model requests are disabled")
    models.ALLOW_MODEL_REQUESTS = True
    # 单 ScenarioAgent 是生产默认入口。旧的 Interpreter+Mentor 双节点只在
    # 明确设置 runtime mode=legacy 时启用，避免部署遗漏一个新 provider 环境变量
    # 就静默退回已经冻结的旧主链。
    runtime_mode = os.getenv("HIDDENWORLD_RUNTIME_MODE", "single_agent").strip().lower()
    if runtime_mode in {"legacy", "dual", "interpreter_mentor", "v1"}:
        return HiddenWorldRuntime(
            interpreter=create_interpreter_agent(_model_for_provider("HIDDENWORLD_INTERPRETER_PROVIDER")),
            mentor=create_mentor_agent(_model_for_provider("HIDDENWORLD_MENTOR_PROVIDER")),
        )
    if runtime_mode not in {"single_agent", "single", "scenario_agent", "agent"}:
        raise ModelConfigurationError(
            "unsupported runtime mode configured: "
            f"{runtime_mode}; expected single_agent or explicit legacy"
        )

    provider = os.getenv("HIDDENWORLD_AGENT_PROVIDER", "glm").strip() or "glm"
    # 一个模型节点负责理解、工具规划和最终回复；Runtime 本地执行确定性工具、
    # 状态归约和 Guard，不再额外调用 Interpreter/Mentor。
    model = _model_for_provider_value(provider, role="AGENT")
    fallback_model = _fallback_model_for_provider(provider)
    return SingleAgentRuntime(create_scenario_agent_runner(model, fallback_model=fallback_model))


def _model_for_provider(env_name: str):
    provider = os.getenv(env_name, "glm").strip().lower()
    # 角色模型由统一分派函数按 provider 读取；旧 LiteLLM 入口仍兼容各角色别名。
    role = env_name.removeprefix("HIDDENWORLD_").removesuffix("_PROVIDER")
    return _model_for_provider_value(provider, role=role)


def _model_for_provider_value(provider: str, *, role: str = "AGENT"):
    provider = provider.strip().lower()
    role_model = os.getenv(f"GLM_{role}_MODEL") or os.getenv("GLM_MODEL")
    if provider in {"routes", "router", "llm_routes"}:
        return build_routed_model()
    if provider == "deepseek":
        return build_deepseek_model()
    if provider in {"glm", "zai"}:
        return build_glm_model(model=role_model)
    if provider == "xuan":
        return build_xuan_model()
    if provider in {"litellm", "gateway"}:
        # 兼容旧部署配置；新部署不应再使用 LiteLLM。
        legacy_model = os.getenv(f"LITELLM_{role}_MODEL") or os.getenv("LITELLM_MODEL")
        return build_litellm_model(model=legacy_model)
    raise ModelConfigurationError(f"unsupported provider configured: {provider}")


def _fallback_model_for_provider(provider: str):
    """构造显式 GLM 回退模型；没有凭证时不在启动阶段改变既有错误。"""

    if provider not in {"glm", "zai"}:
        return None
    fallback_id = (os.getenv("GLM_FALLBACK_MODEL") or GLM_FALLBACK_MODEL_ID).strip()
    primary_id = (os.getenv("GLM_AGENT_MODEL") or os.getenv("GLM_MODEL") or GLM_MODEL_ID).strip()
    if not fallback_id or fallback_id.casefold() == primary_id.casefold():
        return None
    if not (os.getenv("ZAI_API_KEY", "").strip() or os.getenv("GLM_API_KEY", "").strip()):
        return None
    try:
        return build_glm_model(model=fallback_id)
    except ModelConfigurationError:
        logger.warning("GLM fallback model is not configured; continuing without fallback")
        return None


app = create_app()
