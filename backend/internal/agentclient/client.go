package agentclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("hiddenworld agent circuit is open")

var (
	ErrRequestTimeout   = errors.New("hiddenworld agent request timed out")
	ErrAgentUnavailable = errors.New("hiddenworld agent is unavailable")
)

const maxResponseBytes = 4 << 20

func logTurnStreamFailure(
	request TurnRequest,
	startedAt time.Time,
	stage string,
	resultSeen bool,
	lastEvent string,
	eventCounts map[string]int,
	droppedProcessEvents int,
	totalBytes int64,
	err error,
) {
	log.Printf("[agentclient-stream] request_id=%s session_id=%s state_revision=%d stage=%s elapsed_ms=%d budget_deadline_ms=%d result_seen=%t last_event=%s event_counts=%v dropped_process_events=%d response_bytes=%d error_type=%T error=%v",
		request.RequestID,
		request.SessionID,
		request.StateRevision,
		stage,
		time.Since(startedAt).Milliseconds(),
		request.Budget.DeadlineMS,
		resultSeen,
		lastEvent,
		eventCounts,
		droppedProcessEvents,
		totalBytes,
		err,
		err)
}

type ContractVersionError struct {
	Received string
}

func (e ContractVersionError) Error() string {
	return fmt.Sprintf("agent contract version mismatch: expected %q, got %q", ContractVersion, e.Received)
}

type HTTPError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e HTTPError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("agent request failed: status=%d code=%s", e.StatusCode, e.Code)
	}
	return fmt.Sprintf("agent request failed: status=%d", e.StatusCode)
}

type Config struct {
	BaseURL          string
	Timeout          time.Duration
	FailureThreshold int
	OpenDuration     time.Duration
	HTTPClient       *http.Client
	// AllowLegacyStructuredResponse 是显式的迁移开关。正式 Agent V2
	// 响应必须包含 turn_assessment、teaching_decision、guidance_state 和
	// turn_control；只有调用方明确打开此项时，客户端才接受旧的扁平响应。
	// 默认值必须保持严格，避免上游悄悄退回旧主链。
	AllowLegacyStructuredResponse bool
}

type Client struct {
	baseURL                       string
	httpClient                    *http.Client
	timeout                       time.Duration
	failureThreshold              int
	openDuration                  time.Duration
	allowLegacyStructuredResponse bool

	mu           sync.Mutex
	failures     int
	circuitUntil time.Time
}

type StreamCallbacks struct {
	OnTurnAnalysis      func(TurnAnalysis) error
	OnPublicTrace       func(PublicTraceEvent) error
	OnReplyDelta        func(text string) error
	OnReasoningRawDelta func(text string) error
}

func New(config Config) *Client {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	threshold := config.FailureThreshold
	if threshold <= 0 {
		threshold = 3
	}
	openDuration := config.OpenDuration
	if openDuration <= 0 {
		openDuration = 30 * time.Second
	}
	return &Client{
		baseURL:                       strings.TrimRight(strings.TrimSpace(config.BaseURL), "/"),
		httpClient:                    client,
		timeout:                       timeout,
		failureThreshold:              threshold,
		openDuration:                  openDuration,
		allowLegacyStructuredResponse: config.AllowLegacyStructuredResponse,
	}
}

