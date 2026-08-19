package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"situational-teaching/backend/internal/agentclient"
	"situational-teaching/backend/internal/domain"
)

const interpreterLowConfidenceThreshold = 0.45

var (
	scenarioIdentifierPattern       = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.:/-]{2,}`)
	scenarioNumberPattern           = regexp.MustCompile(`\d+(?:[.,]\d+)*`)
	scenarioChineseComponentPattern = regexp.MustCompile(`(?:[A-Za-z_][A-Za-z0-9_]{1,15}|[\p{Han}]{1,8})(?:表|服务|接口|主库|从库|索引|字段)`)
	scenarioHanPattern              = regexp.MustCompile(`\p{Han}`)
	scenarioWhitespacePattern       = regexp.MustCompile(`\s+`)
)

type scenarioAgentClient interface {
	Turn(context.Context, agentclient.TurnRequest) (agentclient.TurnResult, error)
}

type scenarioAgentStreamingClient interface {
	TurnStream(context.Context, agentclient.TurnRequest, agentclient.StreamCallbacks) (agentclient.TurnResult, error)
}

type deterministicScenarioAgentClient struct{}

func (deterministicScenarioAgentClient) Turn(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
	return agentclient.TurnResult{
		ContractVersion:  agentclient.ContractVersion,
		RequestID:        request.RequestID,
		ExpectedRevision: request.StateRevision,
		Reply:            "已记录你的排查思路，请继续说明下一步要验证的公开现象。",
		TurnAnalysis: agentclient.TurnAnalysis{
			Actions:          []string{},
			EstablishedFacts: []string{},
			StudentAffect:    "engaged",
			Confidence:       0.9,
		},
		Proposals: []agentclient.Proposal{
			{Kind: "set_stalled_turns", Value: request.LearnerState.StalledTurns + 1},
			{Kind: "record_opening", Text: "已记录你的排查思路，请继续说明下一步要验证的公开现象。"},
		},
		PublicTrace: []agentclient.PublicTraceEvent{
			{Sequence: 1, Kind: "reasoning_summary_completed", Status: "completed", Summary: "已完成本轮公开意图识别。"},
			{Sequence: 2, Kind: "mentor_buffered", Status: "completed", Summary: "导师回复已完成私有缓冲。"},
			{Sequence: 3, Kind: "guard_passed", Status: "completed", Summary: "回复已通过安全校验。"},
		},
		InternalVerification: agentclient.VerificationResult{
			Relation:         "unknown",
			RuledOutThisTurn: []string{},
		},
		InternalAudit: agentclient.AuditTrace{ReasonCodes: []string{}, RulesVersion: agentclient.ContractVersion},
	}, nil
}

type scenarioProposalApproval struct {
	Kind       string `json:"kind"`
	Accepted   bool   `json:"accepted"`
	ReasonCode string `json:"reason_code"`
}

type scenarioAgentHTTPError struct {
	Status  int
	Code    string
	Message string
}

type scenarioTurnFlight struct {
	fingerprint string
	done        chan struct{}
	message     domain.ScenarioMessage
	session     domain.ScenarioSession
	hasSession  bool
	err         error
}

func (e scenarioAgentHTTPError) Error() string { return e.Message }

func scenarioRequestFingerprint(sessionID, content string) string {
	digest := sha256.Sum256([]byte(sessionID + "\x00" + strings.TrimSpace(content)))
	return hex.EncodeToString(digest[:])
}

func scenarioTurnFlightKey(sessionID, requestID string) string {
	return sessionID + "\x00" + requestID
}

func (s *Server) beginScenarioTurnFlight(sessionID, requestID, fingerprint string) (*scenarioTurnFlight, bool, error) {
	key := scenarioTurnFlightKey(sessionID, requestID)
	s.scenarioTurnMu.Lock()
	defer s.scenarioTurnMu.Unlock()
	if s.scenarioTurnFlights == nil {
		s.scenarioTurnFlights = map[string]*scenarioTurnFlight{}
	}
	if existing, ok := s.scenarioTurnFlights[key]; ok {
		if existing.fingerprint != fingerprint {
			return nil, false, domain.ScenarioRequestConflictError{RequestID: requestID}
		}
		return existing, false, nil
	}
	flight := &scenarioTurnFlight{fingerprint: fingerprint, done: make(chan struct{})}
	s.scenarioTurnFlights[key] = flight
	return flight, true, nil
}

func (s *Server) finishScenarioTurnFlight(
	sessionID string,
	requestID string,
	flight *scenarioTurnFlight,
	message domain.ScenarioMessage,
	session *domain.ScenarioSession,
	err error,
) {
	if flight == nil {
		return
	}
	key := scenarioTurnFlightKey(sessionID, requestID)
	s.scenarioTurnMu.Lock()
	flight.message = message
	if session != nil {
		flight.session = *session
		flight.hasSession = true
	}
	flight.err = err
	close(flight.done)
	delete(s.scenarioTurnFlights, key)
	s.scenarioTurnMu.Unlock()
}

func waitScenarioTurnFlight(ctx context.Context, flight *scenarioTurnFlight) (domain.ScenarioMessage, *domain.ScenarioSession, error) {
	select {
	case <-ctx.Done():
		return domain.ScenarioMessage{}, nil, ctx.Err()
	case <-flight.done:
		if flight.err != nil {
			return domain.ScenarioMessage{}, nil, flight.err
		}
		if !flight.hasSession {
			return domain.ScenarioMessage{}, nil, errors.New("scenario in-flight result is missing a session")
		}
		session := flight.session
		return flight.message, &session, nil
	}
}

func classifyScenarioAgentError(err error) scenarioAgentHTTPError {
	switch {
	case errors.Is(err, agentclient.ErrCircuitOpen):
		return scenarioAgentHTTPError{Status: http.StatusServiceUnavailable, Code: "agent_circuit_open", Message: "排查导师暂时不可用，请稍后重试"}
	case errors.Is(err, agentclient.ErrRequestTimeout):
		return scenarioAgentHTTPError{Status: http.StatusGatewayTimeout, Code: "agent_timeout", Message: "排查导师本轮处理超时，请重试"}
	case errors.Is(err, agentclient.ErrAgentUnavailable):
		return scenarioAgentHTTPError{Status: http.StatusServiceUnavailable, Code: "agent_unavailable", Message: "排查导师暂时不可用，请稍后重试"}
	}
	var versionErr agentclient.ContractVersionError
	if errors.As(err, &versionErr) {
		return scenarioAgentHTTPError{Status: http.StatusBadGateway, Code: "agent_contract_mismatch", Message: "排查导师服务契约不兼容"}
	}
	var httpErr agentclient.HTTPError
	if errors.As(err, &httpErr) {
		return scenarioAgentHTTPError{Status: http.StatusBadGateway, Code: "agent_upstream_error", Message: "排查导师服务返回异常"}
	}
	return scenarioAgentHTTPError{Status: http.StatusBadGateway, Code: "agent_invalid_response", Message: "排查导师返回了无效结果"}
}

func scenarioMessageError(err error) (int, string, string) {
	var httpErr scenarioAgentHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status, httpErr.Code, httpErr.Message
	}
	var revisionErr domain.ScenarioRevisionConflictError
	if errors.As(err, &revisionErr) {
		return http.StatusConflict, "revision_conflict", "会话状态已更新，请重新发送本轮内容"
	}
	var requestErr domain.ScenarioRequestConflictError
	if errors.As(err, &requestErr) {
		return http.StatusConflict, "request_id_conflict", "该请求标识已被其他内容使用"
	}
	return http.StatusBadRequest, "invalid_request", err.Error()
}

func approveScenarioProposals(
	session *domain.ScenarioSession,
	world *domain.HiddenWorld,
	result agentclient.TurnResult,
) (domain.ScenarioLearnerState, []scenarioProposalApproval, error) {
	if session == nil || world == nil {
		return domain.ScenarioLearnerState{}, nil, errors.New("hiddenworld session state is unavailable")
	}
	state := session.LearnerState.Normalized()
	hypotheses := make(map[string]bool, len(world.Hypotheses))
	for _, hypothesis := range world.Hypotheses {
		hypotheses[hypothesis.HypothesisID] = true
	}
	evidence := make(map[string]domain.EvidenceNode, len(world.EvidenceGraph))
	for _, node := range world.EvidenceGraph {
		evidence[node.EvidenceID] = node
	}
	actions := stringSet(result.TurnAnalysis.Actions)
	facts := stringSet(result.TurnAnalysis.EstablishedFacts)
	ruledOut := stringSet(result.InternalVerification.RuledOutThisTurn)
	lowConfidence := result.TurnAnalysis.Confidence < interpreterLowConfidenceThreshold
	progress := false
	approvals := make([]scenarioProposalApproval, 0, len(result.Proposals))

	for _, proposal := range result.Proposals {
		approval := scenarioProposalApproval{Kind: proposal.Kind}
		reject := func(code string) (domain.ScenarioLearnerState, []scenarioProposalApproval, error) {
			approval.ReasonCode = code
			approvals = append(approvals, approval)
			return domain.ScenarioLearnerState{}, approvals, fmt.Errorf("proposal %s rejected: %s", proposal.Kind, code)
		}
		switch proposal.Kind {
		case "release_evidence":
			if lowConfidence {
				return reject("low_confidence_mutation")
			}
			node, ok := evidence[proposal.EvidenceID]
			if !ok || scenarioContainsString(state.CollectedEvidence, proposal.EvidenceID) {
				return reject("invalid_evidence")
			}
			if !intersects(actions, stringSet(node.ObtainedBy)) {
				return reject("evidence_not_requested")
			}
			if !containsAll(state.CollectedEvidence, node.Prerequisites) {
				return reject("evidence_prerequisite_missing")
			}
			state.CollectedEvidence = append(state.CollectedEvidence, proposal.EvidenceID)
			progress = true
		case "record_action":
			if lowConfidence || proposal.Action == "" || !actions[proposal.Action] {
				return reject("action_not_in_turn_analysis")
			}
			state.ActionsTaken = appendUnique(state.ActionsTaken, proposal.Action)
		case "record_established_fact":
			if lowConfidence || proposal.Fact == "" || !facts[proposal.Fact] {
				return reject("fact_not_in_turn_analysis")
			}
			before := len(state.EstablishedFacts)
			state.EstablishedFacts = appendUnique(state.EstablishedFacts, proposal.Fact)
			progress = progress || len(state.EstablishedFacts) > before
		case "set_current_hypothesis":
			if lowConfidence || !hypotheses[proposal.HypothesisID] || proposal.HypothesisID != result.TurnAnalysis.HypothesisID {
				return reject("invalid_hypothesis")
			}
			if state.CurrentHypothesis != proposal.HypothesisID {
				state.CurrentHypothesis = proposal.HypothesisID
				progress = true
			}
		case "rule_out_hypothesis":
			if !hypotheses[proposal.HypothesisID] || !ruledOut[proposal.HypothesisID] {
				return reject("hypothesis_not_ruled_out_this_turn")
			}
			before := len(state.RuledOutHypotheses)
			state.RuledOutHypotheses = appendUnique(state.RuledOutHypotheses, proposal.HypothesisID)
			progress = progress || len(state.RuledOutHypotheses) > before
		case "set_current_focus":
			if !validScenarioFocus(proposal.Focus) {
				return reject("invalid_focus")
			}
			state.CurrentFocus = proposal.Focus
		case "advance_effective_turn":
			if proposal.Value != 1 || !progress {
				return reject("effective_turn_without_progress")
			}
			state.EffectiveTurns++
		case "set_stalled_turns":
			expected := session.LearnerState.StalledTurns
			if progress {
				expected = 0
			} else if !result.TurnAnalysis.IsNoise {
				expected++
			}
			if proposal.Value != expected {
				return reject("invalid_stalled_turns")
			}
			state.StalledTurns = proposal.Value
		case "record_opening":
			if proposal.Text == "" || proposal.Text != mentorOpening(result.Reply) {
				return reject("opening_not_from_reply")
			}
			state.RecentOpenings = appendUnique(state.RecentOpenings, proposal.Text)
			if len(state.RecentOpenings) > 3 {
				state.RecentOpenings = append([]string{}, state.RecentOpenings[len(state.RecentOpenings)-3:]...)
			}
		default:
			return reject("unsupported_proposal_kind")
		}
		approval.Accepted = true
		approval.ReasonCode = "approved"
		approvals = append(approvals, approval)
	}
	return state.Normalized(), approvals, nil
}

func validateScenarioReply(reply string, world *domain.HiddenWorld, state domain.ScenarioLearnerState) error {
	if world == nil {
		return errors.New("hidden world is unavailable")
	}
	released := stringSet(state.CollectedEvidence)
	sources := []string{world.RootCause.Description}
	for _, node := range world.EvidenceGraph {
		if !released[node.EvidenceID] {
			sources = append(sources, node.Content)
		}
	}
	entities := []string{world.RootCause.ID, world.RootCause.Component}
	for _, source := range sources {
		entities = append(entities, extractScenarioSensitiveTokens(source)...)
	}
	for entity := range stringSet(entities) {
		if scenarioReplyContainsEntity(reply, entity) {
			return errors.New("reply contains unreleased entity")
		}
	}
	return nil
}

func validateScenarioPublicTrace(
	requestID string,
	userContent string,
	result agentclient.TurnResult,
	world *domain.HiddenWorld,
	state domain.ScenarioLearnerState,
) error {
	if len(result.PublicTrace) > 64 {
		return errors.New("public trace exceeds event limit")
	}
	allowedStatuses := map[string]bool{
		"started": true, "running": true, "completed": true, "failed": true,
	}
	allowedReasoningStages := map[string]bool{
		"understanding_message": true,
		"checking_observations": true,
		"verifying_answer":      true,
		"composing_reply":       true,
	}
	allowedSupportStatuses := map[string]bool{
		"insufficiently_specific": true,
		"needs_more_evidence":     true,
		"has_evidence_conflict":   true,
		"evidence_consistent":     true,
	}
	phaseByKind := map[string]int{
		"reasoning_summary_delta":     1,
		"reasoning_summary_completed": 1,
		"tool_started":                2,
		"tool_result":                 2,
		"tool_completed":              2,
		"response_summary":            3,
		"mentor_buffered":             4,
		"guard_passed":                5,
	}

	previousSequence := 0
	currentPhase := 0
	toolStage := 0
	guardCount := 0
	shouldCompareAnswer := result.TurnAnalysis.ContainsAnswerAttempt && result.TurnAnalysis.Confidence >= interpreterLowConfidenceThreshold
	normalizedUserContent := strings.ToLower(norm.NFKC.String(userContent))

	for _, trace := range result.PublicTrace {
		if trace.Sequence <= previousSequence {
			return errors.New("public trace sequence is not strictly increasing")
		}
		previousSequence = trace.Sequence
		phase, ok := phaseByKind[trace.Kind]
		if !ok {
			return fmt.Errorf("public trace kind %q is not allowed from Python", trace.Kind)
		}
		if phase < currentPhase {
			return errors.New("public trace event order is invalid")
		}
		currentPhase = phase
		if !allowedStatuses[trace.Status] {
			return fmt.Errorf("public trace status %q is invalid", trace.Status)
		}
		if trace.Text != "" {
			return errors.New("Python public trace cannot publish reply text")
		}
		if trace.Summary != "" {
			if err := validateScenarioReply(trace.Summary, world, state); err != nil {
				return fmt.Errorf("public trace summary rejected: %w", err)
			}
		}
		if trace.Reasoning != nil {
			if !allowedReasoningStages[trace.Reasoning.Stage] || strings.TrimSpace(trace.Reasoning.Text) == "" {
				return errors.New("public reasoning summary is invalid")
			}
			if err := validateScenarioReply(trace.Reasoning.Text, world, state); err != nil {
				return fmt.Errorf("public reasoning summary rejected: %w", err)
			}
		}

		isToolEvent := trace.Kind == "tool_started" || trace.Kind == "tool_result" || trace.Kind == "tool_completed"
		if !isToolEvent {
			if trace.Tool != nil || trace.ToolName != "" || trace.DurationMS != 0 {
				return errors.New("non-tool public trace contains tool payload")
			}
		} else {
			if !shouldCompareAnswer {
				return errors.New("compare_answer trace is not allowed for this turn")
			}
			tool := trace.Tool
			if tool == nil || tool.Name != "compare_answer" || trace.ToolName != "compare_answer" {
				return errors.New("public trace contains an unsupported tool")
			}
			if tool.DurationMS < 0 || trace.DurationMS < 0 || (trace.DurationMS != 0 && trace.DurationMS != tool.DurationMS) {
				return errors.New("public tool duration is invalid")
			}
			if len(tool.RedactedArguments) != 1 || tool.RedactedArguments["answer_attempt_id"] != requestID+":answer" {
				return errors.New("compare_answer arguments are not bound to the current request")
			}
			switch trace.Kind {
			case "tool_started":
				if toolStage != 0 || trace.Status != "started" || tool.Result != nil {
					return errors.New("compare_answer start event is invalid")
				}
				toolStage = 1
			case "tool_result":
				if toolStage != 1 || trace.Status != "completed" || tool.Result == nil {
					return errors.New("compare_answer result event is invalid")
				}
				toolStage = 2
			case "tool_completed":
				if toolStage != 2 || trace.Status != "completed" || tool.Result == nil {
					return errors.New("compare_answer completion event is invalid")
				}
				toolStage = 3
			}
			if tool.Result != nil {
				if tool.Result.Tool != "compare_answer" || tool.Result.Status != "completed" || !allowedSupportStatuses[tool.Result.SupportStatus] {
					return errors.New("public compare_answer result is invalid")
				}
				if err := validateScenarioReply(tool.Result.NextAction, world, state); err != nil {
					return fmt.Errorf("public compare_answer next action rejected: %w", err)
				}
				for _, point := range tool.Result.UserPoints {
					normalizedPoint := strings.ToLower(strings.TrimSpace(norm.NFKC.String(point)))
					if normalizedPoint == "" || !strings.Contains(normalizedUserContent, normalizedPoint) {
						return errors.New("public compare_answer point was not sourced from the user message")
					}
				}
			}
		}

		if trace.Kind == "guard_passed" {
			guardCount++
			if trace.Status != "completed" {
				return errors.New("guard_passed event must be completed")
			}
		}
	}
	if guardCount != 1 {
		return errors.New("public trace must contain exactly one guard_passed event")
	}
	if shouldCompareAnswer && toolStage != 3 {
		return errors.New("answer attempt is missing the complete compare_answer lifecycle")
	}
	if !shouldCompareAnswer && toolStage != 0 {
		return errors.New("ordinary turn contains a compare_answer lifecycle")
	}
	return nil
}

type scenarioPublicTraceStream struct {
	requestID       string
	userContent     string
	world           *domain.HiddenWorld
	state           domain.ScenarioLearnerState
	analysis        *agentclient.TurnAnalysis
	lastSequence    int
	lastPhase       int
	toolStage       int
	guardCount      int
	emittedCount    int
	validationError error
}

func newScenarioPublicTraceStream(
	requestID string,
	userContent string,
	world *domain.HiddenWorld,
	state domain.ScenarioLearnerState,
) *scenarioPublicTraceStream {
	return &scenarioPublicTraceStream{
		requestID:   requestID,
		userContent: userContent,
		world:       world,
		state:       state,
	}
}

func (stream *scenarioPublicTraceStream) onTurnAnalysis(analysis agentclient.TurnAnalysis) error {
	stream.analysis = &analysis
	return nil
}

func (stream *scenarioPublicTraceStream) onPublicTrace(trace agentclient.PublicTraceEvent) error {
	if stream.validationError != nil {
		return stream.validationError
	}
	if err := stream.validate(trace); err != nil {
		stream.validationError = err
		return err
	}
	stream.emittedCount++
	return nil
}

func (stream *scenarioPublicTraceStream) validate(trace agentclient.PublicTraceEvent) error {
	phaseByKind := map[string]int{
		"reasoning_summary_delta":     1,
		"reasoning_summary_completed": 1,
		"tool_started":                2,
		"tool_result":                 2,
		"tool_completed":              2,
		"response_summary":            3,
		"mentor_buffered":             4,
		"guard_passed":                5,
	}
	phase, ok := phaseByKind[trace.Kind]
	if !ok {
		return fmt.Errorf("public trace kind %q is not allowed from Python", trace.Kind)
	}
	if trace.Sequence <= stream.lastSequence {
		return errors.New("public trace sequence is not strictly increasing")
	}
	stream.lastSequence = trace.Sequence
	if phase < stream.lastPhase {
		return errors.New("public trace event order is invalid")
	}
	stream.lastPhase = phase
	if !scenarioTraceStatusAllowed(trace.Status) {
		return fmt.Errorf("public trace status %q is invalid", trace.Status)
	}
	if trace.Text != "" {
		return errors.New("Python public trace cannot publish reply text")
	}
	if trace.Summary != "" {
		if err := validateScenarioReply(trace.Summary, stream.world, stream.state); err != nil {
			return fmt.Errorf("public trace summary rejected: %w", err)
		}
	}
	if trace.Reasoning != nil {
		if !scenarioReasoningStageAllowed(trace.Reasoning.Stage) || strings.TrimSpace(trace.Reasoning.Text) == "" {
			return errors.New("public reasoning summary is invalid")
		}
		if err := validateScenarioReply(trace.Reasoning.Text, stream.world, stream.state); err != nil {
			return fmt.Errorf("public reasoning summary rejected: %w", err)
		}
	}
	if trace.Kind != "tool_started" && trace.Kind != "tool_result" && trace.Kind != "tool_completed" {
		if trace.Tool != nil || trace.ToolName != "" || trace.DurationMS != 0 {
			return errors.New("non-tool public trace contains tool payload")
		}
	} else {
		if stream.analysis == nil || !stream.analysis.ContainsAnswerAttempt || stream.analysis.Confidence < interpreterLowConfidenceThreshold {
			return errors.New("compare_answer trace is not allowed for this turn")
		}
		if err := stream.validateTool(trace); err != nil {
			return err
		}
	}
	if trace.Kind == "guard_passed" {
		stream.guardCount++
		if stream.guardCount > 1 || trace.Status != "completed" {
			return errors.New("guard_passed event is invalid")
		}
	}
	return nil
}

func (stream *scenarioPublicTraceStream) validateTool(trace agentclient.PublicTraceEvent) error {
	tool := trace.Tool
	if tool == nil || tool.Name != "compare_answer" || trace.ToolName != "compare_answer" {
		return errors.New("public trace contains an unsupported tool")
	}
	if tool.DurationMS < 0 || trace.DurationMS < 0 || (trace.DurationMS != 0 && trace.DurationMS != tool.DurationMS) {
		return errors.New("public tool duration is invalid")
	}
	if len(tool.RedactedArguments) != 1 || tool.RedactedArguments["answer_attempt_id"] != stream.requestID+":answer" {
		return errors.New("compare_answer arguments are not bound to the current request")
	}
	switch trace.Kind {
	case "tool_started":
		if stream.toolStage != 0 || trace.Status != "started" || tool.Result != nil {
			return errors.New("compare_answer start event is invalid")
		}
		stream.toolStage = 1
	case "tool_result":
		if stream.toolStage != 1 || trace.Status != "completed" || tool.Result == nil {
			return errors.New("compare_answer result event is invalid")
		}
		stream.toolStage = 2
	case "tool_completed":
		if stream.toolStage != 2 || trace.Status != "completed" || tool.Result == nil {
			return errors.New("compare_answer completion event is invalid")
		}
		stream.toolStage = 3
	}
	if tool.Result != nil {
		if err := validateScenarioPublicComparison(tool.Result, stream.userContent, stream.world, stream.state); err != nil {
			return err
		}
	}
	return nil
}

func validateScenarioPublicComparison(
	comparison *agentclient.PublicAnswerComparison,
	userContent string,
	world *domain.HiddenWorld,
	state domain.ScenarioLearnerState,
) error {
	allowed := map[string]bool{
		"insufficiently_specific": true,
		"needs_more_evidence":     true,
		"has_evidence_conflict":   true,
		"evidence_consistent":     true,
	}
	if comparison.Tool != "compare_answer" || comparison.Status != "completed" || !allowed[comparison.SupportStatus] {
		return errors.New("public compare_answer result is invalid")
	}
	if err := validateScenarioReply(comparison.NextAction, world, state); err != nil {
		return fmt.Errorf("public compare_answer next action rejected: %w", err)
	}
	normalizedUserContent := strings.ToLower(norm.NFKC.String(userContent))
	for _, point := range comparison.UserPoints {
		normalizedPoint := strings.ToLower(strings.TrimSpace(norm.NFKC.String(point)))
		if normalizedPoint == "" || !strings.Contains(normalizedUserContent, normalizedPoint) {
			return errors.New("public compare_answer point was not sourced from the user message")
		}
	}
	return nil
}

func scenarioTraceStatusAllowed(status string) bool {
	return status == "started" || status == "running" || status == "completed" || status == "failed"
}

func scenarioReasoningStageAllowed(stage string) bool {
	return stage == "understanding_message" || stage == "checking_observations" || stage == "verifying_answer" || stage == "composing_reply"
}

func extractScenarioSensitiveTokens(text string) []string {
	tokens := []string{}
	for _, token := range scenarioIdentifierPattern.FindAllString(text, -1) {
		if strings.ContainsAny(token, "_./:") || strings.IndexFunc(token, func(r rune) bool { return r >= '0' && r <= '9' }) >= 0 {
			tokens = append(tokens, token)
		}
	}
	tokens = append(tokens, scenarioNumberPattern.FindAllString(text, -1)...)
	tokens = append(tokens, scenarioChineseComponentPattern.FindAllString(text, -1)...)
	return tokens
}

func scenarioReplyContainsEntity(text, entity string) bool {
	entity = strings.ToLower(strings.TrimSpace(norm.NFKC.String(entity)))
	if entity == "" {
		return false
	}
	text = strings.ToLower(norm.NFKC.String(text))
	if scenarioHanPattern.MatchString(entity) {
		entity = scenarioWhitespacePattern.ReplaceAllString(entity, "")
		text = scenarioWhitespacePattern.ReplaceAllString(text, "")
		return strings.Contains(text, entity)
	}
	pattern := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(entity) + `([^A-Za-z0-9_]|$)`)
	return pattern.MatchString(text)
}

func learnerStateForAgent(state domain.ScenarioLearnerState) agentclient.LearnerState {
	state = state.Normalized()
	return agentclient.LearnerState{
		CollectedEvidence:  append([]string{}, state.CollectedEvidence...),
		RuledOutHypotheses: append([]string{}, state.RuledOutHypotheses...),
		CurrentHypothesis:  state.CurrentHypothesis,
		EstablishedFacts:   append([]string{}, state.EstablishedFacts...),
		ActionsTaken:       append([]string{}, state.ActionsTaken...),
		CurrentFocus:       state.CurrentFocus,
		EffectiveTurns:     state.EffectiveTurns,
		StalledTurns:       state.StalledTurns,
		RecentOpenings:     append([]string{}, state.RecentOpenings...),
	}
}

func scenarioTranscript(messages []domain.ScenarioMessage) []agentclient.Turn {
	turns := make([]agentclient.Turn, 0, len(messages)*2)
	for _, message := range messages {
		if text := strings.TrimSpace(message.UserContent); text != "" {
			turns = append(turns, agentclient.Turn{Role: "user", Content: text, TurnNumber: message.TurnNumber})
		}
		if text := strings.TrimSpace(message.AssistantContent); text != "" {
			turns = append(turns, agentclient.Turn{Role: "mentor", Content: text, TurnNumber: message.TurnNumber})
		}
	}
	return turns
}

func scenarioInitialRunEvents(requestID, userContent string) []domain.ScenarioRunEvent {
	return []domain.ScenarioRunEvent{
		{
			RequestID: requestID,
			Sequence:  1,
			Kind:      "user_message",
			Status:    "completed",
			Text:      userContent,
		},
	}
}

func buildScenarioRunEvents(requestID, userContent string, result agentclient.TurnResult, reply string) []domain.ScenarioRunEvent {
	events := scenarioInitialRunEvents(requestID, userContent)
	sequence := len(events) + 1
	hasGuardPassed := false
	for _, trace := range result.PublicTrace {
		event := mapScenarioPublicTraceEvent(requestID, trace, sequence)
		if trace.Kind == "guard_passed" {
			hasGuardPassed = true
		}
		if trace.Reasoning != nil {
			event.Reasoning = &domain.ScenarioPublicReasoningSummary{
				Stage: trace.Reasoning.Stage,
				Text:  trace.Reasoning.Text,
			}
		}
		if trace.Tool != nil {
			event.Tool = scenarioToolEvent(trace.Tool)
		}
		events = append(events, event)
		sequence++
	}
	if !hasGuardPassed {
		events = append(events, domain.ScenarioRunEvent{
			RequestID: requestID,
			Sequence:  sequence,
			Kind:      "guard_passed",
			Status:    "completed",
			Summary:   "回复已通过安全校验。",
		})
		sequence++
	}
	events = append(events, domain.ScenarioRunEvent{
		RequestID: requestID,
		Sequence:  sequence,
		Kind:      "proposal_approved",
		Status:    "completed",
		Summary:   "状态提议已通过校验并完成提交。",
	})
	sequence++
	for _, chunk := range chunkText(reply, 20) {
		events = append(events, domain.ScenarioRunEvent{
			RequestID: requestID,
			Sequence:  sequence,
			Kind:      "reply_delta",
			Status:    "running",
			Text:      chunk,
		})
		sequence++
	}
	events = append(events, domain.ScenarioRunEvent{
		RequestID: requestID,
		Sequence:  sequence,
		Kind:      "turn_completed",
		Status:    "completed",
		Summary:   "本轮排查已完成。",
	})
	return events
}

func mapScenarioPublicTraceEvent(requestID string, trace agentclient.PublicTraceEvent, sequence int) domain.ScenarioRunEvent {
	status := trace.Status
	if status == "" {
		status = "completed"
	}
	event := domain.ScenarioRunEvent{
		RequestID: requestID,
		Sequence:  sequence,
		Kind:      trace.Kind,
		Status:    status,
		Text:      trace.Text,
		Summary:   trace.Summary,
	}
	if trace.Reasoning != nil {
		event.Reasoning = &domain.ScenarioPublicReasoningSummary{
			Stage: trace.Reasoning.Stage,
			Text:  trace.Reasoning.Text,
		}
	}
	if trace.Tool != nil {
		event.Tool = scenarioToolEvent(trace.Tool)
	}
	return event
}

func scenarioToolEvent(tool *agentclient.ToolEventPayload) *domain.ScenarioToolEventPayload {
	if tool == nil {
		return nil
	}
	payload := &domain.ScenarioToolEventPayload{
		Name:              tool.Name,
		RedactedArguments: map[string]string{},
		DurationMS:        tool.DurationMS,
	}
	for key, value := range tool.RedactedArguments {
		payload.RedactedArguments[key] = value
	}
	if tool.Result != nil {
		payload.Result = &domain.ScenarioPublicAnswerComparison{
			Tool:          tool.Result.Tool,
			Status:        tool.Result.Status,
			UserPoints:    append([]string{}, tool.Result.UserPoints...),
			SupportStatus: tool.Result.SupportStatus,
			NextAction:    tool.Result.NextAction,
		}
	}
	return payload
}

func marshalAgentAudit(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		if value != "" {
			result[value] = true
		}
	}
	return result
}

func appendUnique(values []string, value string) []string {
	if value == "" || scenarioContainsString(values, value) {
		return values
	}
	return append(values, value)
}

func scenarioContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsAll(available, required []string) bool {
	known := stringSet(available)
	for _, value := range required {
		if !known[value] {
			return false
		}
	}
	return true
}

func intersects(left, right map[string]bool) bool {
	for value := range left {
		if right[value] {
			return true
		}
	}
	return false
}

func validScenarioFocus(value string) bool {
	switch value {
	case "logs", "metrics", "config", "change", "dependency", "data", "resource":
		return true
	default:
		return false
	}
}

func mentorOpening(reply string) string {
	line := strings.TrimSpace(strings.Split(strings.TrimSpace(reply), "\n")[0])
	runes := []rune(line)
	if len(runes) > 80 {
		runes = runes[:80]
	}
	return string(runes)
}
