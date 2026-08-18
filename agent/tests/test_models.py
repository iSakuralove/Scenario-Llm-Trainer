from pydantic_ai.models.openai import OpenAIChatModel
from pydantic_ai.models.zai import ZaiModel

from hiddenworld.agents.models import build_deepseek_model, build_glm_model


def test_deepseek_model_uses_locked_id_and_disables_sdk_retries() -> None:
    model = build_deepseek_model(api_key="test-deepseek-key")

    assert isinstance(model, OpenAIChatModel)
    assert model.model_name == "deepseek-v4-flash"
    assert model.system == "deepseek"
    assert model.client.max_retries == 0


def test_glm_model_uses_native_zai_model_and_disables_sdk_retries() -> None:
    model = build_glm_model(api_key="test-zai-key")

    assert isinstance(model, ZaiModel)
    assert model.model_name == "glm-5.2"
    assert model.system == "zai"
    assert model.client.max_retries == 0
