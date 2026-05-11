package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/domain"
)

func TestListJSONSchemasIncludesAllStructuredAITasks(t *testing.T) {
	schemas := ListJSONSchemas()
	if len(schemas) != 5 {
		t.Fatalf("expected 5 schemas, got %d: %+v", len(schemas), schemas)
	}
	seen := map[string]JSONSchemaInfo{}
	for _, schema := range schemas {
		seen[schema.SchemaName] = schema
		if schema.Name == "" || schema.Target == "" || schema.Version == "" || schema.Task == "" || schema.Description == "" {
			t.Fatalf("schema metadata is incomplete: %+v", schema)
		}
		if schema.Status != "ok" {
			t.Fatalf("schema should be ok: %+v", schema)
		}
	}
	for _, name := range []string{SchemaScenarioQuestion, SchemaScenarioContentPreview, SchemaInterviewFeedback, SchemaScenarioReply, SchemaSensitiveCheck} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("missing schema %s in %+v", name, schemas)
		}
	}
}

func TestValidateJSONSchemasWithValidSamples(t *testing.T) {
	question := validScenarioQuestionSample()
	if err := ValidateDomainJSONSchema(SchemaScenarioQuestion, question); err != nil {
		t.Fatalf("scenario question schema should pass: %v", err)
	}
	if err := ValidateDomainJSONSchema(SchemaScenarioContentPreview, question.Content); err != nil {
		t.Fatalf("scenario content preview schema should pass: %v", err)
	}
	feedback := InterviewFeedback{
		Highlights:       []string{"定位路径清晰"},
		Deficiencies:     []string{"回滚验证还可以更具体"},
		FollowUpQuestion: "",
		FinalReport:      "整体达到岗位要求。",
	}
	if err := ValidateDomainJSONSchema(SchemaInterviewFeedback, feedback); err != nil {
		t.Fatalf("interview feedback schema should pass: %v", err)
	}
	if err := ValidateDomainJSONSchema(SchemaScenarioReply, map[string]string{"reply": "建议继续查看慢查询日志。"}); err != nil {
		t.Fatalf("scenario reply schema should pass: %v", err)
	}
}

func TestValidateJSONSchemasRejectInvalidSamples(t *testing.T) {
	question := validScenarioQuestionSample()
	question.Content.RootCause = ""
	if err := ValidateDomainJSONSchema(SchemaScenarioQuestion, question); err == nil {
		t.Fatal("expected missing root cause to fail")
	}

	preview := validScenarioQuestionSample().Content
	preview.KeyEvidence = nil
	if err := ValidateDomainJSONSchema(SchemaScenarioContentPreview, preview); err == nil {
		t.Fatal("expected missing key evidence to fail")
	}

	feedback := InterviewFeedback{Highlights: []string{"ok"}, FollowUpQuestion: "", FinalReport: ""}
	if err := ValidateDomainJSONSchema(SchemaInterviewFeedback, feedback); err == nil {
		t.Fatal("expected missing deficiencies to fail")
	}

	if err := ValidateJSONSchema(SchemaScenarioReply, `{"reply":""}`); err == nil {
		t.Fatal("expected empty reply to fail")
	}
	if err := ValidateJSONSchema(SchemaSensitiveCheck, `{"status":"risk","sanitized":true,"summary":"bad","findings":[{"type":"company","field":"raw_content","excerpt":"ACME","severity":"medium","suggestion":"mask","confidence":2}]}`); err == nil {
		t.Fatal("expected invalid sensitive confidence to fail")
	}
}

