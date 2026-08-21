package ai

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// llm_routes.yaml 把 LiteLLM 网关替换成一层薄顺序路由：
// 按 providers 声明顺序依次尝试，任一站点失败（限流 / 网络错误 / 鉴权失败 /
// 上游 5xx）自动切下一站。全部条目按 OpenAI Chat Completions 兼容协议调用，
// 官方 GLM / DeepSeek / MiniMax 与任意中转站都只是 base_url + model 的差别。
//
// 文件内 api_key 支持 ${VAR} 形式引用环境变量（.env / 容器注入），key 不落盘：
//
//	providers:
//	  - name: glm-official
//	    base_url: https://open.bigmodel.cn/api/paas/v4
//	    api_key: ${GLM_API_KEY}
//	    model: glm-4.7
//	  - name: my-proxy
//	    base_url: https://your-proxy.example.com/v1
//	    api_key: ${PROXY_KEY}
//	    model: gpt-5.6
//	    extra_headers:
//	      User-Agent: python-httpx/0.28.1
type llmRoutesFile struct {
	Providers []llmRouteProvider `yaml:"providers"`
	Embedding *llmRouteEndpoint  `yaml:"embedding"`
	STT       *llmRouteEndpoint  `yaml:"stt"`
}

// llmRouteEndpoint 是 embedding / stt 专用端点段：key 直接写在文件里，
// 也可以 ${VAR} 引用环境变量（系统变量同样生效）。
type llmRouteEndpoint struct {
	BaseURL        string            `yaml:"base_url"`
	APIKey         string            `yaml:"api_key"`
	Model          string            `yaml:"model"`
	FallbackModel  string            `yaml:"fallback_model"`
	TimeoutSeconds int               `yaml:"timeout_seconds"`
	ExtraHeaders   map[string]string `yaml:"extra_headers"`
}

// LLMRoutes 是一次配置加载的完整结果。
type LLMRoutes struct {
	Providers []Config
	Embedding *EmbeddingRoute
	STT       *STTRoute
}

// EmbeddingRoute 是语义检索 embedding 端点配置。
type EmbeddingRoute struct {
	BaseURL       string
	APIKey        string
	Model         string
	FallbackModel string
	Timeout       time.Duration
}

// STTRoute 是语音转写端点配置。
type STTRoute struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

type llmRouteProvider struct {
	Name         string            `yaml:"name"`
	BaseURL      string            `yaml:"base_url"`
	APIKey       string            `yaml:"api_key"`
	Model        string            `yaml:"model"`
	MaxTokens    int               `yaml:"max_tokens"`
	ExtraHeaders map[string]string `yaml:"extra_headers"`
}

var llmRoutesEnvPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// DefaultLLMRoutesPath 是未显式配置 LLM_ROUTES_FILE 时的探测路径（工作目录下）。
const DefaultLLMRoutesPath = "llm_routes.yaml"

