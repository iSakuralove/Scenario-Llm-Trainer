"""锁定的一期模型构造器；API Key 只从参数或环境变量读取。"""

from __future__ import annotations

import os

from openai import AsyncOpenAI
from pydantic_ai.models.openai import OpenAIChatModel
from pydantic_ai.models.zai import ZaiModel
from pydantic_ai.providers.deepseek import DeepSeekProvider
from pydantic_ai.providers.zai import ZaiProvider

DEEPSEEK_MODEL_ID = "deepseek-v4-flash"
GLM_MODEL_ID = "glm-4.7"


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
    key = api_key or os.getenv("ZAI_API_KEY") or os.getenv("GLM_API_KEY")
    if key:
        key = key.strip()
    if not key:
        raise ModelConfigurationError("ZAI_API_KEY or GLM_API_KEY is required")
    base_url = (
        os.getenv("ZAI_BASE_URL")
        or os.getenv("GLM_BASE_URL")
        or "https://api.z.ai/api/paas/v4"
    ).strip()
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
