package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
}

type Client struct {
	baseURL          string
	httpClient       *http.Client
	timeout          time.Duration
	failureThreshold int
	openDuration     time.Duration

	mu           sync.Mutex
	failures     int
	circuitUntil time.Time
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
		baseURL:          strings.TrimRight(strings.TrimSpace(config.BaseURL), "/"),
		httpClient:       client,
		timeout:          timeout,
		failureThreshold: threshold,
		openDuration:     openDuration,
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
	if err := validateRequiredResultFields(payload); err != nil {
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
	if result.ContractVersion != ContractVersion {
		c.recordFailure()
		return TurnResult{}, ContractVersionError{Received: result.ContractVersion}
	}
	if result.RequestID != request.RequestID {
		c.recordFailure()
		return TurnResult{}, fmt.Errorf("agent response request_id mismatch")
	}
	if result.ExpectedRevision != request.StateRevision {
		c.recordFailure()
		return TurnResult{}, fmt.Errorf("agent response revision mismatch")
	}
	c.recordSuccess()
	return result, nil
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
	return state
}

func validateRequiredResultFields(payload []byte) error {
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
