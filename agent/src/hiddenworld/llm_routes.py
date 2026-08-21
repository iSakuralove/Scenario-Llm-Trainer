"""llm_routes.yaml 顺序路由 —— LiteLLM 网关的薄替代。

配置文件（仓库根 config/llm_routes.yaml，容器内由 LLM_ROUTES_FILE 指向）：
按 providers 声明顺序尝试站点，key 留空（${VAR} 未设置）自动跳过，
默认请求第一个有 key 的站点。全部条目走 OpenAI Chat Completions 兼容协议，
官方 GLM / DeepSeek / MiniMax 与任意中转站只差 base_url 和 model。

与 backend/internal/ai/llm_routes.go 保持同一份语义：
  - ${VAR} 引用环境变量，key 不落盘
  - 站点失败（限流 / 网络 / 鉴权 / 5xx）自动切下一站，全部失败抛最后一个错误
  - 官方域名缺省 base_url / model 时自动补默认值
"""

from __future__ import annotations

import logging
import os
import re
from dataclasses import dataclass, field
from typing import Any

import yaml
from openai import AsyncOpenAI

from .agents.models import ModelConfigurationError

logger = logging.getLogger("hiddenworld.llm_routes")

_ENV_PATTERN = re.compile(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}")

DEFAULT_ROUTES_PATH = "llm_routes.yaml"

# 官方站点预置默认值：配置文件只填 api_key 也能跑。
_VENDOR_BASE_URLS = {
    "glm": "https://open.bigmodel.cn/api/paas/v4",
    "deepseek": "https://api.deepseek.com",
    "minimax": "https://api.minimax.io/v1",
}
_VENDOR_MODELS = {
    "glm": "glm-4.7",
    "deepseek": "deepseek-v4-flash",
    "minimax": "MiniMax-M2.7",
}
# 官方文档标注的最大输出 token：GLM-4.7 128K、DeepSeek-V4 384K、
# MiniMax-M2.7 最大 128K（通常 16K 已够用）。
_VENDOR_MAX_TOKENS = {
    "glm": 131072,
    "deepseek": 393216,
    "minimax": 131072,
}
_PLACEHOLDER_NAMES = ("base_url", "api_key", "model")


@dataclass
class RouteCandidate:
    """一个可用的 OpenAI 兼容站点。"""

    name: str
    base_url: str
    api_key: str
    model: str
    max_tokens: int = 0
    extra_headers: dict[str, str] = field(default_factory=dict)
    client: AsyncOpenAI | None = None

    def build_client(self, timeout: float) -> AsyncOpenAI:
        if self.client is None:
            self.client = AsyncOpenAI(
                api_key=self.api_key,
                base_url=self.base_url,
                timeout=timeout,
                max_retries=0,
                default_headers=self.extra_headers or None,
            )
        return self.client


def _interpolate(value: str) -> str:
    return _ENV_PATTERN.sub(
        lambda m: os.environ.get(m.group(1), m.group(0)),
        value,
    )


def _is_unresolved(value: str) -> bool:
    return bool(_ENV_PATTERN.search(value.strip()))


def _vendor_key(text: str, name: str) -> str | None:
    blob = f"{name} {text}".lower()
    for vendor in _VENDOR_BASE_URLS:
        if vendor in blob:
            return vendor
    return None


