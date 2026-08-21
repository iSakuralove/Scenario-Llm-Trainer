package ai

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"situational-teaching/backend/internal/domain"
)

type MockProvider struct{}

type mockScenarioVariant struct {
	Title        string
	Description  string
	RootCause    string
	Keywords     []string
	Evidence     []string
	Procedure    []string
	SurfaceClues []domain.Clue
	DeepClues    []domain.Clue
	Distractors  []domain.Clue
	Diagram      string
	DiagramSpec  domain.ScenarioDiagramSpec
	References   []string
}

var mockScenarioVariants = []mockScenarioVariant{
	{
		Title:       "配置发布后核心链路异常",
		Description: "一次配置发布后，核心接口错误率升高，但下游服务基础指标正常。请通过日志、指标和变更记录逐步定位。",
		RootCause:   "配置变更后缺少必要验证，导致核心链路出现异常。",
		Keywords:    []string{"配置变更", "验证", "核心链路"},
		Evidence:    []string{"异常开始时间与配置发布时间一致", "回滚配置后指标恢复", "下游服务本身无异常"},
		Procedure:   []string{"确认异常窗口", "聚合日志与指标", "比对最近变更", "验证依赖链路", "灰度回滚并观察"},
		SurfaceClues: []domain.Clue{
			{ClueID: "c1", TriggerKeywords: []string{"日志", "时间", "窗口"}, Content: "异常开始时间与一次配置发布高度重合。", RecommendedNextAsk: "继续询问变更内容。"},
			{ClueID: "c2", TriggerKeywords: []string{"指标", "监控", "依赖"}, Content: "下游依赖服务本身指标正常，异常主要集中在核心服务调用分支。", RecommendedNextAsk: "继续询问配置或回滚。"},
		},
		DeepClues:   []domain.Clue{{ClueID: "c3", TriggerKeywords: []string{"配置", "变更", "回滚"}, PrerequisiteClues: []string{"c1"}, Content: "灰度回滚配置后，错误率从 8% 降至 0.5%。", RecommendedNextAsk: "可以提交根因判断。"}},
		Distractors: []domain.Clue{{ClueID: "d1", TriggerKeywords: []string{"网络", "CPU"}, Content: "网络和 CPU 指标都在正常范围内。", IsDistractor: true}},
		Diagram:     "graph TD\nA[Client] --> B[API]\nB --> C[Core Service]\nC --> D[Dependency]\nC --> E[Config Center]",
		DiagramSpec: domain.ScenarioDiagramSpec{
			Direction: "TD",
			Nodes: []domain.ScenarioDiagramNode{
				{ID: "Client", Label: "Client"},
				{ID: "API", Label: "API"},
				{ID: "Core", Label: "Core Service"},
				{ID: "Dependency", Label: "Dependency"},
				{ID: "Config", Label: "Config Center"},
			},
			Edges: []domain.ScenarioDiagramEdge{
				{From: "Client", To: "API"},
				{From: "API", To: "Core"},
				{From: "Core", To: "Dependency"},
				{From: "Core", To: "Config"},
			},
		},
		References: []string{"变更管理", "故障复盘"},
	},
	{
		Title:       "数据库连接池排队导致接口变慢",
		Description: "订单接口在高峰期响应时间突然上升，数据库 CPU 不高，但应用端等待时间明显增加。请逐步排查。",
		RootCause:   "数据库连接池耗尽导致请求排队，接口响应时间升高。",
		Keywords:    []string{"连接池", "排队", "响应时间"},
		Evidence:    []string{"连接池活跃连接接近上限", "获取连接等待时间升高", "扩容连接池后 P95 延迟下降"},
		Procedure:   []string{"确认接口耗时分布", "查看连接池活跃数与等待队列", "核对慢查询和连接释放", "临时扩容连接池", "补充超时和监控告警"},
		SurfaceClues: []domain.Clue{
			{ClueID: "c1", TriggerKeywords: []string{"连接", "池"}, Content: "连接池活跃连接数长期接近上限。", RecommendedNextAsk: "继续询问等待队列或获取连接耗时。"},
			{ClueID: "c2", TriggerKeywords: []string{"日志", "等待", "耗时"}, Content: "应用日志显示获取数据库连接的等待时间明显升高。", RecommendedNextAsk: "继续询问连接释放或慢查询。"},
		},
		DeepClues:   []domain.Clue{{ClueID: "c3", TriggerKeywords: []string{"释放", "扩容", "队列"}, PrerequisiteClues: []string{"c1"}, Content: "临时扩容连接池后，接口 P95 延迟从 3s 降到 400ms。", RecommendedNextAsk: "可以整理根因并提交答案。"}},
		Distractors: []domain.Clue{{ClueID: "d1", TriggerKeywords: []string{"网络", "丢包"}, Content: "应用到数据库的网络延迟稳定，没有丢包。", IsDistractor: true}},
		Diagram:     "graph TD\nA[Order API] --> B[DB Pool]\nB --> C[(Database)]\nB --> D[Wait Queue]",
		DiagramSpec: domain.ScenarioDiagramSpec{
			Direction: "TD",
			Nodes: []domain.ScenarioDiagramNode{
				{ID: "OrderAPI", Label: "Order API"},
				{ID: "Pool", Label: "DB Pool"},
				{ID: "Database", Label: "Database"},
				{ID: "Queue", Label: "Wait Queue"},
			},
			Edges: []domain.ScenarioDiagramEdge{
				{From: "OrderAPI", To: "Pool"},
				{From: "Pool", To: "Database"},
				{From: "Pool", To: "Queue"},
			},
		},
		References: []string{"连接池监控", "慢查询排查"},
	},
	{
		Title:       "缓存 Key 变更引发回源风暴",
		Description: "发布后缓存命中率下降，数据库读流量升高，业务接口出现周期性抖动。请通过指标和发布记录排查。",
		RootCause:   "缓存 Key 规则变更导致历史缓存失效，引发数据库回源风暴。",
		Keywords:    []string{"缓存", "Key", "回源"},
		Evidence:    []string{"发布后缓存命中率从 92% 降到 35%", "数据库读 QPS 同步升高", "回滚 Key 规则后命中率恢复"},
		Procedure:   []string{"确认命中率变化", "对比发布前后 Key 规则", "核对数据库读流量", "灰度回滚或双读兼容", "补充缓存预热"},
		SurfaceClues: []domain.Clue{
			{ClueID: "c1", TriggerKeywords: []string{"缓存", "命中"}, Content: "发布后缓存命中率从 92% 降到 35%。", RecommendedNextAsk: "继续询问发布内容或 Key 规则。"},
			{ClueID: "c2", TriggerKeywords: []string{"数据库", "QPS", "回源"}, Content: "数据库读 QPS 与缓存命中率下降同时出现。", RecommendedNextAsk: "继续询问回滚或预热情况。"},
		},
		DeepClues:   []domain.Clue{{ClueID: "c3", TriggerKeywords: []string{"Key", "规则", "回滚"}, PrerequisiteClues: []string{"c1"}, Content: "新版本拼接缓存 Key 时新增了一个维度，历史缓存无法命中。", RecommendedNextAsk: "可以提交根因判断。"}},
		Distractors: []domain.Clue{{ClueID: "d1", TriggerKeywords: []string{"CPU", "内存"}, Content: "应用实例 CPU 和内存水位都稳定。", IsDistractor: true}},
		Diagram:     "graph TD\nA[API] --> B[Cache]\nB --> C[(Database)]\nD[Release] --> B",
		DiagramSpec: domain.ScenarioDiagramSpec{
			Direction: "TD",
			Nodes: []domain.ScenarioDiagramNode{
				{ID: "API", Label: "API"},
				{ID: "Cache", Label: "Cache"},
				{ID: "Database", Label: "Database"},
				{ID: "Release", Label: "Release"},
			},
			Edges: []domain.ScenarioDiagramEdge{
				{From: "API", To: "Cache"},
				{From: "Cache", To: "Database"},
				{From: "Release", To: "Cache"},
			},
		},
		References: []string{"缓存预热", "灰度发布"},
	},
}