func TestScenarioQuestionSchemaRejectsExtraFieldsAndInvalidDiagramSpec(t *testing.T) {
	question := validScenarioQuestionSample()
	question.Content.ArchitectureDiagramSpec = &domain.ScenarioDiagramSpec{
		Direction: "SIDEWAYS",
		Nodes: []domain.ScenarioDiagramNode{
			{ID: "A", Label: "API"},
			{ID: "B", Label: "DB"},
		},
		Edges: []domain.ScenarioDiagramEdge{{From: "A", To: "B"}},
	}
	if err := ValidateDomainJSONSchema(SchemaScenarioQuestion, question); err == nil {
		t.Fatal("expected invalid diagram direction to fail")
	}

	raw := `{"title":"题目","description":"描述","domain":"database","difficulty":"L2","scenario_type":"troubleshooting","tags":["数据库"],"unexpected":"extra","content":{"root_cause":"根因","root_cause_keywords":["a","b"],"key_evidence":["证据"],"standard_procedure":["步骤一","步骤二"],"architecture_diagram":"","architecture_diagram_spec":{"direction":"TD","nodes":[{"id":"A","label":"API"},{"id":"B","label":"DB"}],"edges":[{"from":"A","to":"B"}]},"reference_links":[],"reveal_strategy":{"surface_clues":[{"clue_id":"c1","trigger_keywords":["a"],"content":"线索","is_distractor":false}],"deep_clues":[{"clue_id":"c2","trigger_keywords":["b"],"content":"深层线索","is_distractor":false}],"distractors":[]}}}`
	if err := ValidateJSONSchema(SchemaScenarioQuestion, raw); err == nil {
		t.Fatal("expected extra field to fail")
	}

	rawWithDiagramMeta := `{"title":"题目","description":"描述","domain":"database","difficulty":"L2","scenario_type":"troubleshooting","tags":["数据库"],"content":{"root_cause":"根因","root_cause_keywords":["a","b"],"key_evidence":["证据"],"standard_procedure":["步骤一","步骤二"],"architecture_diagram":"","architecture_diagram_spec":{"direction":"TD","nodes":[{"id":"A","label":"API"},{"id":"B","label":"DB"}],"edges":[{"from":"A","to":"B"}]},"diagram_status":"generated","diagram_warnings":["normalized"],"reference_links":[],"reveal_strategy":{"surface_clues":[{"clue_id":"c1","trigger_keywords":["a"],"content":"线索","is_distractor":false}],"deep_clues":[{"clue_id":"c2","trigger_keywords":["b"],"content":"深层线索","is_distractor":false}],"distractors":[]}}}`
	if err := ValidateJSONSchema(SchemaScenarioQuestion, rawWithDiagramMeta); err == nil {
		t.Fatal("expected ai schema to reject diagram_status and diagram_warnings")
	}
}

