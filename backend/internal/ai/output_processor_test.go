package ai

import (
	"context"
	"strings"
	"testing"

	"situational-teaching/backend/internal/domain"
)

func TestOutputProcessorExtractsMarkdownJSONAndMarksRepair(t *testing.T) {
	var target map[string]string
	result, err := NewOutputProcessor().Process(OutputProcessRequest{
		Task:       RouterTaskScenarioReply,
		Schema:     SchemaScenarioReply,
		OutputMode: OutputModeJSON,
		RawOutput:  "```json\n{\"reply\":\"建议继续查看慢查询日志。\"}\n```",
		Target:     &target,
	})
	if err != nil {
		t.Fatal(err)
	}
	if target["reply"] == "" {
		t.Fatalf("expected reply to be decoded, got %+v", target)
	}
	if result.Output.ParseStatus != "parsed" || !result.Output.RepairUsed {
		t.Fatalf("expected repaired parse result, got %+v", result.Output)
	}
	if result.Validation.Status != "passed" || result.Validation.Detail == "" {
		t.Fatalf("expected passed validation detail, got %+v", result.Validation)
	}
}

func TestOutputProcessorClassifiesInvalidAndMissingFieldJSON(t *testing.T) {
	_, err := NewOutputProcessor().Process(OutputProcessRequest{
		Task:       RouterTaskScenarioReply,
		Schema:     SchemaScenarioReply,
		OutputMode: OutputModeJSON,
		RawOutput:  "not json",
		Target:     &map[string]string{},
	})
	if err == nil || !strings.Contains(err.Error(), "json parse failed") {
		t.Fatalf("expected json parse failure, got %v", err)
	}

	_, err = NewOutputProcessor().Process(OutputProcessRequest{
		Task:       RouterTaskScenarioReply,
		Schema:     SchemaScenarioReply,
		OutputMode: OutputModeJSON,
		RawOutput:  `{"reply":""}`,
		Target:     &map[string]string{},
	})
	if err == nil || !strings.Contains(err.Error(), "schema validation failed") {
		t.Fatalf("expected schema validation failure, got %v", err)
	}
}

func TestOutputProcessorRunsDomainValidator(t *testing.T) {
	question := validScenarioQuestionSample()
	question.Content.ArchitectureDiagram = "not mermaid"
	_, err := NewOutputProcessor().Process(OutputProcessRequest{
		Task:        RouterTaskScenarioGenerate,
		Schema:      SchemaScenarioQuestion,
		OutputMode:  OutputModeJSON,
		DomainValue: question,
		Validate: func(value interface{}) error {
			return ValidateScenarioQuestion(value.(domain.ScenarioQuestion))
		},
	})
	if err == nil || !strings.Contains(err.Error(), "domain validation failed") {
		t.Fatalf("expected domain validation failure, got %v", err)
	}
	if strings.Contains(err.Error(), "sk-test-secret") {
		t.Fatalf("validation errors must be sanitized, got %v", err)
	}
}

func TestSafetyFilterSanitizesBlocksLeaksAndAllowsTeachingContent(t *testing.T) {
	filter := NewSafetyFilter()
	result := filter.Apply(SafetyFilterRequest{
		Task:   RouterTaskCommunityStructure,
		Text:   "数据库 10.0.0.1 password=abc api_key=sk-test-secret",
		Policy: SafetyPolicyDefault,
	})
	if !result.RewriteUsed || result.Status != "rewritten" || result.Blocked {
		t.Fatalf("expected sanitized rewrite without block, got %+v", result)
	}
	for _, leaked := range []string{"10.0.0.1", "abc", "sk-test-secret"} {
		if strings.Contains(result.SanitizedPreview, leaked) {
			t.Fatalf("sensitive preview leaked %q in %q", leaked, result.SanitizedPreview)
		}
	}

	leak := filter.Apply(SafetyFilterRequest{
		Task:           RouterTaskScenarioReply,
		Text:           "根因是数据库连接池耗尽导致排队",
		ForbiddenTerms: []string{"数据库连接池耗尽导致排队"},
		Policy:         SafetyPolicyDefault,
	})
	if !leak.Blocked || !leak.RewriteUsed || leak.Status != "blocked" {
		t.Fatalf("expected root cause leak to be blocked, got %+v", leak)
	}
	if strings.Contains(leak.SanitizedPreview, "数据库连接池耗尽导致排队") {
		t.Fatalf("blocked preview leaked root cause: %q", leak.SanitizedPreview)
	}

	normal := filter.Apply(SafetyFilterRequest{
		Task:   RouterTaskScenarioReply,
		Text:   "建议继续查看慢查询日志和连接池等待队列。",
		Policy: SafetyPolicyDefault,
	})
	if normal.Blocked || normal.RewriteUsed || normal.Status != "passed" {
		t.Fatalf("expected normal teaching content to pass, got %+v", normal)
	}
}