func NewMockProvider() MockProvider {
	return MockProvider{}
}

func (MockProvider) Info() ProviderInfo {
	return ProviderInfo{Provider: ProviderMock, Model: ProviderMock}
}

func (MockProvider) GenerateScenario(_ context.Context, req ScenarioGenerationRequest) (domain.ScenarioQuestion, error) {
	if req.Domain == "" {
		req.Domain = "database"
	}
	if req.Difficulty == "" {
		req.Difficulty = "L2"
	}
	if req.ScenarioType == "" {
		req.ScenarioType = "troubleshooting"
	}
	if len(req.Tags) == 0 {
		req.Tags = []string{"AI生成", req.Domain}
	}

	variant := mockScenarioVariants[mockVariantIndex(req)]
	title := fmt.Sprintf("%s 方向 %s：%s", req.Domain, req.Difficulty, variant.Title)
	if strings.TrimSpace(req.Constraints.TitleHint) != "" {
		title = strings.TrimSpace(req.Constraints.TitleHint)
	}
	description := variant.Description
	if strings.TrimSpace(req.Constraints.DescriptionHint) != "" {
		description = strings.TrimSpace(req.Constraints.DescriptionHint)
	}
	if len(req.Constraints.TopicScope) > 0 {
		description = fmt.Sprintf("%s 聚焦：%s", description, strings.Join(req.Constraints.TopicScope, " / "))
	}

	question := domain.ScenarioQuestion{
		Title:        title,
		Description:  description,
		Domain:       req.Domain,
		Difficulty:   req.Difficulty,
		ScenarioType: req.ScenarioType,
		Tags:         req.Tags,
		Status:       "active",
		Source:       "llm_generated",
		CreatedBy:    req.UserID,
		Version:      1,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Content: domain.ScenarioContent{
			ModelVersion:   domain.HiddenWorldContractVersion,
			PublicScenario: mockPublicScenario(title, description, variant),
			HiddenWorld:    mockHiddenWorld(variant, req),
		},
	}
	question = PrepareScenarioQuestion(question)
	if err := ValidateDomainJSONSchema(SchemaHiddenWorldQuestion, question); err != nil {
		return domain.ScenarioQuestion{}, err
	}
	return question, ValidateScenarioQuestion(question)
}