func TestOpenAICompatibleScenarioRejectsNonPureJSON(t *testing.T) {
	content := openAICompatibleScenarioJSON("非纯 JSON 题目")
	for _, raw := range []string{
		"说明：" + content,
		"```json\n" + content + "\n```",
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` + quoteJSON(raw) + `}}]}`))
		}))
		router := NewRouter(Config{
			Provider: ProviderOpenAICompatible,
			BaseURL:  server.URL,
			APIKey:   "test-key",
			Model:    "fake-model",
			Timeout:  time.Second,
		})
		_, meta, err := router.GenerateScenario(context.Background(), ScenarioGenerationRequest{Domain: "database", Difficulty: "L2", ScenarioType: "troubleshooting"})
		server.Close()
		if err == nil {
			t.Fatalf("expected non-pure JSON to fail under strict scenario_generate, raw=%q meta=%+v", raw, meta)
		}
		if meta.FallbackUsed || meta.Provider == ProviderMock {
			t.Fatalf("expected no mock fallback for non-pure scenario JSON, raw=%q meta=%+v", raw, meta)
		}
	}
}

func TestSensitiveCheckSchemaAndMockModel(t *testing.T) {
	result := domain.SensitiveCheckResult{
		Status:    "risk",
		Sanitized: true,
		Summary:   "发现真实公司名。",
		Findings: []domain.SensitiveFinding{{
			Type:       "company",
			Field:      "raw_content",
			Excerpt:    "ACME Corp",
			Severity:   "medium",
			Suggestion: "替换为业务系统代称。",
			Confidence: 0.86,
		}},
	}
	if err := ValidateDomainJSONSchema(SchemaSensitiveCheck, result); err != nil {
		t.Fatalf("sensitive check schema should pass: %v", err)
	}

	modelResult, err := NewMockProvider().CheckSensitiveContent(context.Background(), SensitiveCheckRequest{
		Field: "raw_content",
		Text:  "ACME Corp 的客户A通过 svc-order 内网拓扑访问异常。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if modelResult.Status != "risk" || modelResult.Source != "model" || len(modelResult.Findings) == 0 {
		t.Fatalf("expected model sensitive findings, got %+v", modelResult)
	}
}

func TestOpenAICompatibleStreamValidatesCompleteJSONBeforeUnmarshal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"reply\\\":\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	router := NewRouter(Config{
		Provider:         ProviderOpenAICompatible,
		BaseURL:          server.URL,
		APIKey:           "test-key",
		Model:            "fake-model",
		Timeout:          time.Second,
		StreamEnabled:    true,
		StreamConfigured: true,
	})
	reply, meta, err := router.RewriteScenarioReplyStream(context.Background(), ScenarioReplyRequest{
		QuestionTitle:  "连接池排查",
		UserMessage:    "查日志",
		ResponseType:   "redirect",
		AllowedContent: "建议继续查看慢查询日志。",
		HintLevel:      1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !meta.FallbackUsed || meta.Provider != ProviderMock {
		t.Fatalf("expected invalid streamed json to fall back to mock, meta=%+v", meta)
	}
	if strings.TrimSpace(reply) == "" || strings.Contains(reply, `{"reply"`) {
		t.Fatalf("expected clean fallback reply, got %q", reply)
	}
}

func validScenarioQuestionSample() domain.ScenarioQuestion {
	return domain.ScenarioQuestion{
		Title:        "数据库连接池耗尽导致接口超时",
		Description:  "订单接口在高峰期响应变慢，需要逐步定位。",
		Domain:       "database",
		Difficulty:   "L2",
		ScenarioType: "troubleshooting",
		Tags:         []string{"数据库", "连接池"},
		Content: domain.ScenarioContent{
			RootCause:           "数据库连接池耗尽导致请求排队。",
			RootCauseKeywords:   []string{"连接池", "排队"},
			KeyEvidence:         []string{"活跃连接接近上限", "等待连接耗时升高"},
			StandardProcedure:   []string{"查看接口耗时", "检查连接池指标", "确认等待队列"},
			ArchitectureDiagram: "graph TD\nA[API] --> B[Pool]\nB --> C[(DB)]",
			ArchitectureDiagramSpec: &domain.ScenarioDiagramSpec{
				Direction: "TD",
				Nodes: []domain.ScenarioDiagramNode{
					{ID: "API", Label: "API"},
					{ID: "Pool", Label: "Pool"},
					{ID: "DB", Label: "DB"},
				},
				Edges: []domain.ScenarioDiagramEdge{
					{From: "API", To: "Pool"},
					{From: "Pool", To: "DB"},
				},
			},
			ReferenceLinks: []string{"连接池监控"},
			RevealStrategy: domain.RevealStrategy{
				SurfaceClues: []domain.Clue{{
					ClueID:             "c1",
					TriggerKeywords:    []string{"连接", "池"},
					Content:            "连接池活跃连接接近上限。",
					RecommendedNextAsk: "继续询问等待队列。",
				}},
				DeepClues: []domain.Clue{{
					ClueID:             "c2",
					TriggerKeywords:    []string{"等待", "队列"},
					PrerequisiteClues:  []string{"c1"},
					Content:            "等待队列持续增长。",
					RecommendedNextAsk: "可以提交根因判断。",
				}},
				Distractors: []domain.Clue{{
					ClueID:          "d1",
					TriggerKeywords: []string{"网络"},
					Content:         "网络延迟正常。",
					IsDistractor:    true,
				}},
			},
		},
	}
}

func openAICompatibleScenarioJSON(title string) string {
	return `{"title":` + quoteJSON(title) + `,"description":"用于验证严格 JSON 输出的场景题。","domain":"database","difficulty":"L2","scenario_type":"troubleshooting","tags":["数据库","连接池"],"content":{"root_cause":"数据库连接池耗尽导致请求排队。","root_cause_keywords":["连接池","排队"],"key_evidence":["活跃连接接近上限"],"standard_procedure":["查看接口耗时","检查连接池指标"],"architecture_diagram":"","architecture_diagram_spec":{"direction":"TD","nodes":[{"id":"API","label":"API"},{"id":"Pool","label":"DB Pool"},{"id":"DB","label":"数据库(主库)"}],"edges":[{"from":"API","to":"Pool"},{"from":"Pool","to":"DB"}]},"reference_links":["连接池监控"],"reveal_strategy":{"surface_clues":[{"clue_id":"c1","trigger_keywords":["连接池"],"content":"活跃连接接近上限。","is_distractor":false}],"deep_clues":[{"clue_id":"c2","trigger_keywords":["排队"],"content":"等待队列持续增长。","is_distractor":false}],"distractors":[{"clue_id":"d1","trigger_keywords":["网络"],"content":"网络延迟正常。","is_distractor":true}]}}}`
}
