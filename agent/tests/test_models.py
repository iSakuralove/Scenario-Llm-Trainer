import pytest
from pydantic_ai.models.openai import OpenAIChatModel
from pydantic_ai.models.zai import ZaiModel

from hiddenworld.agents.models import build_deepseek_model, build_glm_model, build_xuan_model


def test_deepseek_model_uses_locked_id_and_disables_sdk_retries() -> None:
    model = build_deepseek_model(api_key="test-deepseek-key")

    assert isinstance(model, OpenAIChatModel)
    assert model.model_name == "deepseek-v4-flash"
    assert model.system == "deepseek"
    assert model.client.max_retries == 0


def test_glm_model_uses_native_zai_model_and_disables_sdk_retries() -> None:
    model = build_glm_model(api_key="test-zai-key")

    assert isinstance(model, ZaiModel)
    assert model.model_name == "glm-4.7"
    assert model.system == "zai"
    assert model.client.max_retries == 0


def test_glm_model_accepts_project_glm_environment_alias(monkeypatch) -> None:
    monkeypatch.delenv("ZAI_API_KEY", raising=False)
    monkeypatch.setenv("GLM_API_KEY", "test-glm-key")
    monkeypatch.setenv("GLM_BASE_URL", "https://open.bigmodel.cn/api/paas/v4")

    model = build_glm_model()

    assert str(model.client.base_url) == "https://open.bigmodel.cn/api/paas/v4/"


@pytest.mark.parametrize("model_id", ["grok-4.5", "deepseek-v4-flash-0731"])
def test_xuan_model_uses_openai_compatible_route_and_locked_models(model_id: str) -> None:
    model = build_xuan_model(
        api_key="test-xuan-key",
        base_url="https://elysiver.h-e.top/",
        model=model_id,
    )

    assert isinstance(model, OpenAIChatModel)
    assert model.model_name == model_id
    assert model.system == "openai"
    assert str(model.client.base_url) == "https://elysiver.h-e.top/"
    assert model.client.max_retries == 0


def test_xuan_model_rejects_unknown_model() -> None:
    with pytest.raises(ValueError, match="XUAN_MODEL must be one of"):
        build_xuan_model(api_key="test-xuan-key", model="unknown-model")