func mockPublicScenario(title, description string, variant mockScenarioVariant) *domain.PublicScenario {
	return &domain.PublicScenario{
		Title:       title,
		Description: description,
		Environment: "模拟排障环境：应用服务、数据存储与外部依赖组成一条可观测链路。",
		InitialSymptoms: []string{
			"核心请求耗时在一次变更后明显上升，但请求仍能完成。",
			"同一时间窗口内只有部分业务路径受到影响。",
			fmt.Sprintf("题面背景：%s", variant.Description),
		},
		// 生成模型不得手写架构图；后端由 spec 确定性渲染安全投影。
		ArchitectureDiagram: "",
		ArchitectureDiagramSpec: &domain.ScenarioDiagramSpec{
			Direction: "TD",
			Nodes: []domain.ScenarioDiagramNode{
				{ID: "Client", Label: "客户端"},
				{ID: "Gateway", Label: "API 网关"},
				{ID: "Service", Label: "业务服务"},
				{ID: "Storage", Label: "数据存储"},
			},
			Edges: []domain.ScenarioDiagramEdge{
				{From: "Client", To: "Gateway", Label: "请求"},
				{From: "Gateway", To: "Service", Label: "转发"},
				{From: "Service", To: "Storage", Label: "读写"},
			},
		},
	}
}

