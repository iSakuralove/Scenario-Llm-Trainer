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
			{Sequence: 1, Kind: "reasoning_summary_completed", Summary: "已完成本轮公开意图识别。"},
			{Sequence: 2, Kind: "mentor_buffered", Summary: "导师回复已完成私有缓冲。"},
			{Sequence: 3, Kind: "guard_passed", Summary: "回复已通过安全校验。"},
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

func (e scenarioAgentHTTPError) Error() string { return e.Message }

func scenarioRequestFingerprint(sessionID, content string) string {
	digest := sha256.Sum256([]byte(sessionID + "\x00" + strings.TrimSpace(content)))
	return hex.EncodeToString(digest[:])
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