func TestSanitizeScenarioContentFieldsRemovesSecretsFromStructuredUGC(t *testing.T) {
	content := validScenarioQuestionSample().Content
	content.RootCause = "真实地址 10.0.0.1 password=abc"
	content.KeyEvidence[0] = "api_key=sk-test-secret"
	content.RevealStrategy.SurfaceClues[0].Content = "联系 test@example.com"

	sanitized := SanitizeScenarioContentFields(content)
	serialized := strings.Join([]string{
		sanitized.RootCause,
		strings.Join(sanitized.KeyEvidence, "\n"),
		sanitized.RevealStrategy.SurfaceClues[0].Content,
	}, "\n")
	for _, leaked := range []string{"10.0.0.1", "abc", "sk-test-secret", "test@example.com"} {
		if strings.Contains(serialized, leaked) {
			t.Fatalf("structured content leaked %q in %q", leaked, serialized)
		}
	}
}

func TestRouterTelemetryRecordsOutputAndSafetyFields(t *testing.T) {
	router := &Router{
		primary:       unsafeReplyProvider{},
		info:          ProviderInfo{Provider: "unsafe", Model: "unsafe-model"},
		streamEnabled: true,
		telemetry:     newRouterTelemetryStore(),
	}
	reply, meta, err := router.RewriteScenarioReply(context.Background(), ScenarioReplyRequest{
		AllowedContent: "可以提示继续观察连接池指标。",
		UserMessage:    "数据库连接池耗尽导致排队",
		IsAnswerLeak:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !meta.SafetyRewritten {
		t.Fatalf("expected safety rewrite meta, got %+v", meta)
	}
	if strings.Contains(reply, "数据库连接池耗尽导致排队") {
		t.Fatalf("reply leaked root cause: %q", reply)
	}
	decision := router.Info().Telemetry.RecentDecisions[0]
	if decision.Output.ParseStatus != "parsed" || decision.Output.RepairUsed {
		t.Fatalf("expected output telemetry, got %+v", decision.Output)
	}
	if decision.Safety.Status != "blocked" || !decision.Safety.Blocked || !decision.Safety.RewriteUsed {
		t.Fatalf("expected safety telemetry, got %+v", decision.Safety)
	}
}

func TestRouterValidationFailureDoesNotCountSuccess(t *testing.T) {
	router := &Router{
		primary:     invalidScenarioProvider{},
		fallback:    NewMockProvider(),
		info:        ProviderInfo{Provider: "invalid", Model: "invalid-model"},
		telemetry:   newRouterTelemetryStore(),
		health:      newProviderHealthStore(),
		rateLimiter: newProviderRateLimiter(8),
	}
	question, meta, err := router.GenerateScenario(context.Background(), ScenarioGenerationRequest{Domain: "database", Difficulty: "L2", ScenarioType: "troubleshooting"})
	if err == nil {
		t.Fatalf("expected strict failure for scenario generation validation error, got question=%+v meta=%+v", question, meta)
	}
	if meta.Provider == ProviderMock || meta.FallbackUsed {
		t.Fatalf("scenario generation validation failure must not fall back, question=%+v meta=%+v", question, meta)
	}
	info := router.Info()
	if info.Telemetry.SuccessfulCalls != 0 || info.Telemetry.FailedCalls != 1 || info.Telemetry.FallbackCalls != 0 || info.Telemetry.ValidationErrors != 1 {
		t.Fatalf("expected failed strict-validation telemetry, got %+v", info.Telemetry)
	}
	decision := info.Telemetry.RecentDecisions[0]
	if decision.Provider == ProviderMock || decision.Validation.Status != "failed" || decision.Status != "failed" {
		t.Fatalf("expected failed validation decision without fallback, got %+v", decision)
	}
}

func TestRouterValidationFailureWithoutFallbackRecordsFailure(t *testing.T) {
	router := &Router{
		primary:   invalidScenarioProvider{},
		info:      ProviderInfo{Provider: "invalid", Model: "invalid-model"},
		telemetry: newRouterTelemetryStore(),
	}
	_, _, err := router.GenerateScenario(context.Background(), ScenarioGenerationRequest{Domain: "database"})
	if err == nil {
		t.Fatal("expected validation error")
	}
	info := router.Info()
	if info.Telemetry.SuccessfulCalls != 0 || info.Telemetry.FailedCalls != 1 || info.Telemetry.ValidationErrors != 1 {
		t.Fatalf("expected failed validation telemetry, got %+v", info.Telemetry)
	}
}

func TestCommunityStructureRegeneratesMermaidAfterSpecSanitization(t *testing.T) {
	router := &Router{
		primary:   sensitiveDiagramProvider{},
		fallback:  NewMockProvider(),
		info:      ProviderInfo{Provider: "sensitive", Model: "sensitive-model"},
		telemetry: newRouterTelemetryStore(),
		health:    newProviderHealthStore(),
	}

	content, meta, err := router.StructureCommunityPost(context.Background(), CommunityStructureRequest{
		Title:  "社区案例",
		Domain: "database",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !meta.Validated {
		t.Fatalf("expected validated community structure, got %+v", meta)
	}
	serialized := strings.Join([]string{
		content.ArchitectureDiagram,
		content.ArchitectureDiagramSpec.Nodes[0].Label,
	}, "\n")
	for _, leaked := range []string{"sk-test-secret", "10.0.0.1"} {
		if strings.Contains(serialized, leaked) {
			t.Fatalf("sanitized structured diagram leaked %q in %q", leaked, serialized)
		}
	}
	if !strings.Contains(content.ArchitectureDiagram, "已脱敏") && !strings.Contains(content.ArchitectureDiagram, "【密钥】") {
		t.Fatalf("expected mermaid to be regenerated from sanitized spec, got %q", content.ArchitectureDiagram)
	}
}

func TestScenarioReplyStreamEmitsOnlyProcessedReply(t *testing.T) {
	router := &Router{
		primary:       unsafeReplyProvider{},
		info:          ProviderInfo{Provider: "unsafe", Model: "unsafe-model"},
		streamEnabled: true,
		telemetry:     newRouterTelemetryStore(),
	}
	var streamed strings.Builder
	reply, meta, err := router.RewriteScenarioReplyStream(context.Background(), ScenarioReplyRequest{
		UserMessage:    "数据库连接池耗尽导致排队",
		IsAnswerLeak:   true,
		AllowedContent: "可以提示继续观察连接池指标。",
	}, func(chunk string) {
		streamed.WriteString(chunk)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !meta.SafetyRewritten {
		t.Fatalf("expected safety rewrite meta, got %+v", meta)
	}
	for _, output := range []string{reply, streamed.String()} {
		if strings.Contains(output, "数据库连接池耗尽导致排队") {
			t.Fatalf("stream leaked unsafe reply: %q", output)
		}
	}
}

type unsafeReplyProvider struct{}

func (unsafeReplyProvider) Info() ProviderInfo {
	return ProviderInfo{Provider: "unsafe", Model: "unsafe-model"}
}

func (unsafeReplyProvider) GenerateScenario(context.Context, ScenarioGenerationRequest) (domain.ScenarioQuestion, error) {
	return domain.ScenarioQuestion{}, nil
}

func (unsafeReplyProvider) StructureCommunityPost(context.Context, CommunityStructureRequest) (domain.ScenarioContent, error) {
	return domain.ScenarioContent{}, nil
}

func (unsafeReplyProvider) StructureCommunityPostStream(context.Context, CommunityStructureRequest, func(string)) (domain.ScenarioContent, error) {
	return domain.ScenarioContent{}, nil
}

func (unsafeReplyProvider) RewriteScenarioReply(context.Context, ScenarioReplyRequest) (string, error) {
	return "根因是数据库连接池耗尽导致排队", nil
}

func (p unsafeReplyProvider) RewriteScenarioReplyStream(ctx context.Context, req ScenarioReplyRequest, onDelta func(string)) (string, error) {
	return p.RewriteScenarioReply(ctx, req)
}

func (unsafeReplyProvider) GenerateInterviewFeedback(context.Context, InterviewFeedbackRequest) (InterviewFeedback, error) {
	return InterviewFeedback{}, nil
}

func (unsafeReplyProvider) GenerateInterviewFeedbackStream(context.Context, InterviewFeedbackRequest, func(string)) (InterviewFeedback, error) {
	return InterviewFeedback{}, nil
}

func (unsafeReplyProvider) CheckSensitiveContent(context.Context, SensitiveCheckRequest) (domain.SensitiveCheckResult, error) {
	return domain.SensitiveCheckResult{}, nil
}

type invalidScenarioProvider struct {
	unsafeReplyProvider
}

func (invalidScenarioProvider) Info() ProviderInfo {
	return ProviderInfo{Provider: "invalid", Model: "invalid-model"}
}

func (invalidScenarioProvider) GenerateScenario(context.Context, ScenarioGenerationRequest) (domain.ScenarioQuestion, error) {
	question := validScenarioQuestionSample()
	question.Difficulty = "L9"
	return question, nil
}

type sensitiveDiagramProvider struct {
	unsafeReplyProvider
}

func (sensitiveDiagramProvider) Info() ProviderInfo {
	return ProviderInfo{Provider: "sensitive", Model: "sensitive-model"}
}

func (sensitiveDiagramProvider) StructureCommunityPost(context.Context, CommunityStructureRequest) (domain.ScenarioContent, error) {
	content := validScenarioQuestionSample().Content
	content.ArchitectureDiagramSpec = &domain.ScenarioDiagramSpec{
		Direction: "TD",
		Nodes: []domain.ScenarioDiagramNode{
			{ID: "A", Label: "API key sk-test-secret"},
			{ID: "B", Label: "DB 10.0.0.1"},
		},
		Edges: []domain.ScenarioDiagramEdge{{From: "A", To: "B", Label: "password=secret"}},
	}
	content.ArchitectureDiagram = `graph TD
A["API key sk-test-secret"]
B["DB 10.0.0.1"]
A -->|"password=secret"| B`
	return content, nil
}