func mockHiddenWorld(variant mockScenarioVariant, req ScenarioGenerationRequest) *domain.HiddenWorld {
	primaryLabel := "主要故障方向"
	if len(req.Constraints.HypothesisHints) > 0 && strings.TrimSpace(req.Constraints.HypothesisHints[0]) != "" {
		primaryLabel = strings.TrimSpace(req.Constraints.HypothesisHints[0])
	}
	primaryEvidence := mockVariantEvidence(variant.Evidence, 0, "主要现象证据")
	confirmEvidence := mockVariantEvidence(variant.Evidence, 1, "关键确认结果")
	releaseEvidence := "同一时间窗口存在相关变更记录。"
	if len(req.Constraints.EvidencePathHints) > 0 {
		releaseEvidence += " 路径方向：" + strings.Join(req.Constraints.EvidencePathHints, "、")
	}
	diffEvidence := mockVariantEvidence(variant.Evidence, 2, "变更前后差异")
	observationSuffix := ""
	if len(req.Constraints.ObservationHints) > 0 {
		observationSuffix = " 补充观察方向：" + strings.Join(req.Constraints.ObservationHints, "、")
	}
	requiredActions := append([]string{}, variant.Procedure...)
	if len(req.Constraints.SolutionHints) > 0 {
		requiredActions = append(requiredActions, req.Constraints.SolutionHints...)
	}
	misconceptionWhyWrong := "相关指标与现象不一致，继续沿该方向扩容或重试不能解释主症状。"
	if len(variant.Distractors) > 0 && strings.TrimSpace(variant.Distractors[0].Content) != "" {
		misconceptionWhyWrong = variant.Distractors[0].Content
	}
	if len(req.Constraints.MisconceptionHints) > 0 {
		misconceptionWhyWrong += " 常见误解方向：" + strings.Join(req.Constraints.MisconceptionHints, "、")
	}

	return &domain.HiddenWorld{
		RootCause: domain.RootCause{
			ID:                     "RC_MOCK_PRIMARY",
			Category:               "behavior",
			Component:              "核心业务链路",
			Description:            variant.RootCause,
			SufficientEvidenceSets: [][]string{{"E_PRIMARY", "E_CONFIRM"}, {"E_RELEASE", "E_DIFF"}},
			AcceptedHypotheses:     []string{"H_PRIMARY"},
			SolutionRequirements:   requiredActions,
		},
		CanonicalAnswer: &domain.CanonicalAnswer{
			CanonicalConclusion:     variant.RootCause,
			RootCauseID:             "RC_MOCK_PRIMARY",
			RequiredEvidenceIDs:     []string{"E_PRIMARY", "E_CONFIRM"},
			RequiredCausalRelations: []string{"REL_PRIMARY_EVIDENCE_TO_ROOT_CAUSE", "REL_RELEASE_TO_DIFF"},
			AcceptedEquivalents:     []string{},
			SolutionRequirements:    append([]string{}, requiredActions...),
			AnswerVersion:           "hiddenworld.v2",
		},
		DiagnosticRelations: []string{"REL_PRIMARY_EVIDENCE_TO_ROOT_CAUSE", "REL_RELEASE_TO_DIFF"},
		Hypotheses: []domain.Hypothesis{
			{HypothesisID: "H_PRIMARY", Label: primaryLabel},
			{HypothesisID: "H_RESOURCE", Label: "资源压力"},
			{HypothesisID: "H_DEPENDENCY", Label: "网络或依赖抖动"},
			{HypothesisID: "H_OTHER", Label: "其他"},
		},
		EvidenceGraph: []domain.EvidenceNode{
			{EvidenceID: "E_PRIMARY", Content: primaryEvidence, Category: "logs", Prerequisites: []string{}, ObtainedBy: []string{"inspect:logs.primary"}},
			{EvidenceID: "E_CONFIRM", Content: confirmEvidence, Category: "data", Prerequisites: []string{"E_PRIMARY"}, ObtainedBy: []string{"inspect:data.confirm"}},
			{EvidenceID: "E_RELEASE", Content: releaseEvidence, Category: "change", Prerequisites: []string{}, ObtainedBy: []string{"inspect:change.release"}},
			{EvidenceID: "E_DIFF", Content: diffEvidence, Category: "change", Prerequisites: []string{"E_RELEASE"}, ObtainedBy: []string{"inspect:change.diff"}},
			{EvidenceID: "E_RESOURCE", Content: "资源指标处于正常范围，没有明显排队或饱和。", Category: "resource", Prerequisites: []string{}, ObtainedBy: []string{"inspect:resource.metrics"}},
			{EvidenceID: "E_NETWORK", Content: "服务间延迟稳定，依赖调用没有异常波动。", Category: "dependency", Prerequisites: []string{}, ObtainedBy: []string{"inspect:dependency.network"}},
		},
		Observations: []domain.Observation{
			{Action: "inspect:logs.primary", Result: "查看主请求日志后，可以看到异常集中在同一类业务调用。" + observationSuffix, YieldsEvidence: []string{"E_PRIMARY"}, RulesOut: []string{}, UnmetPrerequisiteResult: ""},
			{Action: "inspect:data.confirm", Result: "对主请求做进一步确认，结果显示执行路径存在明显退化。", YieldsEvidence: []string{"E_CONFIRM"}, RulesOut: []string{}, UnmetPrerequisiteResult: "还没有主请求证据，先说明要确认哪一类调用。"},
			{Action: "inspect:change.release", Result: "发布记录显示异常窗口附近存在相关变更。", YieldsEvidence: []string{"E_RELEASE"}, RulesOut: []string{}, UnmetPrerequisiteResult: "需要先指定要核对的发布窗口。"},
			{Action: "inspect:change.diff", Result: "对比变更前后的配置或结构，发现关键差异。", YieldsEvidence: []string{"E_DIFF"}, RulesOut: []string{}, UnmetPrerequisiteResult: "还没有可对比的变更记录。"},
			{Action: "inspect:resource.metrics", Result: "资源指标正常，暂时没有资源饱和证据。", IsNegative: true, YieldsEvidence: []string{"E_RESOURCE"}, RulesOut: []string{"H_RESOURCE"}, UnmetPrerequisiteResult: ""},
			{Action: "inspect:dependency.network", Result: "依赖链路延迟稳定，没有观察到网络抖动。", IsNegative: true, YieldsEvidence: []string{"E_NETWORK"}, RulesOut: []string{"H_DEPENDENCY"}, UnmetPrerequisiteResult: ""},
		},
		VirtualTools: []domain.VirtualTool{
			{ToolID: "tool.logs.primary", Kind: "logs", Target: "主请求日志", Aliases: []string{"看日志", "查看主请求日志"}, QueryPatterns: []string{"SELECT * FROM request_log", "status=", "connect timeout"}, RedactedParameters: []string{"time_range", "service"}, SimulatedOutput: "返回主请求状态码、目标和耗时的模拟日志。", ObservationAction: "inspect:logs.primary", EvidenceIDs: []string{"E_PRIMARY"}},
			{ToolID: "tool.database.confirm", Kind: "database", Target: "业务确认数据", Aliases: []string{"看数据库", "数据库日志", "查数据库"}, QueryPatterns: []string{"SELECT * FROM business", "SELECT count(*)"}, RedactedParameters: []string{"table", "time_range"}, SimulatedOutput: "返回题目虚拟数据库中的只读确认结果。", ObservationAction: "inspect:data.confirm", EvidenceIDs: []string{"E_CONFIRM"}},
			{ToolID: "tool.config.release", Kind: "config", Target: "发布配置", Aliases: []string{"看配置", "看发布记录"}, QueryPatterns: []string{"SELECT * FROM release", "config diff"}, RedactedParameters: []string{"release_id"}, SimulatedOutput: "返回发布窗口附近的模拟配置记录。", ObservationAction: "inspect:change.release", EvidenceIDs: []string{"E_RELEASE"}},
			{ToolID: "tool.metrics.resource", Kind: "metrics", Target: "资源指标", Aliases: []string{"看指标", "看资源"}, QueryPatterns: []string{"SELECT cpu", "metrics"}, RedactedParameters: []string{"service", "time_range"}, SimulatedOutput: "返回资源指标的模拟结果。", ObservationAction: "inspect:resource.metrics", EvidenceIDs: []string{"E_RESOURCE"}},
		},
		SolutionRubric: domain.SolutionRubric{
			RequiredActions:   requiredActions,
			VerificationSteps: []string{"复查关键指标是否回到稳定区间", "确认同类请求不再重复出现异常"},
			RollbackNotes:     []string{"变更前先保留可回滚版本，并在低峰期验证。"},
		},
		MisconceptionRules: []domain.MisconceptionRule{{MisconceptionID: "M_BLAME_RESOURCE", PatternHypotheses: []string{"H_RESOURCE"}, WhyWrong: misconceptionWhyWrong}},
	}
}

