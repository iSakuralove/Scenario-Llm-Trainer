package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"situational-teaching/backend/internal/domain"
)

type OpenAICompatibleProvider struct {
	baseURL     string
	apiKey      string
	model       string
	temperature float64
	topP        float64
	topK        int
	maxTokens   int
	stream      bool
	client      *http.Client
	name        string
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionRequest struct {
	Model          string                 `json:"model"`
	Messages       []chatMessage          `json:"messages"`
	Temperature    *float64               `json:"temperature,omitempty"`
	TopP           *float64               `json:"top_p,omitempty"`
	TopK           *int                   `json:"top_k,omitempty"`
	MaxTokens      *int                   `json:"max_tokens,omitempty"`
	ResponseFormat map[string]string      `json:"response_format,omitempty"`
	Stream         bool                   `json:"stream,omitempty"`
	Extra          map[string]interface{} `json:"-"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

type RawOutputError struct {
	Raw string
	Err error
}

func (e RawOutputError) Error() string {
	if e.Err == nil {
		return "llm output validation failed"
	}
	return e.Err.Error()
}

func (e RawOutputError) Unwrap() error {
	return e.Err
}

func RawOutputFromError(err error) string {
	var rawErr RawOutputError
	if errors.As(err, &rawErr) {
		return rawErr.Raw
	}
	var rawErrPtr *RawOutputError
	if errors.As(err, &rawErrPtr) && rawErrPtr != nil {
		return rawErrPtr.Raw
	}
	return ""
}

type chatCompletionStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func NewOpenAICompatibleProvider(cfg Config) *OpenAICompatibleProvider {
	name := cfg.Provider
	if strings.TrimSpace(name) == "" {
		name = ProviderOpenAICompatible
	}
	return &OpenAICompatibleProvider{
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		temperature: cfg.Temperature,
		topP:        cfg.TopP,
		topK:        cfg.TopK,
		maxTokens:   cfg.MaxTokens,
		stream:      cfg.StreamEnabled,
		client:      &http.Client{Timeout: cfg.Timeout},
		name:        name,
	}
}

func (p *OpenAICompatibleProvider) Info() ProviderInfo {
	return ProviderInfo{Provider: p.name, Model: p.model, BaseURL: p.baseURL}
}

func (p *OpenAICompatibleProvider) GenerateScenario(ctx context.Context, req ScenarioGenerationRequest) (domain.ScenarioQuestion, error) {
	var out domainQuestion
	prompt, err := renderPrompt("scenario_generate", map[string]interface{}{
		"Domain":       defaultString(req.Domain, "database"),
		"Difficulty":   defaultString(req.Difficulty, "L2"),
		"ScenarioType": defaultString(req.ScenarioType, "troubleshooting"),
		"Nonce":        defaultString(req.Nonce, "none"),
		"TagsText":     strings.Join(req.Tags, ","),
	})
	if err != nil {
		return domain.ScenarioQuestion{}, err
	}
	if err := p.completeJSON(ctx, prompt, SchemaScenarioQuestion, &out); err != nil {
		return domain.ScenarioQuestion{}, err
	}
	question := PrepareScenarioQuestion(out.toDomain(req))
	return question, ValidateScenarioQuestion(question)
}

func (p *OpenAICompatibleProvider) StructureCommunityPost(ctx context.Context, req CommunityStructureRequest) (domain.ScenarioContent, error) {
	var out domainContent
	prompt, err := renderPrompt("community_structure", map[string]interface{}{
		"Title":      req.Title,
		"Domain":     req.Domain,
		"TagsText":   strings.Join(req.Tags, ","),
		"RawContent": req.RawContent,
	})
	if err != nil {
		return domain.ScenarioContent{}, err
	}
	if err := p.completeJSON(ctx, prompt, SchemaScenarioContentPreview, &out); err != nil {
		return domain.ScenarioContent{}, err
	}
	content := PrepareScenarioContent(out.toDomain(), domain.ScenarioQuestion{Title: req.Title, Domain: req.Domain})
	return content, ValidateScenarioContent(content, true)
}

func (p *OpenAICompatibleProvider) StructureCommunityPostStream(ctx context.Context, req CommunityStructureRequest, onDelta func(string)) (domain.ScenarioContent, error) {
	var out domainContent
	prompt, err := renderPrompt("community_structure", map[string]interface{}{
		"Title":      req.Title,
		"Domain":     req.Domain,
		"TagsText":   strings.Join(req.Tags, ","),
		"RawContent": req.RawContent,
	})
	if err != nil {
		return domain.ScenarioContent{}, err
	}
	if err := p.completeJSONStream(ctx, prompt, SchemaScenarioContentPreview, &out, onDelta); err != nil {
		return domain.ScenarioContent{}, err
	}
	content := PrepareScenarioContent(out.toDomain(), domain.ScenarioQuestion{Title: req.Title, Domain: req.Domain})
	return content, ValidateScenarioContent(content, true)
}

func (p *OpenAICompatibleProvider) RewriteScenarioReply(ctx context.Context, req ScenarioReplyRequest) (string, error) {
	var out struct {
		Reply string `json:"reply"`
	}
	prompt, err := renderScenarioReplyPrompt(req)
	if err != nil {
		return "", err
	}
	if err := p.completeJSON(ctx, prompt, SchemaScenarioReply, &out); err != nil {
		return "", err
	}
	if err := ValidateScenarioReply(out.Reply); err != nil {
		return "", err
	}
	return out.Reply, nil
}

func (p *OpenAICompatibleProvider) RewriteScenarioReplyStream(ctx context.Context, req ScenarioReplyRequest, onDelta func(string)) (string, error) {
	var out struct {
		Reply string `json:"reply"`
	}
	prompt, err := renderScenarioReplyPrompt(req)
	if err != nil {
		return "", err
	}
	if err := p.completeJSONStream(ctx, prompt, SchemaScenarioReply, &out, onDelta); err != nil {
		return "", err
	}
	if err := ValidateScenarioReply(out.Reply); err != nil {
		return "", err
	}
	return out.Reply, nil
}

func renderScenarioReplyPrompt(req ScenarioReplyRequest) (string, error) {
	return renderPrompt("scenario_reply", map[string]interface{}{
		"QuestionTitle":       req.QuestionTitle,
		"UserMessage":         req.UserMessage,
		"ResponseType":        req.ResponseType,
		"AllowedContent":      req.AllowedContent,
		"DiagnosticIntent":    req.DiagnosticIntent,
		"CoachingAction":      req.CoachingAction,
		"DiagnosticFocus":     req.DiagnosticFocus,
		"MissingEvidenceText": strings.Join(req.MissingEvidence, "、"),
		"RepeatedWithTurn":    req.RepeatedWithTurn,
		"ToneStyle":           req.ToneStyle,
		"HintLevel":           req.HintLevel,
		"ConversationSummary": req.ConversationSummary,
		"RecentMessagesText":  formatScenarioContext(req.RecentMessages),
	})
}

func (p *OpenAICompatibleProvider) GenerateInterviewFeedback(ctx context.Context, req InterviewFeedbackRequest) (InterviewFeedback, error) {
	var out InterviewFeedback
	prompt, err := interviewFeedbackPrompt(req)
	if err != nil {
		return InterviewFeedback{}, err
	}
	if err := p.completeJSON(ctx, prompt, SchemaInterviewFeedback, &out); err != nil {
		return InterviewFeedback{}, err
	}
	return out, ValidateInterviewFeedback(out, req.Evaluation.FollowUpTriggered, req.NeedReport)
}

func (p *OpenAICompatibleProvider) GenerateInterviewFeedbackStream(ctx context.Context, req InterviewFeedbackRequest, onDelta func(string)) (InterviewFeedback, error) {
	var out InterviewFeedback
	prompt, err := interviewFeedbackPrompt(req)
	if err != nil {
		return InterviewFeedback{}, err
	}
	if err := p.completeJSONStream(ctx, prompt, SchemaInterviewFeedback, &out, onDelta); err != nil {
		return InterviewFeedback{}, err
	}
	return out, ValidateInterviewFeedback(out, req.Evaluation.FollowUpTriggered, req.NeedReport)
}

func (p *OpenAICompatibleProvider) CheckSensitiveContent(ctx context.Context, req SensitiveCheckRequest) (domain.SensitiveCheckResult, error) {
	var out sensitiveCheckModelOutput
	prompt, err := renderPrompt("sensitive_check", map[string]interface{}{
		"Field": defaultString(req.Field, "content"),
		"Text":  req.Text,
	})
	if err != nil {
		return domain.SensitiveCheckResult{}, err
	}
	if err := p.completeJSON(ctx, prompt, SchemaSensitiveCheck, &out); err != nil {
		return domain.SensitiveCheckResult{}, err
	}
	result := out.toDomain(req.Field)
	return result, ValidateSensitiveCheck(result)
}

func interviewFeedbackPrompt(req InterviewFeedbackRequest) (string, error) {
	reportInstruction := "final_report 可以为空。"
	if req.NeedReport {
		reportInstruction = "必须生成 final_report，给出 1-2 句中文综合评价。"
	}
	followInstruction := "follow_up_question 可以为空。"
	if req.Evaluation.FollowUpTriggered {
		followInstruction = "必须生成 follow_up_question，只能围绕评分最低的维度追问。"
	}
	questionText := ""
	if req.Question != nil {
		questionText = req.Question.Description
	}
	return renderPrompt("interview_feedback", map[string]interface{}{
		"FollowInstruction": followInstruction,
		"ReportInstruction": reportInstruction,
		"QuestionText":      questionText,
		"Answer":            req.Answer,
		"TotalScore":        req.Evaluation.TotalScore,
		"DimensionScores":   req.Evaluation.DimensionScores,
	})
}

func (p *OpenAICompatibleProvider) completeJSON(ctx context.Context, prompt string, schemaName string, target interface{}) error {
	body := chatCompletionRequest{
		Model: p.model,
		Messages: []chatMessage{
			{Role: "system", Content: "你必须只返回一个合法 JSON 对象，不能包含 Markdown 代码块、解释文字或额外前后缀。"},
			{Role: "user", Content: prompt},
		},
		ResponseFormat: map[string]string{"type": "json_object"},
	}
	p.applySamplingParams(&body, false)
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	response, err := p.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("llm provider returned status %d", response.StatusCode)
	}
	var parsed chatCompletionResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return err
	}
	if parsed.Error != nil {
		return fmt.Errorf("llm provider error")
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return fmt.Errorf("empty llm response")
	}
	rawContent := parsed.Choices[0].Message.Content
	content, err := strictJSONObject(rawContent)
	if err != nil {
		return RawOutputError{Raw: rawContent, Err: err}
	}
	if err := ValidateJSONSchema(schemaName, content); err != nil {
		return RawOutputError{Raw: rawContent, Err: err}
	}
	if err := json.Unmarshal([]byte(content), target); err != nil {
		return err
	}
	return nil
}

func (p *OpenAICompatibleProvider) completeJSONStream(ctx context.Context, prompt string, schemaName string, target interface{}, onDelta func(string)) error {
	body := chatCompletionRequest{
		Model: p.model,
		Messages: []chatMessage{
			{Role: "system", Content: "Return only one valid JSON object. Do not include Markdown fences, explanations, prefixes, or suffixes."},
			{Role: "user", Content: prompt},
		},
		ResponseFormat: map[string]string{"type": "json_object"},
	}
	p.applySamplingParams(&body, true)
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	response, err := p.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("llm provider returned status %d", response.StatusCode)
	}
	var content strings.Builder
	if err := readChatCompletionStream(response.Body, func(chunk string) {
		content.WriteString(chunk)
		if onDelta != nil {
			onDelta(chunk)
		}
	}); err != nil {
		return err
	}
	rawContent := content.String()
	jsonContent, err := strictJSONObject(rawContent)
	if err != nil {
		return RawOutputError{Raw: rawContent, Err: err}
	}
	if err := ValidateJSONSchema(schemaName, jsonContent); err != nil {
		return RawOutputError{Raw: rawContent, Err: err}
	}
	if err := json.Unmarshal([]byte(jsonContent), target); err != nil {
		return err
	}
	return nil
}

func strictJSONObject(text string) (string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", fmt.Errorf("empty llm response")
	}
	extracted := extractJSONObject(trimmed)
	if extracted == "" {
		return "", fmt.Errorf("llm response is not json")
	}
	if extracted != trimmed {
		return "", fmt.Errorf("llm response must contain only one json object")
	}
	return extracted, nil
}

func (p *OpenAICompatibleProvider) applySamplingParams(body *chatCompletionRequest, forceStream bool) {
	if body == nil {
		return
	}
	capability := capabilityForProvider(ProviderInfo{Provider: p.name, Model: p.model}, p.stream)
	if capability.Temperature {
		body.Temperature = &p.temperature
	}
	if capability.TopP && p.topP > 0 {
		topP := p.topP
		body.TopP = &topP
	}
	if capability.TopK && p.topK > 0 {
		topK := p.topK
		body.TopK = &topK
	}
	if capability.MaxTokens > 0 && p.maxTokens > 0 {
		maxTokens := p.maxTokens
		body.MaxTokens = &maxTokens
	}
	body.Stream = forceStream
}

func readChatCompletionStream(body io.Reader, onDelta func(string)) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return nil
		}
		var parsed chatCompletionStreamChunk
		if err := json.Unmarshal([]byte(data), &parsed); err != nil {
			return err
		}
		if parsed.Error != nil {
			return fmt.Errorf("llm provider error")
		}
		for _, choice := range parsed.Choices {
			if choice.Delta.Content != "" && onDelta != nil {
				onDelta(choice.Delta.Content)
			}
		}
	}
	return scanner.Err()
}

func formatScenarioContext(messages []ScenarioContextMessage) string {
	if len(messages) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, message := range messages {
		fmt.Fprintf(&builder, "第 %d 轮 用户：%s\n第 %d 轮 助教：%s\n", message.TurnNumber, message.UserContent, message.TurnNumber, message.AssistantContent)
	}
	return strings.TrimSpace(builder.String())
}

type domainQuestion struct {
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	Domain       string        `json:"domain"`
	Difficulty   string        `json:"difficulty"`
	ScenarioType string        `json:"scenario_type"`
	Tags         []string      `json:"tags"`
	Content      domainContent `json:"content"`
}

func (q domainQuestion) toDomain(req ScenarioGenerationRequest) domain.ScenarioQuestion {
	tags := q.Tags
	if len(tags) == 0 {
		tags = req.Tags
	}
	return domain.ScenarioQuestion{
		Title:        q.Title,
		Description:  q.Description,
		Domain:       defaultString(q.Domain, defaultString(req.Domain, "database")),
		Difficulty:   defaultString(q.Difficulty, defaultString(req.Difficulty, "L2")),
		ScenarioType: defaultString(q.ScenarioType, defaultString(req.ScenarioType, "troubleshooting")),
		Tags:         tags,
		Content:      q.Content.toDomain(),
		Status:       "active",
		Source:       "llm_generated",
		CreatedBy:    req.UserID,
		Version:      1,
	}
}

type domainContent struct {
	RootCause               string                      `json:"root_cause"`
	RootCauseKeywords       []string                    `json:"root_cause_keywords"`
	KeyEvidence             []string                    `json:"key_evidence"`
	StandardProcedure       []string                    `json:"standard_procedure"`
	RevealStrategy          domainRevealStrategy        `json:"reveal_strategy"`
	ArchitectureDiagram     string                      `json:"architecture_diagram"`
	ArchitectureDiagramSpec *domain.ScenarioDiagramSpec `json:"architecture_diagram_spec"`
	ReferenceLinks          []string                    `json:"reference_links"`
}

func (c domainContent) toDomain() domain.ScenarioContent {
	return domain.ScenarioContent{
		RootCause:               c.RootCause,
		RootCauseKeywords:       c.RootCauseKeywords,
		KeyEvidence:             c.KeyEvidence,
		StandardProcedure:       c.StandardProcedure,
		RevealStrategy:          c.RevealStrategy.toDomain(),
		ArchitectureDiagram:     c.ArchitectureDiagram,
		ArchitectureDiagramSpec: c.ArchitectureDiagramSpec,
		ReferenceLinks:          c.ReferenceLinks,
	}
}

type domainRevealStrategy struct {
	SurfaceClues []domainClue `json:"surface_clues"`
	DeepClues    []domainClue `json:"deep_clues"`
	Distractors  []domainClue `json:"distractors"`
}

func (s domainRevealStrategy) toDomain() domain.RevealStrategy {
	return domain.RevealStrategy{
		SurfaceClues: convertClues(s.SurfaceClues),
		DeepClues:    convertClues(s.DeepClues),
		Distractors:  convertClues(s.Distractors),
	}
}

type domainClue struct {
	ClueID             string   `json:"clue_id"`
	TriggerKeywords    []string `json:"trigger_keywords"`
	PrerequisiteClues  []string `json:"prerequisite_clues"`
	Content            string   `json:"content"`
	IsDistractor       bool     `json:"is_distractor"`
	RecommendedNextAsk string   `json:"recommended_next_ask"`
}

type sensitiveCheckModelOutput struct {
	Status    string                  `json:"status"`
	Sanitized bool                    `json:"sanitized"`
	Summary   string                  `json:"summary"`
	Findings  []sensitiveFindingModel `json:"findings"`
}

type sensitiveFindingModel struct {
	Type       string  `json:"type"`
	Field      string  `json:"field"`
	Excerpt    string  `json:"excerpt"`
	Severity   string  `json:"severity"`
	Suggestion string  `json:"suggestion"`
	Confidence float64 `json:"confidence"`
}

func (out sensitiveCheckModelOutput) toDomain(defaultField string) domain.SensitiveCheckResult {
	findings := make([]domain.SensitiveFinding, 0, len(out.Findings))
	for _, item := range out.Findings {
		field := strings.TrimSpace(item.Field)
		if field == "" {
			field = defaultField
		}
		findings = append(findings, domain.SensitiveFinding{
			Type:            strings.TrimSpace(item.Type),
			Field:           field,
			Excerpt:         Sanitize(truncateForFinding(item.Excerpt)),
			RedactedExcerpt: Sanitize(truncateForFinding(item.Excerpt)),
			Severity:        normalizeSeverity(item.Severity),
			Suggestion:      strings.TrimSpace(item.Suggestion),
			Source:          "model",
			Confidence:      item.Confidence,
		})
	}
	status := normalizeSensitiveStatus(out.Status, findings)
	return domain.SensitiveCheckResult{
		Status:    status,
		Sanitized: out.Sanitized || len(findings) > 0,
		Findings:  findings,
		Source:    "model",
		RiskLevel: riskLevelFromFindings(findings),
		Blocked:   shouldBlockSensitiveFindings(findings),
		Summary:   strings.TrimSpace(out.Summary),
	}
}

func convertClues(items []domainClue) []domain.Clue {
	out := make([]domain.Clue, 0, len(items))
	for _, item := range items {
		out = append(out, domain.Clue{
			ClueID:             item.ClueID,
			TriggerKeywords:    item.TriggerKeywords,
			PrerequisiteClues:  item.PrerequisiteClues,
			Content:            item.Content,
			IsDistractor:       item.IsDistractor,
			RecommendedNextAsk: item.RecommendedNextAsk,
		})
	}
	return out
}