def load_llm_routes(path: str | None = None) -> list[RouteCandidate]:
    """读取路由配置；返回按声明顺序排列的可用站点（空 key 已跳过）。"""

    resolved = (path or os.getenv("LLM_ROUTES_FILE") or DEFAULT_ROUTES_PATH).strip()
    if not os.path.exists(resolved):
        raise ModelConfigurationError(f"llm routes file not found: {resolved}")
    with open(resolved, encoding="utf-8") as fh:
        parsed = yaml.safe_load(fh) or {}
    providers = parsed.get("providers") if isinstance(parsed, dict) else None
    if not isinstance(providers, list) or not providers:
        raise ModelConfigurationError(f"llm routes file {resolved} declares no providers")

    candidates: list[RouteCandidate] = []
    skipped: list[str] = []
    seen: set[str] = set()
    for index, item in enumerate(providers):
        if not isinstance(item, dict):
            continue
        name = str(_interpolate(str(item.get("name", "")))).strip() or f"llm-route-{index + 1}"
        if name in seen:
            raise ModelConfigurationError(f"llm routes {resolved}: duplicate provider name {name!r}")
        seen.add(name)

        values = {
            key: _interpolate(str(item.get(key, "") or "")).strip()
            for key in _PLACEHOLDER_NAMES
        }
        api_key = values["api_key"]
        # key 留空 = 该站不参与路由；默认请求第一个有 key 的站点。
        if api_key == "" or _is_unresolved(api_key):
            skipped.append(name)
            continue

        vendor = _vendor_key(values["base_url"], name)
        base_url = values["base_url"] or (_VENDOR_BASE_URLS.get(vendor or "", ""))
        model = values["model"] or (_VENDOR_MODELS.get(vendor or "", ""))
        if not base_url or not model:
            raise ModelConfigurationError(
                f"llm routes {resolved}: provider {name!r} requires base_url and model"
            )
        extra_headers = {
            str(k).strip(): _interpolate(str(v)).strip()
            for k, v in (item.get("extra_headers") or {}).items()
            if str(k).strip() and str(v).strip()
        }
        max_tokens = int(item.get("max_tokens") or 0)
        if max_tokens <= 0:
            max_tokens = _VENDOR_MAX_TOKENS.get(vendor or "", 0)
        candidates.append(
            RouteCandidate(
                name=name,
                base_url=base_url,
                api_key=api_key,
                model=model,
                max_tokens=max_tokens,
                extra_headers=extra_headers,
            )
        )

    if not candidates:
        raise ModelConfigurationError(
            f"llm routes {resolved}: all providers skipped (missing api_key): {skipped} — "
            "填好任一 ${VAR} 后生效"
        )
    if skipped:
        logger.info("llm routes skipped (no api_key): %s", ", ".join(skipped))
    return candidates


class _FallbackCompletions:
    def __init__(self, router: "OrderedFallbackRouter") -> None:
        self._router = router

    async def create(self, **kwargs: Any) -> Any:
        # pydantic_ai 可能携带自己的 model 参数；每个站点必须用自己的 model。
        kwargs.pop("model", None)
        return await self._router.create(**kwargs)


class _FallbackChat:
    def __init__(self, router: "OrderedFallbackRouter") -> None:
        self.completions = _FallbackCompletions(router)


class OrderedFallbackRouter:
    """鸭子类型的 AsyncOpenAI 替身：按声明顺序在站点间故障转移。

    pydantic_ai 的 OpenAIChatModel 只访问 client.base_url 与
    client.chat.completions.create，因此这里只需实现这两个面。
    """

    def __init__(self, candidates: list[RouteCandidate], timeout: float = 120.0) -> None:
        self._candidates = candidates
        self._timeout = timeout
        self.chat = _FallbackChat(self)
        # base_url 用第一个真实客户端的值（httpx.URL），保持日志/诊断输出一致。
        self.base_url = candidates[0].build_client(timeout).base_url

    @property
    def candidate_names(self) -> list[str]:
        return [candidate.name for candidate in self._candidates]

    async def create(self, **kwargs: Any) -> Any:
        last_error: Exception | None = None
        for index, candidate in enumerate(self._candidates):
            client = candidate.build_client(self._timeout)
            if candidate.max_tokens > 0:
                kwargs.setdefault("max_tokens", candidate.max_tokens)
            try:
                return await client.chat.completions.create(model=candidate.model, **kwargs)
            except Exception as exc:  # noqa: BLE001 —— 任何站点错误都切下一站
                last_error = exc
                remaining = len(self._candidates) - index - 1
                logger.warning(
                    "llm route %s failed (%s: %s)%s",
                    candidate.name,
                    type(exc).__name__,
                    str(exc)[:200],
                    f"，切换下一站" if remaining else "，已无下一站",
                )
        assert last_error is not None
        raise last_error
