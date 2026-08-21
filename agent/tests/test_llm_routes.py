"""llm_routes.yaml 顺序路由的加载与故障转移语义。"""

from __future__ import annotations

import os
from types import SimpleNamespace

import pytest

from hiddenworld.agents.models import ModelConfigurationError
from hiddenworld.llm_routes import OrderedFallbackRouter, _FallbackChat, load_llm_routes


def _write_routes(tmp_path, content: str) -> str:
    path = tmp_path / "llm_routes.yaml"
    path.write_text(content, encoding="utf-8")
    return str(path)


def test_load_skips_empty_key_and_keeps_order(tmp_path, monkeypatch):
    monkeypatch.setenv("TEST_GLM_KEY", "sk-glm")
    path = _write_routes(
        tmp_path,
        """
providers:
  - name: glm-official
    base_url: https://open.bigmodel.cn/api/paas/v4
    api_key: ${TEST_GLM_KEY}
    model: glm-4.7
  - name: minimax-official
    base_url: https://api.minimax.io/v1
    api_key: ${MINIMAX_API_KEY_NOT_SET}
    model: MiniMax-M2.7
  - name: deepseek-official
    api_key: ${TEST_GLM_KEY}
""",
    )
    routes = load_llm_routes(path)
    assert [route.name for route in routes] == ["glm-official", "deepseek-official"]
    assert routes[1].base_url == "https://api.deepseek.com"
    assert routes[1].model == "deepseek-v4-flash"


def test_load_fills_vendor_defaults(tmp_path, monkeypatch):
    monkeypatch.setenv("TEST_KEY", "sk-x")
    path = _write_routes(
        tmp_path,
        """
providers:
  - name: glm-official
    api_key: ${TEST_KEY}
  - name: minimax-official
    api_key: sk-y
    model: MiniMax-M9
""",
    )
    routes = load_llm_routes(path)
    assert routes[0].base_url.endswith("/api/paas/v4")
    assert routes[0].model == "glm-4.7"
    assert routes[1].model == "MiniMax-M9"


def test_load_rejects_when_all_keys_missing(tmp_path):
    path = _write_routes(
        tmp_path,
        """
providers:
  - name: glm-official
    api_key: ${NOPE_NOT_SET}
""",
    )
    with pytest.raises(ModelConfigurationError):
        load_llm_routes(path)


def test_load_rejects_unknown_site_without_model(tmp_path):
    path = _write_routes(
        tmp_path,
        """
providers:
  - name: my-proxy
    base_url: https://example.com/v1
    api_key: sk-x
""",
    )
    with pytest.raises(ModelConfigurationError):
        load_llm_routes(path)


def test_fallback_router_tries_candidates_in_order(monkeypatch):
    calls: list[str] = []

    def make_client(name: str, fail: bool):
        async def create(**kwargs):
            calls.append(f"{name}:{kwargs.get('model')}")
            if fail:
                raise RuntimeError(f"{name} rate limited")
            return SimpleNamespace(model=kwargs.get("model"))

        return SimpleNamespace(chat=SimpleNamespace(completions=SimpleNamespace(create=create)))

    class FakeCandidate:
        def __init__(self, name, model, fail):
            self.name = name
            self.model = model
            self.max_tokens = 0
            self._fail = fail

        def build_client(self, timeout):
            return make_client(self.name, self._fail)

    first = FakeCandidate("route-first", "m-1", fail=True)
    second = FakeCandidate("route-second", "m-2", fail=False)
    # base_url 属性来自真实客户端；这里直接绕过构造器注入。
    router = object.__new__(OrderedFallbackRouter)
    router._candidates = [first, second]  # noqa: SLF001
    router._timeout = 1
    router.chat = _FallbackChat(router)  # type: ignore[assignment]

    import asyncio

    result = asyncio.run(router.chat.completions.create(model="ignored", prompt="hi"))
    assert calls == ["route-first:m-1", "route-second:m-2"]
    assert result.model == "m-2"