func mockVariantEvidence(values []string, index int, fallback string) string {
	if index >= 0 && index < len(values) && strings.TrimSpace(values[index]) != "" {
		return strings.TrimSpace(values[index])
	}
	return fallback
}

func mockVariantIndex(req ScenarioGenerationRequest) int {
	seed := req.Nonce
	if seed == "" {
		seed = fmt.Sprintf("%s-%s-%d", req.Domain, req.Difficulty, time.Now().UnixNano())
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(seed))
	return int(hash.Sum32() % uint32(len(mockScenarioVariants)))
}

func (MockProvider) StructureCommunityPost(_ context.Context, req CommunityStructureRequest) (domain.ScenarioContent, error) {
	content := domain.ScenarioContent{
		RootCause:           "待讲师审核确认的根因：" + firstSentence(req.RawContent),
		RootCauseKeywords:   req.Tags,
		KeyEvidence:         []string{"由 AI 结构化预览生成，等待人工确认。"},
		StandardProcedure:   []string{"补充现象", "确认根因", "生成提示策略", "讲师审核"},
		ArchitectureDiagram: "graph TD\nA[UGC 原始案例] --> B[AI 结构化预览]\nB --> C[讲师审核]\nC --> D[标准情景题]",
		ArchitectureDiagramSpec: &domain.ScenarioDiagramSpec{
			Direction: "TD",
			Nodes: []domain.ScenarioDiagramNode{
				{ID: "UGC", Label: "UGC 原始案例"},
				{ID: "Preview", Label: "AI 结构化预览"},
				{ID: "Review", Label: "讲师审核"},
				{ID: "Scenario", Label: "标准情景题"},
			},
			Edges: []domain.ScenarioDiagramEdge{
				{From: "UGC", To: "Preview"},
				{From: "Preview", To: "Review"},
				{From: "Review", To: "Scenario"},
			},
		},
		ReferenceLinks: []string{"UGC 案例工坊"},
		RevealStrategy: domain.RevealStrategy{
			SurfaceClues: []domain.Clue{{ClueID: "c1", TriggerKeywords: req.Tags, Content: "该案例已进入结构化预览，需人工确认后开放训练。"}},
		},
	}
	content = PrepareScenarioContent(content, domain.ScenarioQuestion{Title: req.Title, Domain: req.Domain})
	if err := ValidateDomainJSONSchema(SchemaScenarioContentPreview, content); err != nil {
		return domain.ScenarioContent{}, err
	}
	return content, ValidateScenarioContent(content, true)
}

