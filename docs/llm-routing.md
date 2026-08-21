# LLM 顺序路由配置

排查工坊与面试舱的全部 LLM 调用统一走 `config/llm_routes.yaml` 声明的**顺序路由**——这是对原 LiteLLM 网关的薄替代，Python Agent 与 Go 后端共用同一份配置文件。

## 设计规则

1. **按声明顺序尝试**：默认请求第一个「key 非空」的站点
2. **key 留空 = 跳过**：`${VAR}` 未设置或空串的站点不参与路由，静默跳过
3. **自动故障转移**：任一站点失败（限流 429 / 网络错误 / 鉴权失败 / 上游 5xx）自动切下一站，全部失败才向调用方报错
4. **全部 OpenAI 兼容协议**：官方 GLM、DeepSeek、MiniMax 与任意中转站只差 `base_url` 和 `model`

## 配置文件

`config/llm_routes.yaml`（已预置官方三家默认值）：

```yaml
providers:
  - name: glm-official
    base_url: https://open.bigmodel.cn/api/paas/v4
    api_key: ${GLM_API_KEY}
    model: glm-4.7

  - name: deepseek-official
    base_url: https://api.deepseek.com
    api_key: ${DEEPSEEK_KEY}
    model: deepseek-v4-flash

  - name: minimax-official
    base_url: https://api.minimax.io/v1
    api_key: ${MINIMAX_API_KEY}      # 未设置 → 该站自动跳过
    model: MiniMax-M2.7

  # 中转站示例：取消注释并填 key 即启用
  # - name: my-proxy
  #   base_url: https://your-proxy.example.com/v1
  #   api_key: ${PROXY_API_KEY}
  #   model: gpt-5.6
  #   extra_headers:                  # 部分中转站要求特定 User-Agent
  #     User-Agent: python-httpx/0.28.1
```

字段说明：

| 字段 | 必填 | 说明 |
|---|---|---|
| `name` | 建议 | 站点名，用于日志/健康度/审计展示；不填自动编号 |
| `base_url` | 条件 | 官方三家可省略（按 name 自动补全默认地址）；中转站必填 |
| `api_key` | 是 | 支持 `${VAR}` 引用 `.env` 环境变量，key 不落盘 |
| `model` | 条件 | 官方三家可省略（默认 glm-4.7 / deepseek-v4-flash / MiniMax-M2.7）；中转站必填 |
| `extra_headers` | 否 | 站点专属请求头 |

## 常用操作

```powershell
# 换站点顺序 = 调整 providers 顺序，然后重启生效
docker compose restart api agent

# 启用 MiniMax：在 .env 加一行
# MINIMAX_API_KEY=sk-xxx

# 新增中转站：在 yaml 追加条目并填 key
```

## 实现落点

| 层 | 文件 | 说明 |
|---|---|---|
| Go | `backend/internal/ai/llm_routes.go` | YAML 解析 + 环境变量插值 + 厂商默认值；接入现有 Router 故障转移引擎（健康度/限流/审计复用），有序链优先于任务静态链 |
| Python | `agent/src/hiddenworld/llm_routes.py` | `OrderedFallbackRouter` 以鸭子类型替换 AsyncOpenAI，每站用自己的 model；`HIDDENWORLD_*_PROVIDER=routes` 启用 |
| 部署 | `docker-compose.yml` | api/agent 挂载同一份配置（`LLM_ROUTES_FILE=/app/llm_routes.yaml`） |

## 与旧网关的关系

- 原 `litellm/` 目录与 `LITELLM_*` 环境变量已删除
- `.env` 中 `LLM_BASE_URL` / `LLM_API_KEY` / `LLM_MODEL` 不再被读取；存在 `llm_routes.yaml` 时路由文件优先接管全部 LLM 调用
- Embedding 走独立的 `EMBEDDING_*` 变量，不受影响
- 回退方式：删除/改名 `config/llm_routes.yaml` 并恢复 `.env` 的 `LLM_BASE_URL` 等变量，即回到旧网关模式
