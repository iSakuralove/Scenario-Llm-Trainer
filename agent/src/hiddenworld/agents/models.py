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
XUAN_MODEL_IDS = ("gpt-5.6-terra", "grok-4.5", "deepseek-v4-flash-0731")


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
    model_id = (model or os.getenv("XUAN_MODEL") or XUAN_MODEL_IDS[0]).strip()
    if model_id not in XUAN_MODEL_IDS:
        allowed = ", ".join(XUAN_MODEL_IDS)
        raise ModelConfigurationError(f"XUAN_MODEL must be one of: {allowed}")
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