// LoadLLMRoutes 读取并解析路由配置（providers + embedding + stt）。
// 返回 nil, nil 表示文件不存在（沿用旧的环境变量配置方式）。
func LoadLLMRoutes(path string) (*LLMRoutes, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = DefaultLLMRoutesPath
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read llm routes %s: %w", path, err)
	}
	var parsed llmRoutesFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parse llm routes %s: %w", path, err)
	}
	if len(parsed.Providers) == 0 {
		return nil, fmt.Errorf("llm routes %s declares no providers", path)
	}
	routes := &LLMRoutes{}
	if endpoint := parsed.Embedding; endpoint != nil {
		routes.Embedding = &EmbeddingRoute{
			BaseURL:       strings.TrimSpace(interpolateLLMRoutesEnv(endpoint.BaseURL)),
			APIKey:        strings.TrimSpace(interpolateLLMRoutesEnv(endpoint.APIKey)),
			Model:         strings.TrimSpace(interpolateLLMRoutesEnv(endpoint.Model)),
			FallbackModel: strings.TrimSpace(interpolateLLMRoutesEnv(endpoint.FallbackModel)),
			Timeout:       time.Duration(endpoint.TimeoutSeconds) * time.Second,
		}
	}
	if endpoint := parsed.STT; endpoint != nil {
		if endpoint.TimeoutSeconds <= 0 {
			endpoint.TimeoutSeconds = 60
		}
		routes.STT = &STTRoute{
			BaseURL: strings.TrimSpace(interpolateLLMRoutesEnv(endpoint.BaseURL)),
			APIKey:  strings.TrimSpace(interpolateLLMRoutesEnv(endpoint.APIKey)),
			Model:   strings.TrimSpace(interpolateLLMRoutesEnv(endpoint.Model)),
			Timeout: time.Duration(endpoint.TimeoutSeconds) * time.Second,
		}
	}
	candidates := make([]Config, 0, len(parsed.Providers))
	seen := map[string]bool{}
	skipped := []string{}
	for index, provider := range parsed.Providers {
		name := strings.TrimSpace(interpolateLLMRoutesEnv(provider.Name))
		if name == "" {
			name = fmt.Sprintf("llm-route-%d", index+1)
		}
		if seen[name] {
			return nil, fmt.Errorf("llm routes %s: duplicate provider name %q", path, name)
		}
		seen[name] = true
		apiKey := strings.TrimSpace(interpolateLLMRoutesEnv(provider.APIKey))
		baseURL := strings.TrimSpace(interpolateLLMRoutesEnv(provider.BaseURL))
		// key 留空（未填 ${VAR} 或直接为空）= 该站不参与路由，静默跳过；
		// 请求默认落到声明顺序中第一个有 key 的站点。
		if apiKey == "" || IsUnresolvedEnvPlaceholder(apiKey) {
			skipped = append(skipped, name)
			continue
		}
		if baseURL == "" {
			baseURL = defaultBaseURLForRoute(name, provider.BaseURL)
		}
		model := strings.TrimSpace(interpolateLLMRoutesEnv(provider.Model))
		if model == "" {
			model = defaultModelForRoute(baseURL)
		}
		if baseURL == "" || model == "" {
			return nil, fmt.Errorf(
				"llm routes %s: provider %q requires base_url and model",
				path, name,
			)
		}
		extraHeaders := make(map[string]string, len(provider.ExtraHeaders))
		for key, value := range provider.ExtraHeaders {
			extraHeaders[strings.TrimSpace(key)] = strings.TrimSpace(interpolateLLMRoutesEnv(value))
		}
		maxTokens := provider.MaxTokens
		if maxTokens <= 0 {
			maxTokens = defaultMaxTokensForRoute(baseURL)
		}
		candidates = append(candidates, Config{
			Provider:     name,
			BaseURL:      baseURL,
			APIKey:       apiKey,
			Model:        model,
			MaxTokens:    maxTokens,
			ExtraHeaders: extraHeaders,
		})
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf(
			"llm routes %s: all providers skipped (missing api_key): %v — 填好任一站点的 api_key 后生效",
			path, skipped,
		)
	}
	routes.Providers = candidates
	return routes, nil
}

// interpolateLLMRoutesEnv 把 ${VAR} 替换成环境变量值；未定义的变量保留原样，
// 让上方的必填校验给出可读错误而不是静默空串。
func interpolateLLMRoutesEnv(value string) string {
	return llmRoutesEnvPattern.ReplaceAllStringFunc(value, func(match string) string {
		name := match[2 : len(match)-1]
		if resolved, ok := os.LookupEnv(name); ok {
			return resolved
		}
		return match
	})
}

// configFromLLMRoutes 把路由条目组装成 Router 配置：第一个条目为主站点，
// 全部条目按声明顺序进 OrderedChain，共享超时 / 采样参数等全局默认。
func configFromLLMRoutes(routes []Config) Config {
	base := Config{
		Timeout:          time.Duration(parseInt(os.Getenv("LLM_TIMEOUT_SECONDS"), 120)) * time.Second,
		Temperature:      parseFloat(os.Getenv("LLM_TEMPERATURE"), 0.2),
		TopP:             parseFloat(os.Getenv("LLM_TOP_P"), 0),
		TopK:             parseInt(os.Getenv("LLM_TOP_K"), 0),
		MaxTokens:        parseInt(os.Getenv("LLM_MAX_TOKENS"), 0),
		StreamEnabled:    parseBool(os.Getenv("LLM_STREAM_ENABLED"), true),
		StreamConfigured: true,
	}
	primary := routes[0]
	applyLLMRouteDefaults(&primary, base)
	primary.ProviderConfigs = map[string]Config{}
	chain := make([]string, 0, len(routes))
	chain = append(chain, primary.Provider)
	primary.ProviderConfigs[primary.Provider] = primary
	for _, route := range routes[1:] {
		applyLLMRouteDefaults(&route, base)
		route.ProviderConfigs = nil
		primary.ProviderConfigs[route.Provider] = route
		chain = append(chain, route.Provider)
	}
	primary.OrderedChain = chain
	return primary
}