func (c *Client) Turn(ctx context.Context, request TurnRequest) (TurnResult, error) {
	if c == nil || c.baseURL == "" {
		return TurnResult{}, errors.New("hiddenworld agent base URL is required")
	}
	if c.circuitOpen(time.Now()) {
		return TurnResult{}, ErrCircuitOpen
	}
	if request.ContractVersion == "" {
		request.ContractVersion = ContractVersion
	}
	if request.Budget.DeadlineMS <= 0 {
		request.Budget.DeadlineMS = 15000
	}
	if request.Budget.MaxReleases <= 0 {
		request.Budget.MaxReleases = 3
	}
	request.LearnerState = normalizeLearnerState(request.LearnerState)
	if request.Transcript == nil {
		request.Transcript = []Turn{}
	}

	body, err := json.Marshal(request)
	if err != nil {
		return TurnResult{}, fmt.Errorf("encode agent request: %w", err)
	}
	requestContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestContext, http.MethodPost, c.baseURL+"/turn", bytes.NewReader(body))
	if err != nil {
		return TurnResult{}, fmt.Errorf("build agent request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		if errors.Is(err, context.Canceled) && errors.Is(ctx.Err(), context.Canceled) {
			return TurnResult{}, fmt.Errorf("call hiddenworld agent: %w", err)
		}
		c.recordFailure()
		if errors.Is(err, context.DeadlineExceeded) {
			return TurnResult{}, fmt.Errorf("%w: %v", ErrRequestTimeout, err)
		}
		return TurnResult{}, fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		httpErr := decodeHTTPError(response)
		if response.StatusCode >= 500 {
			c.recordFailure()
		} else {
			c.recordSuccess()
		}
		return TurnResult{}, httpErr
	}

	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		c.recordFailure()
		return TurnResult{}, fmt.Errorf("read hiddenworld agent response: %w", err)
	}
	if len(payload) > maxResponseBytes {
		c.recordFailure()
		return TurnResult{}, fmt.Errorf("decode hiddenworld agent response: response exceeds %d bytes", maxResponseBytes)
	}
	if err := validateRequiredResultFields(payload, c.allowLegacyStructuredResponse); err != nil {
		c.recordFailure()
		return TurnResult{}, fmt.Errorf("decode hiddenworld agent response: %w", err)
	}
	var result TurnResult
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		c.recordFailure()
		return TurnResult{}, fmt.Errorf("decode hiddenworld agent response: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		c.recordFailure()
		return TurnResult{}, fmt.Errorf("decode hiddenworld agent response: %w", err)
	}
	if err := validateTurnResult(request, result, c.allowLegacyStructuredResponse); err != nil {
		c.recordFailure()
		return TurnResult{}, err
	}
	c.recordSuccess()
	return result, nil
}

