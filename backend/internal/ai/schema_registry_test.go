package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/domain"
)

func TestListJSONSchemasIncludesAllStructuredAITasks(t *testing.T) {
	schemas := ListJSONSchemas()
	if len(schemas) != 7 {
		t.Fatalf("expected 7 schemas, got %d: %+v", len(schemas), schemas)
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
	for _, name := range []string{SchemaScenarioQuestion, SchemaHiddenWorldQuestion, SchemaScenarioContentPreview, SchemaInterviewFeedback, SchemaInterviewOpening, SchemaScenarioReply, SchemaSensitiveCheck} {
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
	hiddenWorldQuestion := validHiddenWorldQuestionSample()
	if err := ValidateDomainJSONSchema(SchemaHiddenWorldQuestion, hiddenWorldQuestion); err != nil {
		t.Fatalf("hiddenworld question schema should pass: %v", err)
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
	hidden := validHiddenWorldQuestionSample()
	hidden.Content.HiddenWorld.RootCause.AcceptedHypotheses = []string{"H_OTHER"}
	if err := ValidateScenarioQuestion(hidden); err == nil {
		t.Fatal("expected H_OTHER as accepted hypothesis to fail domain validation")
	}
	hidden = validHiddenWorldQuestionSample()
	hidden.Content.HiddenWorld.EvidenceGraph = nil
	if err := ValidateScenarioQuestion(hidden); err == nil {
		t.Fatal("expected empty evidence graph to fail domain validation")
	}
	raw := openAICompatibleScenarioJSON("hiddenworld")
	if err := ValidateJSONSchema(SchemaHiddenWorldQuestion, strings.Replace(raw, `"model_version":"hiddenworld.v1"`, `"model_version":"hiddenworld.v0"`, 1)); err == nil {
		t.Fatal("expected invalid hiddenworld model_version to fail")
	}
	if err := ValidateJSONSchema(SchemaHiddenWorldQuestion, strings.Replace(raw, `"hidden_world":`, `"unknown":"x","hidden_world":`, 1)); err == nil {
		t.Fatal("expected unknown hiddenworld field to fail")
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
	question := validHiddenWorldQuestionSample()
	question.Title = title
	question.Content.PublicScenario.Title = title
	raw, err := json.Marshal(hiddenWorldQuestionSchemaProjection(question))
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func validHiddenWorldQuestionSample() domain.ScenarioQuestion {
	return domain.ScenarioQuestion{
		Title:        "数据库连接池排队导致接口变慢",
		Description:  "订单接口在高峰期响应时间突然上升，需要逐步定位。",
		Domain:       "database",
		Difficulty:   "L2",
		ScenarioType: "troubleshooting",
		Tags:         []string{"数据库", "连接池"},
		Content: domain.ScenarioContent{
			ModelVersion: domain.HiddenWorldContractVersion,
			PublicScenario: &domain.PublicScenario{
				Title:               "数据库连接池排队导致接口变慢",
				Description:         "订单接口在高峰期响应时间突然上升，需要逐步定位。",
				Environment:         "订单服务与数据库组成的模拟链路。",
				InitialSymptoms:     []string{"接口响应时间上升"},
				ArchitectureDiagram: "",
			},
			HiddenWorld: &domain.HiddenWorld{
				RootCause: domain.RootCause{
					ID:                     "RC_POOL",
					Category:               "resource",
					Component:              "数据库连接池",
					Description:            "连接池排队导致请求等待。",
					SufficientEvidenceSets: [][]string{{"E_SLOW", "E_POOL"}, {"E_RELEASE", "E_CONFIG"}},
					AcceptedHypotheses:     []string{"H_POOL"},
					SolutionRequirements:   []string{"确认连接池容量并修复排队原因"},
				},
				Hypotheses: []domain.Hypothesis{
					{HypothesisID: "H_POOL", Label: "连接池问题"},
					{HypothesisID: "H_CPU", Label: "数据库资源压力"},
					{HypothesisID: "H_NETWORK", Label: "网络抖动"},
					{HypothesisID: "H_OTHER", Label: "其他"},
				},
				EvidenceGraph: []domain.EvidenceNode{
					{EvidenceID: "E_SLOW", Content: "接口等待时间升高。", Category: "logs", Prerequisites: []string{}, ObtainedBy: []string{"inspect:logs.slow"}},
					{EvidenceID: "E_POOL", Content: "连接池活跃数接近上限。", Category: "resource", Prerequisites: []string{"E_SLOW"}, ObtainedBy: []string{"inspect:resource.pool"}},
					{EvidenceID: "E_RELEASE", Content: "异常前有连接配置变更。", Category: "change", Prerequisites: []string{}, ObtainedBy: []string{"inspect:change.release"}},
					{EvidenceID: "E_CONFIG", Content: "变更后的连接配置与预期不一致。", Category: "change", Prerequisites: []string{"E_RELEASE"}, ObtainedBy: []string{"inspect:change.config"}},
					{EvidenceID: "E_CPU", Content: "数据库 CPU 处于正常范围。", Category: "metrics", Prerequisites: []string{}, ObtainedBy: []string{"inspect:metrics.cpu"}},
					{EvidenceID: "E_NETWORK", Content: "应用到数据库的网络延迟稳定。", Category: "dependency", Prerequisites: []string{}, ObtainedBy: []string{"inspect:dependency.network"}},
				},
				Observations: []domain.Observation{
					{Action: "inspect:logs.slow", Result: "日志显示接口等待时间升高。", YieldsEvidence: []string{"E_SLOW"}, RulesOut: []string{}, UnmetPrerequisiteResult: ""},
					{Action: "inspect:resource.pool", Result: "连接池活跃数接近上限。", YieldsEvidence: []string{"E_POOL"}, RulesOut: []string{}, UnmetPrerequisiteResult: "还没有先定位到具体接口。"},
					{Action: "inspect:change.release", Result: "异常前有连接配置变更。", YieldsEvidence: []string{"E_RELEASE"}, RulesOut: []string{}, UnmetPrerequisiteResult: ""},
					{Action: "inspect:change.config", Result: "变更后的连接配置存在差异。", YieldsEvidence: []string{"E_CONFIG"}, RulesOut: []string{}, UnmetPrerequisiteResult: "还没有可对比的变更记录。"},
					{Action: "inspect:metrics.cpu", Result: "CPU 正常，没有资源饱和迹象。", IsNegative: true, YieldsEvidence: []string{"E_CPU"}, RulesOut: []string{"H_CPU"}, UnmetPrerequisiteResult: ""},
					{Action: "inspect:dependency.network", Result: "网络稳定，没有依赖抖动迹象。", IsNegative: true, YieldsEvidence: []string{"E_NETWORK"}, RulesOut: []string{"H_NETWORK"}, UnmetPrerequisiteResult: ""},
				},
				SolutionRubric: domain.SolutionRubric{
					RequiredActions:   []string{"确认连接池容量并修复排队原因"},
					VerificationSteps: []string{"观察接口 P95 延迟回落"},
					RollbackNotes:     []string{"保留原配置以便回滚"},
				},
				MisconceptionRules: []domain.MisconceptionRule{{MisconceptionID: "M_CPU", PatternHypotheses: []string{"H_CPU"}, WhyWrong: "CPU 指标正常，不能解释等待队列。"}},
			},
		},
	}
}