func (p MockProvider) StructureCommunityPostStream(ctx context.Context, req CommunityStructureRequest, onDelta func(string)) (domain.ScenarioContent, error) {
	content, err := p.StructureCommunityPost(ctx, req)
	if err == nil {
		emitMockDelta(onDelta, content.RootCause)
	}
	return content, err
}

func (MockProvider) RewriteScenarioReply(_ context.Context, req ScenarioReplyRequest) (string, error) {
	if req.AllowedContent == "" {
		req.AllowedContent = "暂未发现新的可释放线索。你可以换一个排查维度继续提问。"
	}
	return req.AllowedContent, ValidateDomainJSONSchema(SchemaScenarioReply, map[string]string{"reply": req.AllowedContent})
}

func (p MockProvider) RewriteScenarioReplyStream(ctx context.Context, req ScenarioReplyRequest, onDelta func(string)) (string, error) {
	reply, err := p.RewriteScenarioReply(ctx, req)
	if err == nil {
		emitMockDelta(onDelta, reply)
	}
	return reply, err
}

func (MockProvider) GenerateInterviewFeedback(_ context.Context, req InterviewFeedbackRequest) (InterviewFeedback, error) {
	feedback := InterviewFeedback{
		Highlights:       req.Evaluation.Highlights,
		Deficiencies:     req.Evaluation.Deficiencies,
		FollowUpQuestion: req.Evaluation.FollowUpQuestion,
	}
	if req.NeedReport {
		feedback.FinalReport = DefaultInterviewReport(req.Evaluation)
	}
	if err := ValidateDomainJSONSchema(SchemaInterviewFeedback, feedback); err != nil {
		return InterviewFeedback{}, err
	}
	return feedback, ValidateInterviewFeedback(feedback, req.Evaluation.FollowUpTriggered, req.NeedReport)
}

func (p MockProvider) GenerateInterviewFeedbackStream(ctx context.Context, req InterviewFeedbackRequest, onDelta func(string)) (InterviewFeedback, error) {
	feedback, err := p.GenerateInterviewFeedback(ctx, req)
	if err == nil {
		emitMockDelta(onDelta, strings.Join(append(feedback.Highlights, feedback.Deficiencies...), " "))
	}
	return feedback, err
}