func (c *Client) TurnStream(ctx context.Context, request TurnRequest, callbacks StreamCallbacks) (TurnResult, error) {
	if c == nil || c.baseURL == "" {
		return TurnResult{}, errors.New("hiddenworld agent base URL is required")
	}
	if c.circuitOpen(time.Now()) {
		return TurnResult{}, ErrCircuitOpen
	}
	request = normalizeTurnRequest(request)
	startedAt := time.Now()
	body, err := json.Marshal(request)
	if err != nil {
		return TurnResult{}, fmt.Errorf("encode agent request: %w", err)
	}
	requestContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestContext, http.MethodPost, c.baseURL+"/turn/stream", bytes.NewReader(body))
	if err != nil {
		return TurnResult{}, fmt.Errorf("build agent stream request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		c.recordFailure()
		if errors.Is(err, context.DeadlineExceeded) {
			wrapped := fmt.Errorf("%w: %v", ErrRequestTimeout, err)
			logTurnStreamFailure(request, startedAt, "http_do_timeout", false, "", nil, 0, 0, wrapped)
			return TurnResult{}, wrapped
		}
		wrapped := fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
		logTurnStreamFailure(request, startedAt, "http_do", false, "", nil, 0, 0, wrapped)
		return TurnResult{}, wrapped
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		httpErr := decodeHTTPError(response)
		if response.StatusCode >= 500 {
			c.recordFailure()
		} else {
			c.recordSuccess()
		}
		logTurnStreamFailure(request, startedAt, "http_status", false, "", nil, 0, 0, httpErr)
		return TurnResult{}, httpErr
	}

	var result TurnResult
	var resultSeen bool
	var eventName string
	var dataLines []string
	var totalBytes int64
	eventCounts := map[string]int{}
	droppedProcessEvents := 0
	lastEvent := ""
	flush := func() error {
		if eventName == "" && len(dataLines) == 0 {
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = nil
		name := eventName
		eventName = ""
		if len(data) == 0 {
			return nil
		}
		eventCounts[name]++
		lastEvent = name
		totalBytes += int64(len(data))
		if totalBytes > maxResponseBytes {
			return fmt.Errorf("stream response exceeds %d bytes", maxResponseBytes)
		}
		switch name {
		case "turn_analysis":
			var analysis TurnAnalysis
			if err := json.Unmarshal([]byte(data), &analysis); err != nil {
				// turn_analysis 是过程事件，不是最终结果契约。模型/旧 Agent
				// 偶尔发出不完整的分析帧时，不能把已经在途的正文流一并截断。
				// result 事件仍会在收尾阶段做严格校验。
				droppedProcessEvents++
				return nil
			}
			if callbacks.OnTurnAnalysis != nil {
				// 分析回调只用于影子校验/展示，回调拒绝不能取消 Agent
				// 的主流。真正不可恢复的协议错误由 result 校验负责。
				if err := callbacks.OnTurnAnalysis(analysis); err != nil {
					droppedProcessEvents++
				}
			}
		case "public_trace":
			var trace PublicTraceEvent
			if err := json.Unmarshal([]byte(data), &trace); err != nil {
				// 单条公开过程事件损坏时安全丢弃；下游仍可继续接收
				// reply_delta/result。正式历史只由 Go 侧已通过复核的事件重建。
				droppedProcessEvents++
				return nil
			}
			if callbacks.OnPublicTrace != nil {
				if err := callbacks.OnPublicTrace(trace); err != nil {
					// 过程事件是旁路：验证器拒绝在 log/strict 下都只丢弃该条
					// 并记审计，不能把已经在途的正文一起截断。
					droppedProcessEvents++
				}
			}
		case "reply_delta":
			var payload struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(data), &payload); err != nil {
				// reply_delta 只是实时预览；最终 result.reply 才是落库正文。
				// 损坏的单个分片不能阻断后续完整结果。
				droppedProcessEvents++
				return nil
			}
			if callbacks.OnReplyDelta != nil {
				if err := callbacks.OnReplyDelta(payload.Text); err != nil {
					droppedProcessEvents++
				}
			}
		case "reasoning_raw_delta":
			var payload struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(data), &payload); err != nil {
				droppedProcessEvents++
				return nil
			}
			// 测试专用调试事件：正式 Go 事件流和 TurnResult 不承载它，
			// 只有显式注册回调的测试/调试入口才会继续向前传递。
			if callbacks.OnReasoningRawDelta != nil {
				// 调试 reasoning 永远是旁路信息，不得成为正式正文的
				// 失败条件；即使调试消费者拒绝了本片段也继续主流。
				if err := callbacks.OnReasoningRawDelta(payload.Text); err != nil {
					droppedProcessEvents++
				}
			}
		case "result":
			decoder := json.NewDecoder(bytes.NewReader([]byte(data)))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&result); err != nil {
				return fmt.Errorf("decode streamed result: %w", err)
			}
			if err := ensureJSONEOF(decoder); err != nil {
				return fmt.Errorf("decode streamed result: %w", err)
			}
			resultSeen = true
		case "error":
			var payload struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal([]byte(data), &payload); err != nil {
				return fmt.Errorf("decode stream error: %w", err)
			}
			return HTTPError{StatusCode: http.StatusBadGateway, Code: payload.Code, Message: payload.Message}
		case "":
			return nil
		default:
			// 未知事件类型：跳过而不是让整轮失败。agent 新增事件（如心跳）不应
			// 导致 Go 客户端整轮报错；缺 result 依旧会被下方校验拦住。
			return nil
		}
		return nil
	}

	scanner := bufio.NewScanner(response.Body)
	// Python 把 result 编码为单个 SSE data 行；单行限制必须与总响应限制
	// 一致，否则 1–4 MiB 的合法最终结果会先被 Scanner 以 token too long
	// 截断，并在上层表现成 agent_invalid_response。
	scanner.Buffer(make([]byte, 4096), maxResponseBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				c.recordFailure()
				logTurnStreamFailure(request, startedAt, "event_flush", resultSeen, lastEvent, eventCounts, droppedProcessEvents, totalBytes, err)
				return TurnResult{}, err
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimPrefix(line, "data:"))
		}
	}
	if err := scanner.Err(); err != nil {
		c.recordFailure()
		if errors.Is(err, context.DeadlineExceeded) {
			wrapped := fmt.Errorf("%w: %v", ErrRequestTimeout, err)
			logTurnStreamFailure(request, startedAt, "scanner_timeout", resultSeen, lastEvent, eventCounts, droppedProcessEvents, totalBytes, wrapped)
			return TurnResult{}, wrapped
		}
		wrapped := fmt.Errorf("read agent stream: %w", err)
		logTurnStreamFailure(request, startedAt, "scanner", resultSeen, lastEvent, eventCounts, droppedProcessEvents, totalBytes, wrapped)
		return TurnResult{}, wrapped
	}
	if err := flush(); err != nil {
		c.recordFailure()
		logTurnStreamFailure(request, startedAt, "final_flush", resultSeen, lastEvent, eventCounts, droppedProcessEvents, totalBytes, err)
		return TurnResult{}, err
	}
	if !resultSeen {
		c.recordFailure()
		err := errors.New("agent stream ended without result event")
		logTurnStreamFailure(request, startedAt, "eof_without_result", false, lastEvent, eventCounts, droppedProcessEvents, totalBytes, err)
		return TurnResult{}, err
	}
	if err := validateTurnResult(request, result, c.allowLegacyStructuredResponse); err != nil {
		c.recordFailure()
		logTurnStreamFailure(request, startedAt, "result_validation", true, lastEvent, eventCounts, droppedProcessEvents, totalBytes, err)
		return TurnResult{}, err
	}
	c.recordSuccess()
	return result, nil
}

