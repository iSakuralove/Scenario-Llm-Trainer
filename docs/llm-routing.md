# LLM 顺序路由配置

排查工坊、面试舱与语义检索的全部 AI 调用统一走 `config/llm_routes.yaml`——Python Agent 与 Go 后端共用同一份配置文件。这是对原 LiteLLM 网关的薄替代。

## 设计规则

1. **按声明顺序尝试**：默认请求第一个「key 非空」的站点
2. **key 留空 = 跳过**：未填写（或 `${VAR}` 未设置）的站点不参与路由
3. **自动故障转移**：任一站点失败（限流 429 / 网络错误 / 鉴权失败 / 上游 5xx）自动切下一站，全部失败才向调用方报错
4. **全部 OpenAI Chat Completions 兼容协议**：官方 GLM、DeepSeek、MiniMax 与任意中转站只差 `base_url` 和 `model`

## 配置文件

仓库只提交模板 `config/llm_routes.example.yaml`；本地复制为 `config/llm_routes.yaml`（已被 gitignore）后直接填写：

```powershell
Copy-Item config/llm_routes.example.yaml config/llm_routes.yaml
```

**key 两种写法都支持**，推荐直接写在文件里，配置只看这一个地方：

```yaml
api_key: sk-直接粘贴        # 推荐
api_key: ${MY_KEY}          # 引用系统环境变量（存在则生效）
```

### providers（对话补全）

```yaml
providers:
  - name: glm-official
    base_url: https://open.bigmodel.cn/api/paas/v4
    api_key: sk-直接粘贴
    model: glm-4.7
    max_tokens: 131072

  - name: deepseek-official
    base_url: https://api.deepseek.com
    api_key:                       # 留空 = 跳过该站
    model: deepseek-v4-flash
    max_tokens: 393216

  - name: minimax-official
    base_url: https://api.minimax.io/v1
    api_key:
    model: MiniMax-M2.7
    max_tokens: 131072

  # 中转站示例：追加条目即启用，顺序放在哪就第几个被尝试
  # - name: my-proxy
  #   base_url: https://your-proxy.example.com/v1
  #   api_key:
  #   model: gpt-5.6
  #   extra_headers:               # 部分中转站要求特定 User-Agent
  #     User-Agent: python-httpx/0.28.1
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `name` | 建议 | 站点名，用于日志/健康度/审计展示；不填自动编号 |
| `base_url` | 条件 | 官方三家可省略（按 name 自动补全）；中转站必填 |
| `api_key` | 是 | 直接粘贴，或 `${VAR}` 引用系统环境变量；留空跳过该站 |
| `model` | 条件 | 官方三家可省略（默认 glm-4.7 / deepseek-v4-flash / MiniMax-M2.7）；中转站必填 |
| `max_tokens` | 否 | 该站最大输出 token；省略时按官方域名补默认（见下表） |
| `extra_headers` | 否 | 站点专属请求头 |

### 官方三家的 token 上限（官方文档口径）

| 站点 | 上下文 | 最大输出 | 默认 max_tokens |
|---|---|---|---|
| GLM-4.7（智谱） | 200K | 128K | 131072 |
| DeepSeek-V4-Flash | 1M | 384K | 393216 |
| MiniMax-M2.7 | 约 205K | 128K（通常 16K 已够用） | 131072 |

### embedding（语义检索）

```yaml
embedding:
  base_url: https://router.tumuer.me
  api_key:                # 为空时回退本地相似度规则
  model: text-embedding-3-small
  fallback_model:
  timeout_seconds: 8
```

### stt（语音转写）

```yaml
stt:
  base_url: https://api.zetatechs.com
  api_key:                # 为空时使用内置 Mock 转写
  model: gpt-4o-mini-transcribe-2025-12-15
  timeout_seconds: 60
```

## 常用操作

```powershell
# 换站点顺序 = 调整 providers 顺序，然后重启生效
docker compose restart api agent

# 启用 MiniMax：直接在其条目下填 api_key
# 新增中转站：追加条目并填 key
```

## 实现落点

| 层 | 文件 | 说明 |
|---|---|---|
| Go | `backend/internal/ai/llm_routes.go` | YAML 解析 + key/变量插值 + 厂商默认值（model / max_tokens）；providers 接入现有 Router 故障转移引擎，embedding / stt 段分别覆盖 `EmbeddingConfigFromEnv` 与 `NewSTTProviderFromEnv` |
| Python | `agent/src/hiddenworld/llm_routes.py` | `OrderedFallbackRouter` 以鸭子类型替换 AsyncOpenAI，每站用自己的 model 与 max_tokens；`HIDDENWORLD_*_PROVIDER=routes` 启用 |
| 部署 | `docker-compose.yml` | api/agent 挂载同一份配置（`LLM_ROUTES_FILE=/app/llm_routes.yaml`） |

## 与旧配置的关系

- 原 `litellm/` 目录、`LITELLM_*` 与 `LLM_BASE_URL` / `DEEPSEEK_KEY` / `EMBEDDING_*` / `STT_*` 等 AI 环境变量已从 `.env`、compose 与 README 移除
- 旧环境变量在 Go 侧保留**静默回退**（yaml 对应段缺失时才读取），便于存量部署平滑迁移；新部署一律用 yaml
- Embedding / STT key 为空时的降级行为与原来一致（本地规则 / Mock 转写）
