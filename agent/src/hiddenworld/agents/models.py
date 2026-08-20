"""锁定的一期模型构造器；API Key 只从参数或环境变量读取。"""

from __future__ import annotations

import os

from openai import AsyncOpenAI
from pydantic_ai.models.openai import OpenAIChatModel
from pydantic_ai.models.zai import ZaiModel
from pydantic_ai.providers.deepseek import DeepSeekProvider
from pydantic_ai.providers.openai import OpenAIProvider
from pydantic_ai.providers.zai import ZaiProvider

DEEPSEEK_MODEL_ID = "deepseek-v4-flash"
GLM_MODEL_ID = "glm-4.7"
ZAI_BASE_URL = "https://api.z.ai/api/paas/v4"
GLM_BASE_URL = "https://open.bigmodel.cn/api/paas/v4"
XUAN_BASE_URL = "https://ai.centos.hk/v1"
LITELLM_BASE_URL = "http://127.0.0.1:4000/v1"
# 白名单只做规范化后的精确匹配（小写），配置值大小写不敏感；
# 但发往中转站的模型 ID 大小写敏感，须保留用户配置的原始写法。
XUAN_MODEL_IDS = ("gpt-5.6-terra", "grok-4.5", "deepseek-v4-flash-0731")
# LiteLLM 网关别名：切模型只改 LITELLM_MODEL 或网关 config.yaml 的别名指向，
# agent 代码不动。语义别名（interpreter-default 等）在网关侧维护。
LITELLM_MODEL_ALIASES = (
    "interpreter-default",
    "mentor-default",
    "generator-default",
    "deepseek-flash",
    "glm-relay",
    "glm-direct",
    "gpt-5.6-terra",
    "gpt-5.6-sol",
    "gpt-5.6-luna",
    "gpt-5.5",
    "grok-4.5",
    "claude-sonnet-5",
    "claude-opus-5",
    "claude-haiku-4-5",
    "fallback-primary",
)


class ModelConfigurationError(ValueError):
    """模型凭证缺失；错误中不包含凭证值。"""


def build_deepseek_model(*, api_key: str | None = None) -> OpenAIChatModel:
    key = api_key or os.getenv("DEEPSEEK_API_KEY")
    if not key:
        raise ModelConfigurationError("DEEPSEEK_API_KEY is required")
    client = AsyncOpenAI(
        api_key=key,
        base_url="https://api.deepseek.com",
        max_retries=0,
    )
    return OpenAIChatModel(
        DEEPSEEK_MODEL_ID,
        provider=DeepSeekProvider(openai_client=client),
    )


def build_glm_model(*, api_key: str | None = None) -> ZaiModel:
    explicit_key = (api_key or "").strip()
    zai_key = (os.getenv("ZAI_API_KEY") or "").strip()
    glm_key = (os.getenv("GLM_API_KEY") or "").strip()
    if explicit_key:
        key = explicit_key
        base_url = (
            os.getenv("ZAI_BASE_URL")
            or os.getenv("GLM_BASE_URL")
            or ZAI_BASE_URL
        ).strip()
    elif zai_key:
        key = zai_key
        base_url = (os.getenv("ZAI_BASE_URL") or ZAI_BASE_URL).strip()
    elif glm_key:
        key = glm_key
        base_url = (os.getenv("GLM_BASE_URL") or GLM_BASE_URL).strip()
    else:
        raise ModelConfigurationError("ZAI_API_KEY or GLM_API_KEY is required")
    client = AsyncOpenAI(
        api_key=key,
        base_url=base_url,
        max_retries=0,
    )
    return ZaiModel(
        GLM_MODEL_ID,
        provider=ZaiProvider(openai_client=client),
        settings={"thinking": False},
    )


def build_litellm_model(
    *,
    api_key: str | None = None,
    base_url: str | None = None,
    model: str | None = None,
) -> OpenAIChatModel:
    """构造 LiteLLM 网关模型：所有上游统一经网关别名路由。"""

    key = api_key or os.getenv("LITELLM_API_KEY")
    if not key:
        raise ModelConfigurationError("LITELLM_API_KEY is required")
    configured = (model or os.getenv("LITELLM_MODEL") or "mentor-default").strip()
    if configured.lower() not in LITELLM_MODEL_ALIASES:
        allowed = ", ".join(LITELLM_MODEL_ALIASES)
        raise ModelConfigurationError(f"LITELLM_MODEL must be one of: {allowed}")
    endpoint = (base_url or os.getenv("LITELLM_BASE_URL") or LITELLM_BASE_URL).strip()
    client = AsyncOpenAI(
        api_key=key.strip(),
        base_url=endpoint,
        max_retries=0,
    )
    return OpenAIChatModel(
        configured,
        provider=OpenAIProvider(openai_client=client),
    )


def build_xuan_model(
    *,
    api_key: str | None = None,
    base_url: str | None = None,
    model: str | None = None,
) -> OpenAIChatModel:
    """构造宣安中转站的 OpenAI Chat Completions 兼容模型。"""

    key = api_key or os.getenv("XUAN_API_KEY")
    if not key:
        raise ModelConfigurationError("XUAN_API_KEY is required")
    # 中转站模型 ID 大小写敏感：白名单比较用规范化值，发送给上游的保持
    # 用户配置的原始写法（.env 里是 DeepSeek-V4-Flash-0731）。
    configured = (model or os.getenv("XUAN_MODEL") or XUAN_MODEL_IDS[0]).strip().replace("_", "-")
    normalized = configured.lower()
    if normalized not in XUAN_MODEL_IDS:
        allowed = ", ".join(XUAN_MODEL_IDS)
        raise ModelConfigurationError(f"XUAN_MODEL must be one of: {allowed}")
    model_id = configured
    endpoint = (base_url or os.getenv("XUAN_BASE_URL") or XUAN_BASE_URL).strip()
    client = AsyncOpenAI(
        api_key=key.strip(),
        base_url=endpoint,
        max_retries=0,
    )
    return OpenAIChatModel(
        model_id,
        provider=OpenAIProvider(openai_client=client),
    )