func normalizeTurnRequest(request TurnRequest) TurnRequest {
	if request.ContractVersion == "" {
		request.ContractVersion = ContractVersion
	}
	if request.Budget.DeadlineMS <= 0 {
		request.Budget.DeadlineMS = 15000
	}
	if request.Budget.MaxReleases <= 0 {
		request.Budget.MaxReleases = 3
	}
	request.LearnerState = normalizeLearnerState(request.LearnerState)
	if request.Transcript == nil {
		request.Transcript = []Turn{}
	}
	return request
}

func validateTurnResult(request TurnRequest, result TurnResult, allowLegacyStructuredResponse bool) error {
	if result.ContractVersion != ContractVersion {
		return ContractVersionError{Received: result.ContractVersion}
	}
	if result.RequestID != request.RequestID {
		return errors.New("agent response request_id mismatch")
	}
	if result.ExpectedRevision != request.StateRevision {
		return errors.New("agent response revision mismatch")
	}
	if !allowLegacyStructuredResponse {
		if result.TurnAssessment == nil {
			return errors.New("agent response missing required turn_assessment")
		}
		if result.TeachingDecision == nil {
			return errors.New("agent response missing required teaching_decision")
		}
	}
	return nil
}

func normalizeLearnerState(state LearnerState) LearnerState {
	if state.CollectedEvidence == nil {
		state.CollectedEvidence = []string{}
	}
	if state.RuledOutHypotheses == nil {
		state.RuledOutHypotheses = []string{}
	}
	if state.EstablishedFacts == nil {
		state.EstablishedFacts = []string{}
	}
	if state.ActionsTaken == nil {
		state.ActionsTaken = []string{}
	}
	if state.RecentOpenings == nil {
		state.RecentOpenings = []string{}
	}
	if state.ConceptMastery == nil {
		state.ConceptMastery = map[string]int{}
	}
	if state.SkillMastery == nil {
		state.SkillMastery = map[string]int{}
	}
	if state.ExplanationPreferences.Detail == "" {
		state.ExplanationPreferences.Detail = "balanced"
	}
	if state.ExplanationPreferences.Analogy == "" {
		state.ExplanationPreferences.Analogy = "medium"
	}
	if state.ExplanationPreferences.Directness == "" {
		state.ExplanationPreferences.Directness = "medium"
	}
	return state
}

func validateRequiredResultFields(payload []byte, allowLegacyStructuredResponse bool) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return err
	}
	required := []string{
		"contract_version",
		"request_id",
		"expected_revision",
		"reply",
		"turn_analysis",
		"proposals",
		"public_trace",
		"internal_verification",
		"internal_audit",
	}
	if !allowLegacyStructuredResponse {
		required = append(required,
			"turn_assessment",
			"teaching_decision",
			"guidance_state",
			"turn_control",
		)
	}
	for _, name := range required {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("required field %q is missing or null", name)
		}
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("unexpected trailing JSON value")
	}
	return err
}

func decodeHTTPError(response *http.Response) HTTPError {
	payload := struct {
		Detail struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"detail"`
	}{}
	_ = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload)
	return HTTPError{
		StatusCode: response.StatusCode,
		Code:       payload.Detail.Code,
		Message:    payload.Detail.Message,
	}
}

func (c *Client) circuitOpen(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.circuitUntil.IsZero() || !now.Before(c.circuitUntil) {
		if !c.circuitUntil.IsZero() {
			c.circuitUntil = time.Time{}
			c.failures = 0
		}
		return false
	}
	return true
}

func (c *Client) recordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures++
	if c.failures >= c.failureThreshold {
		c.circuitUntil = time.Now().Add(c.openDuration)
	}
}

func (c *Client) recordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures = 0
	c.circuitUntil = time.Time{}
}
