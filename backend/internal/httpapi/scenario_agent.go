package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"situational-teaching/backend/internal/agentclient"
	"situational-teaching/backend/internal/domain"
)

const interpreterLowConfidenceThreshold = 0.45

// scenarioStallUnlockThreshold 是卡住兜底释放的连续无进展轮次阈值。
// 必须与 agent/src/hiddenworld/runtime.py 的 STALL_UNLOCK_THRESHOLD 一致：
// Python 提前判断只是为了不发出注定被拒的提议，权威复核在这里——因为
// StalledTurns 由 Go 持有，而 is_stuck 是模型自报的，不能用来放行状态变更。
const scenarioStallUnlockThreshold = 2

var (
	scenarioIdentifierPattern         = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.:/-]{2,}`)
	scenarioNumberPattern             = regexp.MustCompile(`\d+(?:[.,]\d+)*`)
	scenarioNumberEntityPattern       = regexp.MustCompile(`^\d+(?:[.,]\d+)*$`)
	scenarioChineseComponentPattern   = regexp.MustCompile(`(?:[A-Za-z_][A-Za-z0-9_]{1,15}|[\p{Han}]{1,8})(?:表|服务|接口|主库|从库|索引|字段)`)
	scenarioHanPattern                = regexp.MustCompile(`\p{Han}`)
	scenarioWhitespacePattern         = regexp.MustCompile(`\s+`)
	scenarioSystemConfirmationPattern = regexp.MustCompile(`(?:已|已经|刚才|目前|我们已经)[^。！？!?；;]{0,18}(?:确认|核实|验证|检查完成|查看完成|查明|定位)[^。！？!?；;]{0,28}`)
	scenarioScopeExclusionPattern     = regexp.MustCompile(`(?:这一段|这部分|这一层|这一块|订单落库|数据库(?:这一段|层面)?|入口层|服务层|该环节)[^。！？!?；;]{0,18}(?:看起来|基本|整体上)?(?:正常|没什么异常|没有什么异常|没有问题|没有异常|未见异常|无异常|没异常)`)
	scenarioRemainingScopePattern     = regexp.MustCompile(`(?:剩下的|其余的|其他的|其它的)[^。！？!?；;]{0,12}(?:链路|环节|方向|部分)`)
)

type scenarioAgentClient interface {
	Turn(context.Context, agentclient.TurnRequest) (agentclient.TurnResult, error)
}

type scenarioAgentStreamingClient interface {
	TurnStream(context.Context, agentclient.TurnRequest, agentclient.StreamCallbacks) (agentclient.TurnResult, error)
}

// scenarioRawReasoningStreamEnabled 是测试调试边界；它与 Python 侧同名开关
// 对齐，默认关闭。该事件不会进入正式 RunEvent 或持久化审计。
func scenarioRawReasoningStreamEnabled() bool {
	return strings.TrimSpace(os.Getenv("HIDDENWORLD_TEST_STREAM_RAW_REASONING")) == "1"
}

type deterministicScenarioAgentClient struct{}

func (deterministicScenarioAgentClient) Turn(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
	// 该客户端只供 NewServerForTests 使用，生产入口永远走 Python Agent。
	// 测试替身不得植入一段会被误搬到生产的固定回复，直接回显本轮输入/公开题面
	// 仅用于让 HTTP 契约测试继续拥有非空正文。
	safeScenario := sanitizePublicScenario(request.PublicScenario)
	reply := strings.TrimSpace(safeScenario.Description)
	if reply == "" {
		reply = strings.TrimSpace(safeScenario.Title)
	}
	if reply == "" {
		return agentclient.TurnResult{}, errors.New("test scenario agent has no public reply source")
	}
	return agentclient.TurnResult{
		ContractVersion:  agentclient.ContractVersion,
		RequestID:        request.RequestID,
		ExpectedRevision: request.StateRevision,
		Reply:            reply,
		TurnAssessment: &agentclient.TurnAssessment{
			Intent:             "investigate",
			ClaimType:          "none",
			StudentAffect:      "engaged",
			ProgressAssessment: "no_progress",
			Actions:            []string{},
			EstablishedFacts:   []string{},
			Confidence:         0.9,
			// 瞬时情绪枚举与 Python 契约默认值对齐；缺省零值 "" 过不了校验。
			HumorLevel:            "none",
			FrustrationLevel:      "none",
			ConfusionLevel:        "none",
			ConfidenceLevel:       "low",
			UrgencyLevel:          "low",
			ConceptMasterySignals: map[string]int{},
			SkillMasterySignals:   map[string]int{},
			PreferenceSignals:     map[string]string{},
		},
		TeachingDecision: &agentclient.TeachingDecision{
			TeachingState: "normal_diagnosis",
			Strategy:      "acknowledge",
			PrimaryTask:   "interpret_evidence",
			ReplyPolicy:   "acknowledgement",
		},
		GuidanceState: agentclient.GuidanceState{
			TeachingState:      "normal_diagnosis",
			ProgressAssessment: "no_progress",
			Navigation:         []agentclient.TeachingDimensionRef{},
		},
		TurnControl: agentclient.TurnControl{AllowedActionIDs: []string{}},
		TurnAnalysis: agentclient.TurnAnalysis{
			Actions:          []string{},
			EstablishedFacts: []string{},
			StudentAffect:    "engaged",
			Confidence:       0.9,
		},
		Proposals: []agentclient.Proposal{
			{Kind: "set_stalled_turns", Value: request.LearnerState.StalledTurns + 1},
			{Kind: "record_opening", Text: mentorOpening(reply)},
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
		return scenarioAgentHTTPError{Status: http.StatusServiceUnavailable, Code: "agent_circuit_open", Message: "排查服务暂时不可用，请稍后重试"}
	case errors.Is(err, agentclient.ErrRequestTimeout):
		return scenarioAgentHTTPError{Status: http.StatusGatewayTimeout, Code: "agent_timeout", Message: "本轮处理超时，请重试"}
	case errors.Is(err, agentclient.ErrAgentUnavailable):
		return scenarioAgentHTTPError{Status: http.StatusServiceUnavailable, Code: "agent_unavailable", Message: "排查服务暂时不可用，请稍后重试"}
	}
	var versionErr agentclient.ContractVersionError
	if errors.As(err, &versionErr) {
		return scenarioAgentHTTPError{Status: http.StatusBadGateway, Code: "agent_contract_mismatch", Message: "排查服务契约不兼容"}
	}
	var httpErr agentclient.HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.Code {
		case "turn_deadline_exceeded":
			return scenarioAgentHTTPError{Status: http.StatusGatewayTimeout, Code: "agent_timeout", Message: "本轮处理超时，请重试"}
		case "public_boundary_rejected":
			return scenarioAgentHTTPError{Status: http.StatusBadGateway, Code: "agent_invalid_response", Message: "这轮没有生成完整回复，请重试"}
		case "contract_version_mismatch":
			return scenarioAgentHTTPError{Status: http.StatusBadGateway, Code: "agent_contract_mismatch", Message: "排查服务契约不兼容"}
		case "model_not_configured":
			return scenarioAgentHTTPError{Status: http.StatusServiceUnavailable, Code: "agent_not_configured", Message: "排查服务尚未配置"}
		}
		return scenarioAgentHTTPError{Status: http.StatusBadGateway, Code: "agent_upstream_error", Message: "排查服务返回异常"}
	}
	return scenarioAgentHTTPError{Status: http.StatusBadGateway, Code: "agent_invalid_response", Message: "排查服务返回了无效结果"}
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
	switch err.Error() {
	case "content is required":
		return http.StatusBadRequest, "invalid_request", "请输入排查内容"
	case "request_id is invalid":
		return http.StatusBadRequest, "invalid_request", "本轮请求标识无效，请重新发送"
	case "session not found":
		return http.StatusNotFound, "session_not_found", "排查会话不存在或已失效"
	case "session is abandoned":
		return http.StatusBadRequest, "session_abandoned", "排查会话已结束，请重新开始"
	case "session is not active":
		return http.StatusBadRequest, "session_inactive", "排查会话已结束，请重新开始"
	case "max turns reached, please submit an answer":
		return http.StatusBadRequest, "max_turns_reached", "本轮次已用完，请提交排查结论"
	default:
		// 未分类错误只写服务端日志，公共边界使用稳定文案，避免把
		// 数据库、网络、文件系统和第三方 SDK 的内部错误直接外泄。
		log.Printf("[scenario-error] unclassified_public_error type=%T error=%v", err, err)
		return http.StatusBadGateway, "scenario_turn_failed", "本轮处理失败，请重试"
	}
}

func approveScenarioProposals(
	session *domain.ScenarioSession,
	world *domain.HiddenWorld,
	result agentclient.TurnResult,
	mode scenarioValidationMode,
) (domain.ScenarioLearnerState, []scenarioProposalApproval, error) {
	if session == nil || world == nil {
		return domain.ScenarioLearnerState{}, nil, errors.New("hiddenworld session state is unavailable")
	}
	if err := validateScenarioStructuredTurn(result, world); err != nil {
		return domain.ScenarioLearnerState{}, nil, err
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
	concepts := map[string]bool{}
	if world.TeachingModel != nil {
		for _, concept := range world.TeachingModel.Concepts {
			if strings.TrimSpace(concept.ConceptID) != "" {
				concepts[concept.ConceptID] = true
			}
		}
	}
	actions := stringSet(result.TurnAnalysis.Actions)
	facts := stringSet(result.TurnAnalysis.EstablishedFacts)
	ruledOut := stringSet(result.InternalVerification.RuledOutThisTurn)
	lowConfidence := result.TurnAnalysis.Confidence < interpreterLowConfidenceThreshold
	progress := false
	stallReleases := 0
	approvals := make([]scenarioProposalApproval, 0, len(result.Proposals))
	conceptSignals := map[string]int{}
	skillSignals := map[string]int{}
	preferenceSignals := map[string]string{}
	if result.TurnAssessment != nil {
		conceptSignals = result.TurnAssessment.ConceptMasterySignals
		skillSignals = result.TurnAssessment.SkillMasterySignals
		preferenceSignals = result.TurnAssessment.PreferenceSignals
	}

	for _, proposal := range result.Proposals {
		approval := scenarioProposalApproval{Kind: proposal.Kind}
		// 闸门判定与状态变更放在同一闭包里：命中闸门时先于任何状态变更返回
		// 拒绝码，保证软拒绝（log 模式）下这条提议的副作用完全不会发生。
		rejectCode := func() string {
			gated := mode != scenarioValidationOff
			switch proposal.Kind {
			case "release_evidence":
				if gated {
					if lowConfidence {
						return "low_confidence_mutation"
					}
					node, ok := evidence[proposal.EvidenceID]
					if !ok || scenarioContainsString(state.CollectedEvidence, proposal.EvidenceID) {
						return "invalid_evidence"
					}
					if !intersects(actions, stringSet(node.ObtainedBy)) {
						return "evidence_not_requested"
					}
					if !containsAll(state.CollectedEvidence, node.Prerequisites) {
						return "evidence_prerequisite_missing"
					}
				}
				state.CollectedEvidence = append(state.CollectedEvidence, proposal.EvidenceID)
				progress = true
			case "release_evidence_on_stall":
				// 卡住兜底释放。刻意不复用 release_evidence 的 lowConfidence 与
				// evidence_not_requested 闸门——卡住的学生必然同时踩中这两条，
				// 那正是他越求助拿到的越少的原因。但也不能因此放松：这里不看
				// 模型自报的 is_stuck，只认 Go 自己持有的 StalledTurns。
				if gated {
					if stallReleases >= 1 {
						return "stall_release_limit_exceeded"
					}
					if session.LearnerState.StalledTurns < scenarioStallUnlockThreshold {
						return "stall_threshold_not_met"
					}
					node, ok := evidence[proposal.EvidenceID]
					if !ok || scenarioContainsString(state.CollectedEvidence, proposal.EvidenceID) {
						return "invalid_evidence"
					}
					if len(node.Prerequisites) > 0 {
						return "stall_release_requires_no_prerequisite"
					}
				}
				state.CollectedEvidence = append(state.CollectedEvidence, proposal.EvidenceID)
				stallReleases++
				// 不置 progress：兜底释放是系统给的，不是学生挣来的。
				// 因此 stalled_turns 继续累加，effective_turns 不推进。
			case "record_action":
				if gated && (lowConfidence || proposal.Action == "" || !actions[proposal.Action]) {
					return "action_not_in_turn_analysis"
				}
				state.ActionsTaken = appendUnique(state.ActionsTaken, proposal.Action)
			case "record_established_fact":
				if gated && (lowConfidence || proposal.Fact == "" || !facts[proposal.Fact]) {
					return "fact_not_in_turn_analysis"
				}
				before := len(state.EstablishedFacts)
				state.EstablishedFacts = appendUnique(state.EstablishedFacts, proposal.Fact)
				progress = progress || len(state.EstablishedFacts) > before
			case "set_current_hypothesis":
				if gated && (lowConfidence || !hypotheses[proposal.HypothesisID] || proposal.HypothesisID != result.TurnAnalysis.HypothesisID) {
					return "invalid_hypothesis"
				}
				if state.CurrentHypothesis != proposal.HypothesisID {
					state.CurrentHypothesis = proposal.HypothesisID
					progress = true
				}
			case "rule_out_hypothesis":
				if gated && (!hypotheses[proposal.HypothesisID] || !ruledOut[proposal.HypothesisID]) {
					return "hypothesis_not_ruled_out_this_turn"
				}
				before := len(state.RuledOutHypotheses)
				state.RuledOutHypotheses = appendUnique(state.RuledOutHypotheses, proposal.HypothesisID)
				progress = progress || len(state.RuledOutHypotheses) > before
			case "set_current_focus":
				if gated && !validScenarioFocus(proposal.Focus) {
					return "invalid_focus"
				}
				state.CurrentFocus = proposal.Focus
			case "advance_effective_turn":
				if gated && (proposal.Value != 1 || !progress) {
					return "effective_turn_without_progress"
				}
				state.EffectiveTurns++
			case "set_stalled_turns":
				expected := session.LearnerState.StalledTurns
				if progress {
					expected = 0
				} else if !result.TurnAnalysis.IsNoise {
					expected++
				}
				if gated && proposal.Value != expected {
					return "invalid_stalled_turns"
				}
				state.StalledTurns = proposal.Value
			case "record_opening":
				if gated && (proposal.Text == "" || proposal.Text != mentorOpening(result.Reply)) {
					return "opening_not_from_reply"
				}
				state.RecentOpenings = appendUnique(state.RecentOpenings, proposal.Text)
				if len(state.RecentOpenings) > 3 {
					state.RecentOpenings = append([]string{}, state.RecentOpenings[len(state.RecentOpenings)-3:]...)
				}
			case "increment_concept_mastery":
				current := state.ConceptMastery[proposal.ConceptID]
				demonstrated := conceptSignals[proposal.ConceptID]
				if gated && (!concepts[proposal.ConceptID] || proposal.Value != 1 || demonstrated <= current || current >= 4) {
					return "invalid_concept_mastery_increment"
				}
				if concepts[proposal.ConceptID] && current < 4 {
					state.ConceptMastery[proposal.ConceptID] = current + 1
					progress = true
				}
			case "increment_skill_mastery":
				current := state.SkillMastery[proposal.SkillID]
				demonstrated := skillSignals[proposal.SkillID]
				if gated && (!scenarioSkillIDAllowed(proposal.SkillID) || proposal.Value != 1 || demonstrated <= current || current >= 4) {
					return "invalid_skill_mastery_increment"
				}
				if scenarioSkillIDAllowed(proposal.SkillID) && current < 4 {
					state.SkillMastery[proposal.SkillID] = current + 1
					progress = true
				}
			case "set_explanation_preference":
				if gated && (preferenceSignals[proposal.PreferenceKey] != proposal.PreferenceValue ||
					!scenarioExplanationPreferenceAllowed(proposal.PreferenceKey, proposal.PreferenceValue)) {
					return "invalid_explanation_preference"
				}
				scenarioSetExplanationPreference(&state.ExplanationPreferences, proposal.PreferenceKey, proposal.PreferenceValue)
			case "set_hint_level":
				if gated && !scenarioHintTransitionAllowed(session.LearnerState.HintLevel, proposal.Value, result.TurnAssessment) {
					return "invalid_hint_level"
				}
				state.HintLevel = proposal.Value
				if state.HintLevel == 0 {
					state.LastHint = ""
				}
			case "set_last_hint":
				if gated && !scenarioHintTextAllowed(world, proposal.Text) {
					return "invalid_hint_text"
				}
				state.LastHint = strings.TrimSpace(proposal.Text)
			default:
				// 未知提议类型在任何模式下都无变更逻辑可执行，只能拒绝。
				return "unsupported_proposal_kind"
			}
			return ""
		}()
		if rejectCode != "" {
			approval.Accepted = false
			approval.ReasonCode = rejectCode
			approvals = append(approvals, approval)
			// strict：一条被拒整轮失败（历史行为）；log：软拒绝，记审计后
			// 跳过这条提议继续；off 到不了这里（闸门已全部跳过，除非未知类型）。
			if mode == scenarioValidationLog {
				continue
			}
			return domain.ScenarioLearnerState{}, approvals, fmt.Errorf("proposal %s rejected: %s", proposal.Kind, rejectCode)
		}
		approval.Accepted = true
		approval.ReasonCode = "approved"
		approvals = append(approvals, approval)
	}
	// repair_status 只能由可信的内部答案比较结果驱动；绝不接受模型通过
	// GuidanceState 或 proposal 自行提升修复闭环状态。
	state.RepairStatus = worldRepairStatus(result.InternalVerification.AnswerComparison, state.RepairStatus)
	return state.Normalized(), approvals, nil
}

func scenarioRepairStatusAllowed(value string) bool {
	return value == "none" || value == "partial" || value == "sufficient"
}

func worldRepairStatus(comparison *agentclient.InternalAnswerComparison, fallback string) string {
	if comparison == nil {
		if scenarioRepairStatusAllowed(fallback) {
			return fallback
		}
		return "none"
	}
	if comparison.SolutionCoverage <= 0 {
		return "none"
	}
	if comparison.SolutionCoverage >= 1 {
		return "sufficient"
	}
	return "partial"
}

func scenarioSkillIDAllowed(value string) bool {
	switch value {
	case "log_reading", "causal_reasoning", "cross_layer_debugging":
		return true
	default:
		return false
	}
}

func scenarioExplanationPreferenceAllowed(key, value string) bool {
	switch key {
	case "detail":
		return value == "brief" || value == "balanced" || value == "detailed"
	case "analogy", "directness":
		return value == "low" || value == "medium" || value == "high"
	default:
		return false
	}
}

func scenarioSetExplanationPreference(preferences *domain.ScenarioExplanationPreferences, key, value string) {
	if preferences == nil || !scenarioExplanationPreferenceAllowed(key, value) {
		return
	}
	switch key {
	case "detail":
		preferences.Detail = value
	case "analogy":
		preferences.Analogy = value
	case "directness":
		preferences.Directness = value
	}
}

func scenarioHintTransitionAllowed(current, next int, assessment *agentclient.TurnAssessment) bool {
	if next < 0 || next > 4 || next < current-1 || next > current+1 {
		return false
	}
	if next > current {
		return assessment != nil && (assessment.IsStuck || assessment.RandomInvestigation)
	}
	if next < current {
		return assessment != nil && (assessment.ProgressAssessment == "progress" || assessment.ProgressAssessment == "partial")
	}
	return true
}

func scenarioHintTextAllowed(world *domain.HiddenWorld, text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	if world == nil || world.TeachingModel == nil {
		return false
	}
	for _, hint := range world.TeachingModel.HintLadder {
		if strings.TrimSpace(hint.PublicHint) == text {
			return true
		}
	}
	return false
}

const legacyStructuredResponseEnv = "SCENARIO_ALLOW_LEGACY_STRUCTURED_RESPONSE"

// validateScenarioStructuredTurn 是 Go 侧的结构化回合边界。结构化字段在
// 正式主链中是必填的；旧扁平响应只能通过显式环境开关进入兼容路径，不能
// 因为 Go struct 的零值而被静默当成合法 V2 响应。
func validateScenarioStructuredTurn(result agentclient.TurnResult, world *domain.HiddenWorld) error {
	legacy := scenarioLegacyStructuredResponseAllowed()
	if !legacy {
		if result.TurnAssessment == nil {
			return errors.New("turn assessment is required")
		}
		if result.TeachingDecision == nil {
			return errors.New("teaching decision is required")
		}
		if result.GuidanceState.Navigation == nil {
			return errors.New("guidance state navigation is required")
		}
		if result.TurnControl.AllowedActionIDs == nil {
			return errors.New("turn control allowed_action_ids is required")
		}
	}
	if result.TurnAssessment != nil {
		assessment := result.TurnAssessment
		if assessment.Confidence < 0 || assessment.Confidence > 1 {
			return errors.New("turn assessment confidence is invalid")
		}
		if !scenarioTurnIntentAllowed(assessment.Intent) || !scenarioClaimTypeAllowed(assessment.ClaimType) ||
			!scenarioProgressAssessmentAllowed(assessment.ProgressAssessment) ||
			!scenarioStudentAffectAllowed(assessment.StudentAffect) ||
			!scenarioInstantLevelAllowed(assessment.HumorLevel, "none", "light", "strong") ||
			!scenarioInstantLevelAllowed(assessment.FrustrationLevel, "none", "light", "high") ||
			!scenarioInstantLevelAllowed(assessment.ConfusionLevel, "none", "light", "high") ||
			!scenarioInstantLevelAllowed(assessment.ConfidenceLevel, "low", "medium", "high") ||
			!scenarioInstantLevelAllowed(assessment.UrgencyLevel, "low", "medium", "high") {
			return errors.New("turn assessment enum is invalid")
		}
		if assessment.Actions == nil || assessment.EstablishedFacts == nil ||
			assessment.ConceptMasterySignals == nil || assessment.SkillMasterySignals == nil ||
			assessment.PreferenceSignals == nil {
			return errors.New("turn assessment arrays are required")
		}
		if err := validateScenarioAssessmentConsistency(*assessment, result.TurnAnalysis); err != nil {
			return err
		}
	}
	if decision := result.TeachingDecision; decision != nil {
		if !scenarioTeachingStateAllowed(decision.TeachingState) || !scenarioTeachingStrategyAllowed(decision.Strategy) ||
			!scenarioPrimaryTeachingTaskAllowed(decision.PrimaryTask) || !scenarioReplyPolicyAllowed(decision.ReplyPolicy) {
			return errors.New("teaching decision enum is invalid")
		}
		// 这两个开关在契约中固定为 False；Go 不允许 Agent 打开它们。
		if decision.AllowExplicitNextStep || decision.AllowRuledOutScope {
			return errors.New("teaching decision contains forbidden permission")
		}
	}
	if state := result.GuidanceState; state.StalledTurns < 0 {
		return errors.New("guidance state stalled_turns is invalid")
	} else if (!legacy && (state.TeachingState == "" || state.ProgressAssessment == "")) ||
		(state.TeachingState != "" && !scenarioTeachingStateAllowed(state.TeachingState)) ||
		(state.ProgressAssessment != "" && !scenarioProgressAssessmentAllowed(state.ProgressAssessment)) {
		return errors.New("guidance state enum is invalid")
	} else {
		if state.RepairStatus != "" && !scenarioRepairStatusAllowed(state.RepairStatus) {
			return errors.New("guidance state repair_status is invalid")
		}
		for _, dimension := range state.Navigation {
			if strings.TrimSpace(dimension.DimensionID) == "" || !scenarioDimensionCategoryAllowed(dimension.Category) ||
				!scenarioDimensionStatusAllowed(dimension.Status) || !scenarioHintLevelAllowed(dimension.HintLevel) {
				return errors.New("guidance state navigation is invalid")
			}
		}
	}
	if !legacy && result.TurnAssessment != nil && result.TeachingDecision != nil {
		if result.GuidanceState.TeachingState != result.TeachingDecision.TeachingState {
			return errors.New("guidance state teaching_state disagrees with teaching decision")
		}
		if result.GuidanceState.ProgressAssessment != result.TurnAssessment.ProgressAssessment {
			return errors.New("guidance state progress_assessment disagrees with turn assessment")
		}
	}
	if err := validateScenarioTurnControl(result, world, legacy); err != nil {
		return err
	}
	for _, actionID := range result.TurnControl.AllowedActionIDs {
		// ActionCatalog 只允许题目声明的虚拟观察动作。compare_answer
		// 是 Runtime 内部能力，永远不能进入学生可点击动作目录。
		if !scenarioActionCatalogContains(world, actionID) {
			return errors.New("turn control contains an undeclared or internal action")
		}
	}
	return nil
}

func scenarioLegacyStructuredResponseAllowed() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(legacyStructuredResponseEnv)))
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func validateScenarioAssessmentConsistency(assessment agentclient.TurnAssessment, analysis agentclient.TurnAnalysis) error {
	if !equalScenarioStrings(assessment.Actions, analysis.Actions) ||
		assessment.HypothesisID != analysis.HypothesisID ||
		assessment.HypothesisRaw != analysis.HypothesisRaw ||
		assessment.MadeClaim != analysis.MadeClaim ||
		assessment.ContainsAnswerAttempt != analysis.ContainsAnswerAttempt ||
		assessment.AnswerAttemptText != analysis.AnswerAttemptText ||
		!equalScenarioStrings(assessment.EstablishedFacts, analysis.EstablishedFacts) ||
		assessment.IsStuck != analysis.IsStuck || assessment.IsNoise != analysis.IsNoise ||
		assessment.StudentAffect != analysis.StudentAffect ||
		assessment.Confidence != analysis.Confidence ||
		assessment.RequestedActionRaw != analysis.RequestedActionRaw ||
		assessment.ClarificationTarget != analysis.ClarificationTarget ||
		assessment.ActionMatchStatus != analysis.ActionMatchStatus {
		return fmt.Errorf(
			"turn assessment disagrees with turn analysis: assessment={actions=%v hypothesis_id=%q hypothesis_raw=%q made_claim=%t answer=%t answer_text=%q facts=%v stuck=%t noise=%t affect=%q confidence=%g requested=%q clarification=%q match=%q} analysis={actions=%v hypothesis_id=%q hypothesis_raw=%q made_claim=%t answer=%t answer_text=%q facts=%v stuck=%t noise=%t affect=%q confidence=%g requested=%q clarification=%q match=%q}",
			assessment.Actions, assessment.HypothesisID, assessment.HypothesisRaw, assessment.MadeClaim,
			assessment.ContainsAnswerAttempt, assessment.AnswerAttemptText, assessment.EstablishedFacts,
			assessment.IsStuck, assessment.IsNoise, assessment.StudentAffect, assessment.Confidence,
			assessment.RequestedActionRaw, assessment.ClarificationTarget, assessment.ActionMatchStatus,
			analysis.Actions, analysis.HypothesisID, analysis.HypothesisRaw, analysis.MadeClaim,
			analysis.ContainsAnswerAttempt, analysis.AnswerAttemptText, analysis.EstablishedFacts,
			analysis.IsStuck, analysis.IsNoise, analysis.StudentAffect, analysis.Confidence,
			analysis.RequestedActionRaw, analysis.ClarificationTarget, analysis.ActionMatchStatus,
		)
	}
	if assessment.ContainsAnswerAttempt && strings.TrimSpace(assessment.AnswerAttemptText) == "" {
		return errors.New("answer attempt is marked present but has no text")
	}
	if !assessment.ContainsAnswerAttempt && strings.TrimSpace(assessment.AnswerAttemptText) != "" {
		return errors.New("answer attempt text is present while answer attempt is false")
	}
	if assessment.MadeClaim && (assessment.ClaimType == "none" || assessment.ClaimType == "question" || assessment.ClaimType == "meta") {
		return errors.New("made claim has a non-claim claim_type")
	}
	if !assessment.MadeClaim && (assessment.ClaimType == "observation" || assessment.ClaimType == "hypothesis" || assessment.ClaimType == "answer") {
		return errors.New("claim_type indicates a claim while made_claim is false")
	}
	return nil
}

func validateScenarioTurnControl(result agentclient.TurnResult, world *domain.HiddenWorld, legacy bool) error {
	control := result.TurnControl
	if !legacy {
		// terminal 是会话生命周期状态，只能由 Go 持有的会话状态回注；
		// Agent 不得在本轮打开它。completion_allowed 与 completion_ready
		// 则是答案裁判投影，二者故意不等价：没有执行 compare_answer 时，
		// 即便证据已足，ready 仍为 false。
		if control.Terminal {
			return errors.New("agent cannot set terminal")
		}
		if control.CompletionAllowed != result.InternalVerification.CompletionAllowed {
			return errors.New("turn control completion_allowed disagrees with verification")
		}
		expectedReady := result.InternalVerification.AnswerComparison != nil && result.InternalVerification.CompletionAllowed
		if control.CompletionReady != expectedReady {
			return errors.New("turn control completion_ready disagrees with verification")
		}
		if comparison := result.InternalVerification.AnswerComparison; comparison != nil &&
			comparison.CompletionAllowed != control.CompletionAllowed {
			return errors.New("turn control completion_allowed disagrees with answer comparison")
		}
	}
	seen := map[string]bool{}
	for _, actionID := range control.AllowedActionIDs {
		if strings.EqualFold(strings.TrimSpace(actionID), "compare_answer") {
			return errors.New("compare_answer is not an allowed action")
		}
		if seen[actionID] {
			return errors.New("turn control contains duplicate action")
		}
		seen[actionID] = true
	}
	for _, actionID := range result.TurnAnalysis.Actions {
		if strings.EqualFold(strings.TrimSpace(actionID), "compare_answer") {
			return errors.New("compare_answer is not a learner action")
		}
		if !scenarioActionCatalogContains(world, actionID) {
			return errors.New("turn analysis contains an undeclared or internal action")
		}
	}
	if result.TurnAssessment != nil {
		for _, actionID := range result.TurnAssessment.Actions {
			if strings.EqualFold(strings.TrimSpace(actionID), "compare_answer") {
				return errors.New("compare_answer is not a learner action")
			}
		}
	}
	return nil
}

func scenarioActionCatalogContains(world *domain.HiddenWorld, actionID string) bool {
	if world == nil || strings.TrimSpace(actionID) == "" {
		return false
	}
	if len(world.VirtualTools) > 0 {
		for _, tool := range world.VirtualTools {
			if tool.ObservationAction == actionID {
				return true
			}
		}
		return false
	}
	for _, observation := range world.Observations {
		if observation.Action == actionID {
			return true
		}
	}
	return false
}

func equalScenarioStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func scenarioTurnIntentAllowed(value string) bool {
	switch value {
	case "answer", "probe_plan", "hypothesis", "request_hint", "direct_answer_request", "chat", "off_topic", "garbage", "stuck", "contradiction", "meta", "investigate", "clarification", "explanation_request", "answer_attempt", "help_request":
		return true
	default:
		return false
	}
}

func scenarioClaimTypeAllowed(value string) bool {
	switch value {
	case "none", "observation", "hypothesis", "answer", "question", "meta":
		return true
	default:
		return false
	}
}

func scenarioStudentAffectAllowed(value string) bool {
	switch value {
	case "engaged", "confused", "frustrated", "disengaged":
		return true
	default:
		return false
	}
}

func scenarioInstantLevelAllowed(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func scenarioProgressAssessmentAllowed(value string) bool {
	switch value {
	case "progress", "partial", "no_progress", "unsupported", "contradictory", "leak_risk", "unknown":
		return true
	default:
		return false
	}
}

func scenarioTeachingStateAllowed(value string) bool {
	switch value {
	case "guided_inquiry", "unsupported_hypothesis", "anti_guess_detected", "premature_conclusion", "conclusion_grilling", "evidence_reconstruction", "normal_diagnosis", "debrief", "casual_chat", "clarification", "off_topic", "garbage":
		return true
	default:
		return false
	}
}

func scenarioTeachingStrategyAllowed(value string) bool {
	switch value {
	case "observe", "acknowledge", "reflect", "clarify", "debrief", "chat", "recover", "silence":
		return true
	default:
		return false
	}
}

func scenarioPrimaryTeachingTaskAllowed(value string) bool {
	switch value {
	case "explain_concept", "interpret_evidence", "acknowledge_progress", "correct_conclusion", "release_hint", "redirect_investigation", "close_investigation":
		return true
	default:
		return false
	}
}

func scenarioReplyPolicyAllowed(value string) bool {
	switch value {
	case "neutral_summary", "tool_result_only", "acknowledgement", "reflective_question", "casual_reply", "no_reply":
		return true
	default:
		return false
	}
}

func scenarioDimensionCategoryAllowed(value string) bool {
	switch value {
	case "evidence", "causal", "temporal", "dependency", "capacity", "configuration", "verification", "resource", "data":
		return true
	default:
		return false
	}
}

func scenarioDimensionStatusAllowed(value string) bool {
	return value == "unexplored" || value == "in_progress" || value == "covered"
}

func scenarioHintLevelAllowed(value string) bool {
	return value == "none" || value == "light" || value == "direct"
}

func validateScenarioReply(
	reply string,
	world *domain.HiddenWorld,
	publicScenario *domain.PublicScenario,
	state domain.ScenarioLearnerState,
	observationTexts ...string,
) error {
	for _, term := range []string{
		"下一步",
		"接下来",
		"建议检查",
		"建议查看",
		"建议核对",
		"稍后再试",
		"可以稍后",
		"继续梳理",
		"继续分析",
		"排除范围",
		"排除性观察",
		"问题不在",
		"根因在",
	} {
		if strings.Contains(reply, term) {
			return fmt.Errorf("reply contains forbidden guidance term %q", term)
		}
	}
	if scenarioSystemConfirmationPattern.MatchString(reply) ||
		scenarioScopeExclusionPattern.MatchString(reply) ||
		scenarioRemainingScopePattern.MatchString(reply) {
		return errors.New("reply contains internal confirmation or exclusion framing")
	}
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
	publicEntities := map[string]bool{}
	publicSources := scenarioPublicScenarioSources(publicScenario)
	publicText := strings.Join(publicSources, "\n")
	for _, source := range publicSources {
		for _, entity := range extractScenarioSensitiveTokens(source) {
			publicEntities[scenarioEntityKey(entity)] = true
		}
	}
	for entity := range stringSet(entities) {
		if publicEntities[scenarioEntityKey(entity)] || scenarioReplyContainsEntity(publicText, entity) {
			continue
		}
		if scenarioReplyContainsEntity(reply, entity) {
			return errors.New("reply contains unreleased entity")
		}
	}
	for _, observation := range observationTexts {
		if scenarioReplyRepeatsObservation(reply, observation) {
			return errors.New("reply repeats public observation")
		}
	}
	return nil
}

func scenarioPublicObservationTexts(traces []agentclient.PublicTraceEvent) []string {
	texts := make([]string, 0)
	for _, trace := range traces {
		if trace.Kind != "observation_result" || trace.Observation == nil {
			continue
		}
		if text := strings.TrimSpace(trace.Observation.Result); text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

func scenarioReplyRepeatsObservation(reply, observation string) bool {
	replyChars := scenarioReplayChars(reply)
	observationChars := scenarioReplayChars(observation)
	if len(replyChars) < 18 || len(observationChars) < 24 {
		return false
	}
	replyShingles := scenarioReplayShingles(replyChars)
	observationShingles := scenarioReplayShingles(observationChars)
	shared := 0
	for shingle := range replyShingles {
		if observationShingles[shingle] {
			shared++
		}
	}
	observationCoverage := float64(shared) / float64(len(observationShingles))
	replyCoverage := float64(shared) / float64(len(replyShingles))
	return (shared >= 5 && replyCoverage >= 0.18) || (shared >= 10 && observationCoverage >= 0.12)
}

func scenarioReplayChars(value string) []rune {
	normalized := strings.ToLower(norm.NFKC.String(value))
	chars := make([]rune, 0, len([]rune(normalized)))
	for _, char := range normalized {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || unicode.In(char, unicode.Han) {
			chars = append(chars, char)
		}
	}
	return chars
}

func scenarioReplayShingles(chars []rune) map[string]bool {
	shingles := make(map[string]bool)
	for index := 0; index+3 <= len(chars); index++ {
		shingles[string(chars[index:index+3])] = true
	}
	return shingles
}

func validateScenarioPublicTrace(
	requestID string,
	userContent string,
	result agentclient.TurnResult,
	world *domain.HiddenWorld,
	publicScenario *domain.PublicScenario,
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
	allowedKinds := map[string]bool{
		"reasoning_summary_delta":     true,
		"reasoning_summary_completed": true,
		"observation_result":          true,
		"tool_started":                true,
		"tool_result":                 true,
		"tool_completed":              true,
		"agent_tool_started":          true,
		"agent_tool_result":           true,
		"response_summary":            true,
		"mentor_buffered":             true,
		"guard_passed":                true,
	}

	previousSequence := 0
	hasCompareResult := false
	shouldCompareAnswer := result.TurnAnalysis.ContainsAnswerAttempt && result.TurnAnalysis.Confidence >= interpreterLowConfidenceThreshold
	normalizedUserContent := strings.ToLower(norm.NFKC.String(userContent))
	observationTexts := scenarioPublicObservationTexts(result.PublicTrace)

	for _, trace := range result.PublicTrace {
		if trace.Sequence <= previousSequence {
			return errors.New("public trace sequence is not strictly increasing")
		}
		previousSequence = trace.Sequence
		// V2 起 Go 不再强制固定 phase 顺序，也不要求 guard_passed 恰好一条：
		// 旧阶段事件只做白名单放行，正式事件由 Go 投影为判别联合。
		if !allowedKinds[trace.Kind] {
			return fmt.Errorf("public trace kind %q is not allowed from Python", trace.Kind)
		}
		if !allowedStatuses[trace.Status] {
			return fmt.Errorf("public trace status %q is invalid", trace.Status)
		}
		if trace.Text != "" {
			return errors.New("Python public trace cannot publish reply text")
		}
		if trace.Summary != "" {
			if err := validateScenarioReply(trace.Summary, world, publicScenario, state, observationTexts...); err != nil {
				return fmt.Errorf("public trace summary rejected: %w", err)
			}
		}
		if trace.Reasoning != nil {
			if !allowedReasoningStages[trace.Reasoning.Stage] || strings.TrimSpace(trace.Reasoning.Text) == "" {
				return errors.New("public reasoning summary is invalid")
			}
			if err := validateScenarioReply(trace.Reasoning.Text, world, publicScenario, state, observationTexts...); err != nil {
				return fmt.Errorf("public reasoning summary rejected: %w", err)
			}
		}
		if trace.Kind == "observation_result" {
			if trace.Observation == nil || strings.TrimSpace(trace.Observation.Action) == "" || strings.TrimSpace(trace.Observation.Result) == "" {
				return errors.New("public observation result is invalid")
			}
			if !stringSet(result.TurnAnalysis.Actions)[trace.Observation.Action] {
				return errors.New("public observation is not sourced from the current actions")
			}
			matched := false
			for _, observation := range world.Observations {
				if observation.Action == trace.Observation.Action {
					matched = true
					break
				}
			}
			if !matched {
				return errors.New("public observation action is not configured")
			}
			if err := validateScenarioPublicObservation(trace.Observation, world); err != nil {
				return err
			}
		}

		isToolEvent := trace.Kind == "tool_started" || trace.Kind == "tool_result" || trace.Kind == "tool_completed"
		if trace.Kind == "agent_tool_started" || trace.Kind == "agent_tool_result" {
			// 循环内实时旁路：与流式校验同一套规则，观察负载逐字比对题目声明。
			if trace.Tool != nil {
				return errors.New("agent tool trace must not carry compare_answer payload")
			}
			if !scenarioPublicObservationToolName(world, trace.ToolName) {
				return errors.New("agent tool trace references an undeclared observation action")
			}
			switch trace.Kind {
			case "agent_tool_started":
				if trace.Status != "started" || trace.Observation != nil || trace.DurationMS != 0 {
					return errors.New("agent tool start event is invalid")
				}
			case "agent_tool_result":
				if trace.Status != "completed" && trace.Status != "failed" {
					return errors.New("agent tool result status is invalid")
				}
				if trace.Observation != nil {
					if trace.Status != "completed" {
						return errors.New("agent tool result observation requires completed status")
					}
					if err := validateScenarioPublicObservation(trace.Observation, world); err != nil {
						return err
					}
				}
			}
		} else if !isToolEvent {
			if trace.Tool != nil || trace.ToolName != "" || trace.DurationMS != 0 {
				return errors.New("non-tool public trace contains tool payload")
			}
			if trace.Kind != "observation_result" && trace.Observation != nil {
				return errors.New("non-observation public trace contains observation payload")
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
			// compare_answer 已迁移为无参数工具：Runtime 自动绑定当前轮
			// AnswerAttempt，模型无法构造候选答案参数。任何残留参数都拒绝。
			if len(tool.RedactedArguments) != 0 {
				return errors.New("compare_answer must not carry model-supplied arguments")
			}
			switch trace.Kind {
			case "tool_started":
				if trace.Status != "started" || tool.Result != nil {
					return errors.New("compare_answer start event is invalid")
				}
			case "tool_result":
				if trace.Status != "completed" || tool.Result == nil {
					return errors.New("compare_answer result event is invalid")
				}
				hasCompareResult = true
			case "tool_completed":
				if trace.Status != "completed" || tool.Result == nil {
					return errors.New("compare_answer completion event is invalid")
				}
				hasCompareResult = true
			}
			if tool.Result != nil {
				if err := validateScenarioPublicComparison(tool.Result, userContent, world, publicScenario, state); err != nil {
					return err
				}
				for _, point := range tool.Result.UserPoints {
					normalizedPoint := strings.ToLower(strings.TrimSpace(norm.NFKC.String(point)))
					if normalizedPoint == "" || !strings.Contains(normalizedUserContent, normalizedPoint) {
						return errors.New("public compare_answer point was not sourced from the user message")
					}
				}
			}
		}
	}
	if shouldCompareAnswer && !hasCompareResult {
		return errors.New("answer attempt is missing the compare_answer result")
	}
	if !shouldCompareAnswer && hasCompareResult {
		return errors.New("ordinary turn contains a compare_answer result")
	}
	return nil
}

type scenarioPublicTraceStream struct {
	requestID        string
	userContent      string
	world            *domain.HiddenWorld
	publicScenario   *domain.PublicScenario
	state            domain.ScenarioLearnerState
	analysis         *agentclient.TurnAnalysis
	lastSequence     int
	hasCompareResult bool
	emittedCount     int
	mode             scenarioValidationMode
	// bypasses 收集 log 模式下放行的违规，轮次结束后统一落审计。
	bypasses []string
	// lastAccepted 供流式投影层判断当前事件是否可以安全落到正式事件流。
	// 任何模式下 onPublicTrace 都不会因为单条旁路事件失败而中断主流；违规
	// 事件必须被丢弃，不能把未知 payload 透传到前端或业务历史。
	lastAccepted bool
}

func newScenarioPublicTraceStream(
	requestID string,
	userContent string,
	world *domain.HiddenWorld,
	publicScenario *domain.PublicScenario,
	state domain.ScenarioLearnerState,
	mode scenarioValidationMode,
) *scenarioPublicTraceStream {
	return &scenarioPublicTraceStream{
		requestID:      requestID,
		userContent:    userContent,
		world:          world,
		publicScenario: publicScenario,
		state:          state,
		mode:           mode,
	}
}

func (stream *scenarioPublicTraceStream) onTurnAnalysis(analysis agentclient.TurnAnalysis) error {
	stream.analysis = &analysis
	return nil
}

func (stream *scenarioPublicTraceStream) onPublicTrace(trace agentclient.PublicTraceEvent) error {
	stream.lastAccepted = false
	if err := stream.validate(trace); err != nil {
		// 公开过程事件是旁路信息，不是最终结果契约。无论迁移闸门处于
		// strict/log，单条 trace 的协议或内容问题都只丢弃该条并记审计，
		// 继续消费后续 trace、reply_delta 和 result。否则一条旧 Agent
		// 事件会把已经产生的正文一起截断，前端只能看到“本轮失败”。
		// 真正不可恢复的错误仍由 agentclient 的 result 解码/最终回合校验
		// 返回，不在这里升级。
		stream.bypasses = append(stream.bypasses, err.Error())
		return nil
	}
	stream.emittedCount++
	stream.lastAccepted = true
	return nil
}

// drainBypasses 把流中放行的违规落审计并清空，供轮次收尾调用。
func (stream *scenarioPublicTraceStream) drainBypasses(s *Server) {
	for _, violation := range stream.bypasses {
		s.recordScenarioValidationBypass("public_trace_stream", stream.requestID, violation)
	}
	stream.bypasses = nil
}

func (stream *scenarioPublicTraceStream) validate(trace agentclient.PublicTraceEvent) error {
	if stream.mode == scenarioValidationOff {
		// off：跳过全部协议闸门。序号仍需推进，否则 rebuildScenarioRunEvents
		// 拆分 delta 前后事件段时序号会错位。
		if trace.Sequence <= stream.lastSequence {
			return errors.New("public trace sequence is not strictly increasing")
		}
		stream.lastSequence = trace.Sequence
		return nil
	}
	// V2 起 Go 不再强制固定 phase 顺序，也不要求 guard_passed 恰好一条：
	// 旧阶段事件只做白名单放行，正式事件由 Go 投影为判别联合。
	allowedKinds := map[string]bool{
		"reasoning_summary_delta":     true,
		"reasoning_summary_completed": true,
		"observation_result":          true,
		"tool_started":                true,
		"tool_result":                 true,
		"tool_completed":              true,
		"agent_tool_started":          true,
		"agent_tool_result":           true,
		"response_summary":            true,
		"mentor_buffered":             true,
		"guard_passed":                true,
	}
	if !allowedKinds[trace.Kind] {
		return fmt.Errorf("public trace kind %q is not allowed from Python", trace.Kind)
	}
	if trace.Sequence <= stream.lastSequence {
		return errors.New("public trace sequence is not strictly increasing")
	}
	if !scenarioTraceStatusAllowed(trace.Status) {
		return fmt.Errorf("public trace status %q is invalid", trace.Status)
	}
	if trace.Text != "" {
		return errors.New("Python public trace cannot publish reply text")
	}
	if trace.Summary != "" {
		if err := validateScenarioReply(trace.Summary, stream.world, stream.publicScenario, stream.state); err != nil {
			return fmt.Errorf("public trace summary rejected: %w", err)
		}
	}
	if trace.Reasoning != nil {
		if !scenarioReasoningStageAllowed(trace.Reasoning.Stage) || strings.TrimSpace(trace.Reasoning.Text) == "" {
			return errors.New("public reasoning summary is invalid")
		}
		if err := validateScenarioReply(trace.Reasoning.Text, stream.world, stream.publicScenario, stream.state); err != nil {
			return fmt.Errorf("public reasoning summary rejected: %w", err)
		}
	}
	if trace.Kind == "observation_result" {
		if stream.analysis == nil || trace.Observation == nil || strings.TrimSpace(trace.Observation.Action) == "" || strings.TrimSpace(trace.Observation.Result) == "" {
			return errors.New("public observation result is invalid")
		}
		if !stringSet(stream.analysis.Actions)[trace.Observation.Action] {
			return errors.New("public observation is not sourced from the current actions")
		}
		matched := false
		for _, observation := range stream.world.Observations {
			if observation.Action == trace.Observation.Action {
				matched = true
				break
			}
		}
		if !matched {
			return errors.New("public observation action is not configured")
		}
		if err := validateScenarioPublicObservation(trace.Observation, stream.world); err != nil {
			return err
		}
	}
	if trace.Kind == "agent_tool_started" || trace.Kind == "agent_tool_result" {
		// Python AgentLoop 的循环内实时旁路。工具名必须是题目声明的公开观察
		// 动作；带观察负载时结果文本仍须与题目声明逐字一致，防伪造不变。
		if trace.Tool != nil {
			return errors.New("agent tool trace must not carry compare_answer payload")
		}
		if !scenarioPublicObservationToolName(stream.world, trace.ToolName) {
			return errors.New("agent tool trace references an undeclared observation action")
		}
		switch trace.Kind {
		case "agent_tool_started":
			if trace.Status != "started" || trace.Observation != nil || trace.DurationMS != 0 {
				return errors.New("agent tool start event is invalid")
			}
		case "agent_tool_result":
			if trace.Status != "completed" && trace.Status != "failed" {
				return errors.New("agent tool result status is invalid")
			}
			if trace.DurationMS < 0 {
				return errors.New("agent tool result duration is invalid")
			}
			if trace.Observation != nil {
				if trace.Status != "completed" {
					return errors.New("agent tool result observation requires completed status")
				}
				if err := validateScenarioPublicObservation(trace.Observation, stream.world); err != nil {
					return err
				}
			}
		}
	} else if trace.Kind != "tool_started" && trace.Kind != "tool_result" && trace.Kind != "tool_completed" {
		if trace.Tool != nil || trace.ToolName != "" || trace.DurationMS != 0 {
			return errors.New("non-tool public trace contains tool payload")
		}
		if trace.Kind != "observation_result" && trace.Observation != nil {
			return errors.New("non-observation public trace contains observation payload")
		}
	} else {
		if stream.analysis == nil || !stream.analysis.ContainsAnswerAttempt || stream.analysis.Confidence < interpreterLowConfidenceThreshold {
			return errors.New("compare_answer trace is not allowed for this turn")
		}
		if err := stream.validateTool(trace); err != nil {
			return err
		}
	}
	// 只在整条事件通过所有字段校验后推进序号。坏事件被丢弃时不应以
	// 其伪造的高序号污染后续合法事件的顺序检查。
	stream.lastSequence = trace.Sequence
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
	// compare_answer 已迁移为无参数工具：Runtime 自动绑定当前轮 AnswerAttempt。
	if len(tool.RedactedArguments) != 0 {
		return errors.New("compare_answer must not carry model-supplied arguments")
	}
	switch trace.Kind {
	case "tool_started":
		if trace.Status != "started" || tool.Result != nil {
			return errors.New("compare_answer start event is invalid")
		}
	case "tool_result":
		if trace.Status != "completed" || tool.Result == nil {
			return errors.New("compare_answer result event is invalid")
		}
	case "tool_completed":
		if trace.Status != "completed" || tool.Result == nil {
			return errors.New("compare_answer completion event is invalid")
		}
	}
	if tool.Result != nil {
		if err := validateScenarioPublicComparison(tool.Result, stream.userContent, stream.world, stream.publicScenario, stream.state); err != nil {
			return err
		}
		if trace.Kind == "tool_result" || trace.Kind == "tool_completed" {
			stream.hasCompareResult = true
		}
	}
	return nil
}

func validateScenarioPublicComparison(
	comparison *agentclient.PublicAnswerComparison,
	userContent string,
	world *domain.HiddenWorld,
	publicScenario *domain.PublicScenario,
	state domain.ScenarioLearnerState,
) error {
	conclusionAllowed := map[string]bool{"none": true, "partial": true, "supported": true, "contradictory": true}
	evidenceAllowed := map[string]bool{"none": true, "insufficient": true, "partial": true, "sufficient": true}
	causalAllowed := map[string]bool{"missing": true, "partial": true, "sufficient": true}
	dimensionAllowed := map[string]bool{"conclusion": true, "evidence": true, "causal_link": true, "consistency": true}
	if comparison.Tool != "compare_answer" || comparison.Status != "completed" ||
		!conclusionAllowed[comparison.ConclusionStatus] || !evidenceAllowed[comparison.EvidenceStatus] ||
		!causalAllowed[comparison.CausalStatus] || comparison.MissingDimensions == nil ||
		comparison.Contradictions == nil {
		return errors.New("public compare_answer result is invalid")
	}
	for _, dimension := range comparison.MissingDimensions {
		if !dimensionAllowed[dimension] {
			return errors.New("public compare_answer contains an invalid missing dimension")
		}
	}
	normalizedUserContent := strings.ToLower(norm.NFKC.String(userContent))
	for _, point := range comparison.UserPoints {
		normalizedPoint := strings.ToLower(strings.TrimSpace(norm.NFKC.String(point)))
		if normalizedPoint == "" || !strings.Contains(normalizedUserContent, normalizedPoint) {
			return errors.New("public compare_answer point was not sourced from the user message")
		}
	}
	for _, contradiction := range comparison.Contradictions {
		contradiction = strings.TrimSpace(contradiction)
		if contradiction == "" {
			return errors.New("public compare_answer contains an empty contradiction")
		}
		if err := validateScenarioReply(contradiction, world, publicScenario, state); err != nil {
			return fmt.Errorf("public compare_answer contradiction rejected: %w", err)
		}
	}
	return nil
}

func scenarioTraceStatusAllowed(status string) bool {
	return status == "started" || status == "running" || status == "completed" || status == "failed"
}

func validateScenarioPublicObservation(observation *agentclient.PublicObservation, world *domain.HiddenWorld) error {
	if observation == nil {
		return errors.New("public observation result is invalid")
	}
	for _, configured := range world.Observations {
		if configured.Action != observation.Action {
			continue
		}
		allowedResults := []struct {
			result     string
			isNegative bool
		}{{result: configured.Result, isNegative: configured.IsNegative}}
		allowedResults = append(allowedResults, struct {
			result     string
			isNegative bool
		}{result: "本轮暂未形成新的可公开观察。"})
		if configured.UnmetPrerequisiteResult != "" {
			allowedResults = append(allowedResults, struct {
				result     string
				isNegative bool
			}{result: configured.UnmetPrerequisiteResult})
		} else {
			allowedResults = append(allowedResults, struct {
				result     string
				isNegative bool
			}{result: "当前还缺少足够上下文，暂时无法得到这项观察。"})
		}
		for _, item := range allowedResults {
			if item.result == observation.Result && item.isNegative == observation.IsNegative {
				return nil
			}
		}
		return errors.New("public observation result does not match the configured observation")
	}
	return errors.New("public observation action is not configured")
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
	for _, token := range scenarioNumberPattern.FindAllString(text, -1) {
		if scenarioIsDistinctiveNumber(token) {
			tokens = append(tokens, token)
		}
	}
	tokens = append(tokens, scenarioChineseComponentPattern.FindAllString(text, -1)...)
	return tokens
}

// scenarioIsDistinctiveNumber 过滤掉裸的一两位整数。
//
// 实测固定题库 4 道题里有 3 道把 8 / 10 / 12 / 35 / 45 / 90 这类数字列进了禁词表，
// 它们几乎不携带识别信息，却会让导师连「10 分钟」都写不出来。带小数点或千分位的
// 数字以及三位以上整数仍然是禁词——那些才是真正指向隐藏内容的具体取值。
//
// 必须与 agent/src/hiddenworld/kernel/guard.py 的 _is_distinctive_number 保持一致：
// 这里更严会导致 Python Guard 放行的回复在 validateScenarioReply 被拒，整轮失败。
func scenarioIsDistinctiveNumber(token string) bool {
	if strings.ContainsAny(token, ".,") {
		return true
	}
	return len([]rune(token)) >= 3
}

func scenarioReplyContainsEntity(text, entity string) bool {
	entity = scenarioEntityKey(entity)
	if entity == "" {
		return false
	}
	text = strings.ToLower(norm.NFKC.String(text))
	if scenarioNumberEntityPattern.MatchString(entity) {
		pattern := regexp.MustCompile(`(?i)(^|[^0-9])` + regexp.QuoteMeta(entity) + `([^0-9]|$)`)
		return pattern.MatchString(text)
	}
	if scenarioHanPattern.MatchString(entity) {
		text = scenarioWhitespacePattern.ReplaceAllString(text, "")
		return strings.Contains(text, entity)
	}
	pattern := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(entity) + `([^A-Za-z0-9_]|$)`)
	return pattern.MatchString(text)
}

func scenarioEntityKey(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(norm.NFKC.String(value)))
	if scenarioHanPattern.MatchString(normalized) {
		return scenarioWhitespacePattern.ReplaceAllString(normalized, "")
	}
	return normalized
}

func scenarioPublicScenarioSources(publicScenario *domain.PublicScenario) []string {
	if publicScenario == nil {
		return nil
	}
	sources := []string{
		publicScenario.Title,
		publicScenario.Description,
		publicScenario.Environment,
		publicScenario.ArchitectureDiagram,
	}
	return append(sources, publicScenario.InitialSymptoms...)
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
		ConceptMastery:     cloneScenarioIntMap(state.ConceptMastery),
		SkillMastery:       cloneScenarioIntMap(state.SkillMastery),
		ExplanationPreferences: agentclient.ExplanationPreferences{
			Detail:     state.ExplanationPreferences.Detail,
			Analogy:    state.ExplanationPreferences.Analogy,
			Directness: state.ExplanationPreferences.Directness,
		},
		HintLevel: state.HintLevel,
		LastHint:  state.LastHint,
		RepairStatus: state.RepairStatus,
	}
}

func cloneScenarioIntMap(values map[string]int) map[string]int {
	result := make(map[string]int, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
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

func scenarioRecentTranscript(messages []domain.ScenarioMessage, completeTurns int) []agentclient.Turn {
	if completeTurns <= 0 || len(messages) == 0 {
		return []agentclient.Turn{}
	}
	if len(messages) > completeTurns {
		messages = messages[len(messages)-completeTurns:]
	}
	return scenarioTranscript(messages)
}

// scenarioInitialRunEvents 是每轮的第一条正式事件（V2 turn_started）。
// 旧 v1 的 user_message 事件不再下发：用户正文由消息记录承载，前端从
// message.user_content / activeRun.userContent 渲染。
func scenarioInitialRunEvents(requestID string, stateRevision int) []domain.ScenarioRunEvent {
	return []domain.ScenarioRunEvent{scenarioTurnStartedEvent(requestID, stateRevision, 1)}
}

// scenarioActionDeclared 校验结构化动作是否在题目声明的观察目录内。
func scenarioActionDeclared(world *domain.HiddenWorld, actionID string) bool {
	if world == nil || actionID == "" {
		return false
	}
	for _, observation := range world.Observations {
		if observation.Action == actionID {
			return true
		}
	}
	for _, tool := range world.VirtualTools {
		if tool.ObservationAction == actionID {
			return true
		}
	}
	return false
}

// scenarioActionDisplayTitle 返回结构化动作的展示标题，与 QuickActions
// 按钮文案同源（查看 + 题目声明的工具目标）。
func scenarioActionDisplayTitle(world *domain.HiddenWorld, actionID string) string {
	if target := scenarioActionTarget(world, actionID); target != "" {
		return "查看" + target
	}
	return "发起了一次快捷检查"
}

func scenarioActionTarget(world *domain.HiddenWorld, actionID string) string {
	if world == nil {
		return ""
	}
	for _, tool := range world.VirtualTools {
		if tool.ObservationAction == actionID {
			return strings.TrimSpace(tool.Target)
		}
	}
	return ""
}

// V2 正式事件构造器：每种 kind 只填对应 payload 子对象，
// 判别联合的正确性由构造保证，不提供 kitchen-sink 填法。

func scenarioTurnStartedEvent(requestID string, revision, sequence int) domain.ScenarioRunEvent {
	return domain.ScenarioRunEvent{
		RequestID:     requestID,
		Sequence:      sequence,
		SchemaVersion: domain.ScenarioRunEventSchemaV2,
		StateRevision: revision,
		Kind:          "turn_started",
		Payload:       &domain.ScenarioRunEventPayload{TurnID: requestID},
	}
}

func scenarioAssistantDeltaEvent(requestID string, revision, sequence int, phase, delta string) domain.ScenarioRunEvent {
	return domain.ScenarioRunEvent{
		RequestID:     requestID,
		Sequence:      sequence,
		SchemaVersion: domain.ScenarioRunEventSchemaV2,
		StateRevision: revision,
		Kind:          "assistant_delta",
		Payload: &domain.ScenarioRunEventPayload{
			Phase:              phase,
			MarkdownReadyDelta: delta,
		},
	}
}

func scenarioTaskUpsertedEvent(requestID string, revision, sequence int, task domain.ScenarioTaskPayload) domain.ScenarioRunEvent {
	return domain.ScenarioRunEvent{
		RequestID:     requestID,
		Sequence:      sequence,
		SchemaVersion: domain.ScenarioRunEventSchemaV2,
		StateRevision: revision,
		Kind:          "task_upserted",
		Payload:       &domain.ScenarioRunEventPayload{Task: &task},
	}
}

func scenarioToolResultEvent(requestID string, revision, sequence int, payload domain.ScenarioToolResultPayload) domain.ScenarioRunEvent {
	return domain.ScenarioRunEvent{
		RequestID:     requestID,
		Sequence:      sequence,
		SchemaVersion: domain.ScenarioRunEventSchemaV2,
		StateRevision: revision,
		Kind:          "tool_result",
		Payload:       &domain.ScenarioRunEventPayload{ToolResult: &payload},
	}
}

func scenarioTurnCompletedEvent(requestID string, revision, sequence int, nextActions []domain.ScenarioAllowedAction) domain.ScenarioRunEvent {
	return domain.ScenarioRunEvent{
		RequestID:     requestID,
		Sequence:      sequence,
		SchemaVersion: domain.ScenarioRunEventSchemaV2,
		StateRevision: revision,
		Kind:          "turn_completed",
		Payload:       &domain.ScenarioRunEventPayload{NextActions: nextActions},
	}
}

func scenarioTurnFailedEvent(requestID string, revision, sequence int, errorCode, message string, retryable bool) domain.ScenarioRunEvent {
	return domain.ScenarioRunEvent{
		RequestID:     requestID,
		Sequence:      sequence,
		SchemaVersion: domain.ScenarioRunEventSchemaV2,
		StateRevision: revision,
		Kind:          "turn_failed",
		ErrorCode:     errorCode,
		Summary:       message,
		Payload:       &domain.ScenarioRunEventPayload{ErrorCode: errorCode, Retryable: retryable},
	}
}

// projectScenarioTraceEvents 把一条 Python v1 公开 trace 投影为零或多条 V2 正式事件。
// 实时 SSE 与落库重建共用此函数：同输入必同输出，public sequence 由 Go 独占分配，
// 断线重连或重放时不会重新编号。guard_passed / mentor_buffered / response_summary
// 等内部阶段事件在此静默丢弃——「不下发」是 V2 的正式决策，与 Python 是否继续
// 发送这些旧事件无关（Python 侧保留它们只为 v1 兼容窗口）。
// agent_tool_started / agent_tool_result 是 Python AgentLoop 的循环内实时旁路：
// 工具开始即时出现 running 任务，结束时联动任务终态并（在有观察负载时）投出
// 与 observation_result 同构的工具结果卡片。
func projectScenarioTraceEvents(
	requestID string,
	stateRevision int,
	world *domain.HiddenWorld,
	trace agentclient.PublicTraceEvent,
) ([]domain.ScenarioRunEvent, bool) {
	switch trace.Kind {
	case "reasoning_summary_delta", "reasoning_summary_completed":
		text := ""
		if trace.Reasoning != nil {
			text = trace.Reasoning.Text
		}
		if text == "" {
			text = trace.Summary
		}
		if strings.TrimSpace(text) == "" {
			return nil, false
		}
		// sequence 由调用方分配，这里返回 0 占位。
		return []domain.ScenarioRunEvent{
			scenarioAssistantDeltaEvent(requestID, stateRevision, 0, "understanding", text),
		}, true
	case "observation_result":
		if trace.Observation == nil {
			return nil, false
		}
		return []domain.ScenarioRunEvent{
			scenarioObservationToolResultEvent(requestID, stateRevision, world, trace.Observation),
		}, true
	case "agent_tool_started":
		if !scenarioPublicObservationToolName(world, trace.ToolName) {
			return nil, false
		}
		return []domain.ScenarioRunEvent{
			scenarioTaskUpsertedEvent(requestID, stateRevision, 0, domain.ScenarioTaskPayload{
				TaskID:  "obs:" + trace.ToolName,
				CallID:  trace.ToolName,
				Title:   "查看" + scenarioActionTarget(world, trace.ToolName),
				State:   domain.ScenarioTaskRunning,
				ToolRef: trace.ToolName,
			}),
		}, true
	case "agent_tool_result":
		if !scenarioPublicObservationToolName(world, trace.ToolName) {
			return nil, false
		}
		state := domain.ScenarioTaskCompleted
		if trace.Status == "failed" {
			state = domain.ScenarioTaskFailed
		}
		events := []domain.ScenarioRunEvent{
			scenarioTaskUpsertedEvent(requestID, stateRevision, 0, domain.ScenarioTaskPayload{
				TaskID:  "obs:" + trace.ToolName,
				CallID:  trace.ToolName,
				Title:   "查看" + scenarioActionTarget(world, trace.ToolName),
				State:   state,
				ToolRef: trace.ToolName,
			}),
		}
		if trace.Observation != nil {
			events = append(events, scenarioObservationToolResultEvent(requestID, stateRevision, world, trace.Observation))
		}
		return events, true
	case "tool_started":
		return []domain.ScenarioRunEvent{
			scenarioTaskUpsertedEvent(requestID, stateRevision, 0, domain.ScenarioTaskPayload{
				TaskID:  "compare-answer",
				CallID:  "compare_answer",
				Title:   "对比答案与已公开证据",
				State:   domain.ScenarioTaskRunning,
				ToolRef: "compare_answer",
			}),
		}, true
	case "tool_result":
		if trace.Tool == nil {
			return nil, false
		}
		content := (*domain.ScenarioPublicContent)(nil)
		if trace.Tool.Result != nil {
			content = &domain.ScenarioPublicContent{
				ContentType:    "observation",
				MarkdownReady:  scenarioComparisonMarkdown(trace.Tool.Result),
				DisplayVariant: "tool_return",
				Meta:           &domain.ScenarioPublicContentMeta{ToolKind: "verification"},
			}
		}
		return []domain.ScenarioRunEvent{
			scenarioToolResultEvent(requestID, stateRevision, 0, domain.ScenarioToolResultPayload{
				CallID:       "compare_answer",
				ToolID:       "compare_answer",
				ToolKind:     "verification",
				ResultStatus: "succeeded",
				DurationMS:   trace.Tool.DurationMS,
				Content:      content,
			}),
		}, true
	case "tool_completed":
		return []domain.ScenarioRunEvent{
			scenarioTaskUpsertedEvent(requestID, stateRevision, 0, domain.ScenarioTaskPayload{
				TaskID:  "compare-answer",
				CallID:  "compare_answer",
				Title:   "对比答案与已公开证据",
				State:   domain.ScenarioTaskCompleted,
				ToolRef: "compare_answer",
			}),
		}, true
	default:
		return nil, false
	}
}

// scenarioObservationToolResultEvent 把一条公开观察投影成 V2 工具结果卡片；
// observation_result 与循环内 agent_tool_result 的观察负载共用同一形状。
func scenarioObservationToolResultEvent(
	requestID string,
	stateRevision int,
	world *domain.HiddenWorld,
	observation *agentclient.PublicObservation,
) domain.ScenarioRunEvent {
	toolKind := scenarioToolKindForAction(world, observation.Action)
	return scenarioToolResultEvent(requestID, stateRevision, 0, domain.ScenarioToolResultPayload{
		CallID:       "obs:" + observation.Action,
		ToolID:       observation.Action,
		ToolKind:     toolKind,
		ResultStatus: "succeeded",
		Content: &domain.ScenarioPublicContent{
			ContentType:    "observation",
			MarkdownReady:  scenarioPublicObservationMarkdown(observation.Result),
			DisplayVariant: scenarioObservationDisplayVariant(toolKind),
			Meta: &domain.ScenarioPublicContentMeta{
				ToolKind:    toolKind,
				IsNegative:  observation.IsNegative,
				SourceKind:  "teaching_simulation",
				SourceLabel: "教学模拟",
				Title:       scenarioActionTarget(world, observation.Action),
			},
		},
	})
}

// scenarioPublicObservationToolName 只接受题目声明的公开观察动作：循环旁路
// 事件不得为 compare_answer 或未声明动作伪造任务。
func scenarioPublicObservationToolName(world *domain.HiddenWorld, action string) bool {
	if world == nil || strings.TrimSpace(action) == "" {
		return false
	}
	if strings.Contains(strings.ToLower(action), "compare_answer") {
		return false
	}
	for _, observation := range world.Observations {
		if observation.Action == action {
			return true
		}
	}
	for _, tool := range world.VirtualTools {
		if tool.ObservationAction == action {
			return true
		}
	}
	return false
}

// scenarioToolKindForAction 与 agent runtime.py 的 _tool_kind_for_action 同构：
// 优先取题目声明的 virtual_tools.kind，退化到动作前缀（inspect:logs.x → logs）。
func scenarioToolKindForAction(world *domain.HiddenWorld, action string) string {
	if world != nil {
		for _, tool := range world.VirtualTools {
			if tool.ObservationAction == action {
				return tool.Kind
			}
		}
	}
	_, remainder, _ := strings.Cut(action, ":")
	kind, _, _ := strings.Cut(remainder, ".")
	if kind != "" {
		return kind
	}
	return "observation"
}

func scenarioObservationDisplayVariant(toolKind string) string {
	switch toolKind {
	case "logs":
		return "log"
	default:
		return "tool_return"
	}
}

// scenarioPublicObservationMarkdown 只做空白规整。教学模拟来源由结构化
// content meta 明确展示，不能通过删除“模拟”字样伪装成真实生产数据。
func scenarioPublicObservationMarkdown(result string) string {
	return strings.TrimSpace(result)
}

func scenarioComparisonMarkdown(comparison *agentclient.PublicAnswerComparison) string {
	parts := []string{
		"结论：" + scenarioConclusionStatusLabel(comparison.ConclusionStatus),
		"证据：" + scenarioEvidenceStatusLabel(comparison.EvidenceStatus),
		"因果链：" + scenarioCausalStatusLabel(comparison.CausalStatus),
	}
	if len(comparison.MissingDimensions) > 0 {
		labels := make([]string, 0, len(comparison.MissingDimensions))
		for _, dimension := range comparison.MissingDimensions {
			labels = append(labels, scenarioComparisonDimensionLabel(dimension))
		}
		parts = append(parts, "仍需补齐："+strings.Join(labels, "、"))
	}
	if len(comparison.Contradictions) > 0 {
		parts = append(parts, "表述冲突："+strings.Join(comparison.Contradictions, "；"))
	}
	if len(comparison.UserPoints) > 0 {
		parts = append(parts, "你的要点："+strings.Join(comparison.UserPoints, "；"))
	}
	return strings.Join(parts, "；")
}

func scenarioConclusionStatusLabel(status string) string {
	switch status {
	case "none":
		return "尚未形成明确判断"
	case "partial":
		return "已形成部分判断"
	case "supported":
		return "已有公开证据支撑"
	case "contradictory":
		return "与已公开事实存在冲突"
	default:
		return status
	}
}

func scenarioEvidenceStatusLabel(status string) string {
	switch status {
	case "none":
		return "尚未引用有效观察"
	case "insufficient":
		return "现有观察不足"
	case "partial":
		return "覆盖了部分证据"
	case "sufficient":
		return "证据链覆盖充分"
	default:
		return status
	}
}

func scenarioCausalStatusLabel(status string) string {
	switch status {
	case "missing":
		return "尚未说明因果关系"
	case "partial":
		return "因果关系仍有断点"
	case "sufficient":
		return "因果链已经连贯"
	default:
		return status
	}
}

func scenarioComparisonDimensionLabel(dimension string) string {
	switch dimension {
	case "conclusion":
		return "明确结论"
	case "evidence":
		return "直接证据"
	case "causal_link":
		return "因果连接"
	case "consistency":
		return "表述一致性"
	default:
		return dimension
	}
}

// scenarioAllowedActions 从题目声明的虚拟工具目录（动态 ActionCatalog 实例）
// 生成 turn_completed 下一步动作。只做当前状态过滤：全部关联证据都已收集的
// 工具不再推荐；按钮只表达抽象检查方向，不携带答案关键词。
func scenarioAllowedActions(
	world *domain.HiddenWorld,
	state domain.ScenarioLearnerState,
	catalogVersion string,
	allowedActionIDs ...string,
) []domain.ScenarioAllowedAction {
	if world == nil {
		return nil
	}
	if len(allowedActionIDs) == 0 {
		return nil
	}
	taken := stringSet(state.ActionsTaken)
	tools := make(map[string]domain.VirtualTool, len(world.VirtualTools))
	for _, tool := range world.VirtualTools {
		tools[tool.ObservationAction] = tool
	}
	actions := make([]domain.ScenarioAllowedAction, 0, 3)
	seen := map[string]bool{}
	for _, actionID := range allowedActionIDs {
		tool, ok := tools[actionID]
		if !ok || seen[actionID] || taken[actionID] || !scenarioActionPrerequisitesMet(world, state, tool) {
			continue
		}
		seen[actionID] = true
		actions = append(actions, domain.ScenarioAllowedAction{
			ActionID:       actionID,
			CatalogVersion: catalogVersion,
			ToolKind:       tool.Kind,
			Title:          tool.Target,
		})
		if len(actions) == 3 {
			break
		}
	}
	return actions
}

func scenarioActionPrerequisitesMet(world *domain.HiddenWorld, state domain.ScenarioLearnerState, tool domain.VirtualTool) bool {
	if len(tool.EvidenceIDs) == 0 {
		return true
	}
	collected := stringSet(state.CollectedEvidence)
	for _, evidenceID := range tool.EvidenceIDs {
		if collected[evidenceID] {
			continue
		}
		for _, node := range world.EvidenceGraph {
			if node.EvidenceID == evidenceID && containsAll(state.CollectedEvidence, node.Prerequisites) {
				return true
			}
		}
	}
	return false
}

// buildScenarioRunEvents 产出 V2 正式事件序列。
// streamedChunks 非空表示实时流已经按真实顺序下发过 reply 分片：
// tracesBeforeReply 是首个分片到达前已下发的 trace 投影事件数，
// 落库序列与实时序列逐位对齐，断线重连（after_sequence）回放不重编号。
func buildScenarioRunEvents(
	requestID string,
	result agentclient.TurnResult,
	reply string,
	stateRevision int,
	world *domain.HiddenWorld,
	previousState domain.ScenarioLearnerState,
	state domain.ScenarioLearnerState,
	catalogVersion string,
	streamedChunks []string,
	tracesBeforeReply int,
) []domain.ScenarioRunEvent {
	chunks := streamedChunks
	if len(chunks) == 0 {
		chunks = chunkText(reply, 20)
		tracesBeforeReply = len(result.PublicTrace) + 1
	}
	events := []domain.ScenarioRunEvent{scenarioTurnStartedEvent(requestID, stateRevision, 1)}
	for _, trace := range result.PublicTrace {
		projected, ok := projectScenarioTraceEvents(requestID, stateRevision, world, trace)
		if !ok {
			continue
		}
		events = append(events, projected...)
	}
	// 序号统一在组装时分配：trace 投影事件保持 Python 到达顺序，
	// reply 分片插入 tracesBeforeReply 分割点，与实时流出顺序逐位一致。
	return insertReplyChunksAndComplete(
		events,
		requestID,
		stateRevision,
		chunks,
		tracesBeforeReply,
		previousState,
		state,
		world,
		catalogVersion,
		result.TurnControl.AllowedActionIDs...,
	)
}

// insertReplyChunksAndComplete 把 reply 分片插入 trace 投影事件的正确位置并
// 追加 turn_completed。tracesBeforeReply 之前的投影事件已经排在分片前面，
// 之后的投影事件需要移动到分片后面，与实时流出顺序逐位一致。
func insertReplyChunksAndComplete(
	events []domain.ScenarioRunEvent,
	requestID string,
	stateRevision int,
	chunks []string,
	tracesBeforeReply int,
	previousState domain.ScenarioLearnerState,
	state domain.ScenarioLearnerState,
	world *domain.HiddenWorld,
	catalogVersion string,
	allowedActionIDs ...string,
) []domain.ScenarioRunEvent {
	var before, after []domain.ScenarioRunEvent
	// events[0] 是 turn_started，恒在分片之前。
	for _, event := range events[1:] {
		if len(before) < tracesBeforeReply {
			before = append(before, event)
		} else {
			after = append(after, event)
		}
	}
	out := make([]domain.ScenarioRunEvent, 0, 1+len(before)+len(chunks)+len(after)+1)
	out = append(out, events[0])
	out = append(out, before...)
	for _, chunk := range chunks {
		if chunk == "" {
			continue
		}
		out = append(out, scenarioAssistantDeltaEvent(requestID, stateRevision, 0, "replying", chunk))
	}
	out = append(out, after...)
	for _, clue := range scenarioReleasedClueEvents(requestID, stateRevision, previousState, state, world) {
		out = append(out, clue)
	}
	if hint := scenarioPublishedHintEvent(requestID, stateRevision, previousState, state); hint != nil {
		out = append(out, *hint)
	}
	out = append(out, scenarioTurnCompletedEvent(
		requestID,
		stateRevision,
		0,
		scenarioAllowedActions(world, state, catalogVersion, allowedActionIDs...),
	))
	for i := range out {
		out[i].Sequence = i + 1
	}
	return out
}

// scenarioReleasedClueEvents 将本轮新归约的证据投影为学生可见的常驻线索。
// 只处理 previousState 中尚未存在的证据，避免重复请求/历史回放重复显示。
// 内容来自已通过 Go 提议审批的 EvidenceNode；未批准的题目内部证据不会进入事件流。
func scenarioReleasedClueEvents(
	requestID string,
	stateRevision int,
	previousState domain.ScenarioLearnerState,
	state domain.ScenarioLearnerState,
	world *domain.HiddenWorld,
) []domain.ScenarioRunEvent {
	if world == nil {
		return nil
	}
	previous := stringSet(previousState.CollectedEvidence)
	events := make([]domain.ScenarioRunEvent, 0)
	for _, evidenceID := range state.CollectedEvidence {
		if previous[evidenceID] {
			continue
		}
		for _, node := range world.EvidenceGraph {
			if node.EvidenceID != evidenceID || strings.TrimSpace(node.Content) == "" {
				continue
			}
			if node.ClueImportance == "none" {
				break
			}
			title := strings.TrimSpace(node.PublicTitle)
			if title == "" {
				title = "调查线索"
			}
			content := domain.ScenarioPublicContent{
				ContentType:    "clue",
				MarkdownReady:  scenarioPublicObservationMarkdown(node.Content),
				DisplayVariant: "clue",
				Meta: &domain.ScenarioPublicContentMeta{
					ToolKind:    node.Category,
					SourceKind:  "teaching_simulation",
					SourceLabel: "教学模拟",
					Title:       title,
				},
			}
			events = append(events, domain.ScenarioRunEvent{
				RequestID:     requestID,
				SchemaVersion: domain.ScenarioRunEventSchemaV2,
				StateRevision: stateRevision,
				Kind:          "clue_published",
				Payload: &domain.ScenarioRunEventPayload{
					Clue: &domain.ScenarioCluePayload{
						ClueID:  scenarioOpaquePublicID("clue", evidenceID+"\x00"+node.Content),
						Content: content,
					},
				},
			})
			break
		}
	}
	return events
}

func scenarioPublishedHintEvent(
	requestID string,
	stateRevision int,
	previousState domain.ScenarioLearnerState,
	state domain.ScenarioLearnerState,
) *domain.ScenarioRunEvent {
	text := strings.TrimSpace(state.LastHint)
	if text == "" || (text == strings.TrimSpace(previousState.LastHint) && state.HintLevel == previousState.HintLevel) {
		return nil
	}
	level := state.HintLevel
	if level < 1 {
		level = 1
	}
	if level > 4 {
		level = 4
	}
	content := domain.ScenarioPublicContent{
		ContentType:    "hint",
		MarkdownReady:  text,
		DisplayVariant: "hint",
		Meta: &domain.ScenarioPublicContentMeta{
			SourceKind:  "teaching_guidance",
			SourceLabel: "教学提示",
			Title:       fmt.Sprintf("第 %d 级提示", level),
		},
	}
	return &domain.ScenarioRunEvent{
		RequestID:     requestID,
		SchemaVersion: domain.ScenarioRunEventSchemaV2,
		StateRevision: stateRevision,
		Kind:          "hint_published",
		Payload: &domain.ScenarioRunEventPayload{
			Hint: &domain.ScenarioHintPayload{
				HintID:  scenarioOpaquePublicID("hint", text),
				Level:   level,
				Content: content,
			},
		},
	}
}

func scenarioOpaquePublicID(prefix, source string) string {
	digest := sha256.Sum256([]byte(source))
	return prefix + "_" + hex.EncodeToString(digest[:6])
}

// scenarioFillCurrentFocus 只在 Agent 没有提交焦点时补齐公开调查维度。
// 焦点来自已经通过 Go 审批的证据类别，不接受模型自造字符串，也不把
// 下一步动作或排除范围写入学生状态。
func scenarioFillCurrentFocus(state domain.ScenarioLearnerState, world *domain.HiddenWorld) domain.ScenarioLearnerState {
	if strings.TrimSpace(state.CurrentFocus) != "" || world == nil {
		return state
	}
	for index := len(state.CollectedEvidence) - 1; index >= 0; index-- {
		evidenceID := state.CollectedEvidence[index]
		for _, node := range world.EvidenceGraph {
			if node.EvidenceID == evidenceID && validScenarioFocus(node.Category) {
				state.CurrentFocus = node.Category
				return state
			}
		}
	}
	return state
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