func applyLLMRouteDefaults(route *Config, base Config) {
	if route.Timeout == 0 {
		route.Timeout = base.Timeout
	}
	route.Temperature = base.Temperature
	route.TopP = base.TopP
	route.TopK = base.TopK
	if route.MaxTokens <= 0 {
		route.MaxTokens = base.MaxTokens
	}
	route.StreamEnabled = base.StreamEnabled
	route.StreamConfigured = true
}

// applyExtraHeaders 应用 llm_routes.yaml 里声明的站点专属请求头
// （如中转站要求的 User-Agent 白名单）。
func applyExtraHeaders(request *http.Request, extra map[string]string) {
	for key, value := range extra {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			request.Header.Set(key, value)
		}
	}
}

// llm 官方站点的预置地址与默认模型。配置文件只填 api_key 也能用；
// 想换模型在同一条目里写 model 覆盖默认值。

const (
	defaultRouteGLMBaseURL      = "https://open.bigmodel.cn/api/paas/v4"
	defaultRouteGLMModel        = "glm-4.7"
	defaultRouteDeepSeekBaseURL = "https://api.deepseek.com"
	defaultRouteDeepSeekModel   = "deepseek-v4-flash"
	defaultRouteMiniMaxBaseURL  = "https://api.minimax.io/v1"
	defaultRouteMiniMaxModel    = "MiniMax-M2.7"

	// 官方文档标注的最大输出 token：GLM-4.7 128K、DeepSeek-V4 384K、
	// MiniMax-M2.7 最大 128K（通常 16K 已够用）。
	defaultRouteGLMMaxTokens      = 131072
	defaultRouteDeepSeekMaxTokens = 393216
	defaultRouteMiniMaxMaxTokens  = 131072
)

// IsUnresolvedEnvPlaceholder 判断值是否仍是未定义的 ${VAR} 字面量
// （变量没设置时插值保持原样，等价于 key 为空）。
func IsUnresolvedEnvPlaceholder(value string) bool {
	return llmRoutesEnvPattern.MatchString(strings.TrimSpace(value))
}

// defaultModelForRoute 按已知官方域名补默认模型。
func defaultModelForRoute(baseURL string) string {
	lower := strings.ToLower(baseURL)
	switch {
	case strings.Contains(lower, "bigmodel.cn"):
		return defaultRouteGLMModel
	case strings.Contains(lower, "deepseek.com"):
		return defaultRouteDeepSeekModel
	case strings.Contains(lower, "minimax"):
		return defaultRouteMiniMaxModel
	default:
		return ""
	}
}

// defaultMaxTokensForRoute 按已知官方域名补默认输出上限。
func defaultMaxTokensForRoute(baseURL string) int {
	lower := strings.ToLower(baseURL)
	switch {
	case strings.Contains(lower, "bigmodel.cn"):
		return defaultRouteGLMMaxTokens
	case strings.Contains(lower, "deepseek.com"):
		return defaultRouteDeepSeekMaxTokens
	case strings.Contains(lower, "minimax"):
		return defaultRouteMiniMaxMaxTokens
	default:
		return 0
	}
}

// defaultBaseURLForRoute 按常见命名补官方地址；无法识别时保持空并触发校验。
func defaultBaseURLForRoute(name string, raw string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(lower, "glm") || strings.Contains(lower, "zhipu"):
		return defaultRouteGLMBaseURL
	case strings.Contains(lower, "deepseek"):
		return defaultRouteDeepSeekBaseURL
	case strings.Contains(lower, "minimax"):
		return defaultRouteMiniMaxBaseURL
	default:
		return strings.TrimSpace(interpolateLLMRoutesEnv(raw))
	}
}