func (MockProvider) RewriteInterviewOpening(_ context.Context, req InterviewOpeningRequest) (InterviewOpeningRewrite, error) {
	subject := strings.TrimSpace(req.Subject)
	if subject == "" {
		subject = "当前故障"
	}
	scenario := strings.TrimSpace(req.SourceText)
	if scenario == "" {
		scenario = "你需要处理与「" + subject + "」相关的线上问题"
	} else {
		// 截断过长素材，保留前半段作情景
		runes := []rune(scenario)
		if len(runes) > 60 {
			scenario = string(runes[:60])
		}
		for _, sep := range []string{"请说明", "请给出", "请回答"} {
			if idx := strings.Index(scenario, sep); idx > 0 {
				scenario = strings.TrimSpace(scenario[:idx])
				break
			}
		}
	}
	out := InterviewOpeningRewrite{
		Opening: strings.TrimRight(scenario, "。.!！？?") + "。你第一步会先看什么？",
	}
	if err := ValidateDomainJSONSchema(SchemaInterviewOpening, out); err != nil {
		return InterviewOpeningRewrite{}, err
	}
	return out, ValidateInterviewOpening(out)
}

func (MockProvider) CheckSensitiveContent(_ context.Context, req SensitiveCheckRequest) (domain.SensitiveCheckResult, error) {
	result := domain.SensitiveCheckResult{
		Status:    "clear",
		Findings:  []domain.SensitiveFinding{},
		Source:    "model",
		RiskLevel: "none",
		Summary:   "模型辅助检测未发现额外风险。",
		CheckedAt: time.Now(),
	}
	lower := strings.ToLower(req.Text)
	add := func(kind, excerpt, severity, suggestion string, confidence float64) {
		result.Findings = append(result.Findings, domain.SensitiveFinding{
			Type:            kind,
			Field:           defaultString(req.Field, "content"),
			Excerpt:         Sanitize(truncateForFinding(excerpt)),
			RedactedExcerpt: Sanitize(truncateForFinding(excerpt)),
			Severity:        normalizeSeverity(severity),
			Suggestion:      suggestion,
			Source:          "model",
			Confidence:      confidence,
		})
	}
	tokens := []struct {
		needle     string
		kind       string
		severity   string
		suggestion string
		confidence float64
	}{
		{"acme", "company", "medium", "将真实公司名替换为业务系统代称。", 0.86},
		{"corp", "company", "medium", "将真实公司名替换为业务系统代称。", 0.82},
		{"客户", "customer", "medium", "将客户名称替换为客户A或行业代称。", 0.76},
		{"张三", "person", "medium", "将真实人名替换为角色名称。", 0.78},
		{"svc-", "internal_service", "medium", "将内部服务名替换为通用服务代称。", 0.74},
		{"service-", "internal_service", "medium", "将内部服务名替换为通用服务代称。", 0.74},
		{"拓扑", "topology", "low", "仅保留抽象拓扑关系，不暴露真实节点命名。", 0.66},
	}
	for _, token := range tokens {
		if strings.Contains(lower, strings.ToLower(token.needle)) {
			add(token.kind, token.needle, token.severity, token.suggestion, token.confidence)
		}
	}
	if len(result.Findings) > 0 {
		result.Status = "risk"
		result.Sanitized = true
		result.RiskLevel = riskLevelFromFindings(result.Findings)
		result.Blocked = shouldBlockSensitiveFindings(result.Findings)
		result.Summary = "模型辅助检测发现需要脱敏或人工确认的内容。"
	}
	if err := ValidateDomainJSONSchema(SchemaSensitiveCheck, result); err != nil {
		return domain.SensitiveCheckResult{}, err
	}
	return result, ValidateSensitiveCheck(result)
}

func emitMockDelta(onDelta func(string), text string) {
	if onDelta == nil || text == "" {
		return
	}
	for _, chunk := range StreamTextChunks(text, 24) {
		onDelta(chunk)
	}
}

// StreamTextChunks 按 rune 切分文本，供各层复用同一套流式分片规则。
func StreamTextChunks(text string, size int) []string {
	if size <= 0 {
		size = 24
	}
	runes := []rune(text)
	if len(runes) <= size {
		return []string{text}
	}
	chunks := make([]string, 0, (len(runes)+size-1)/size)
	for len(runes) > 0 {
		n := size
		if len(runes) < n {
			n = len(runes)
		}
		chunks = append(chunks, string(runes[:n]))
		runes = runes[n:]
	}
	return chunks
}
