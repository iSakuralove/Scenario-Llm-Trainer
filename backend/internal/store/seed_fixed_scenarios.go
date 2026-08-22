package store

import (
	"embed"
	"encoding/json"
	"fmt"
	"time"

	"situational-teaching/backend/internal/domain"
)

//go:generate go run ./cmd/sync_hiddenworld_bank

// fixedHiddenWorldAssets 是 agent 固定题 JSON 的生成副本。
// 源文件只维护在 agent/src/hiddenworld/bank/fixed；同步命令负责复制，测试负责防漂移。
//
//go:embed fixed_hiddenworld/*.json
var fixedHiddenWorldAssets embed.FS

// fixedScenarioV3Assets 是 scenario.v3 归一化题库的生成副本；新会话从这里
// 构造 Runtime 投影，旧 fixed_hiddenworld 资产仅保留用于兼容和漂移核对。
//
//go:embed fixed_scenario_v3/*.json
var fixedScenarioV3Assets embed.FS

type fixedScenarioV3Metadata struct {
	StableCode   string   `json:"stable_code"`
	Domain       string   `json:"domain"`
	Difficulty   string   `json:"difficulty"`
	ScenarioType string   `json:"scenario_type"`
	Tags         []string `json:"tags"`
	Source       string   `json:"source"`
	Version      int      `json:"version"`
	Status       string   `json:"status"`
}

type fixedScenarioV3Question struct {
	ContractVersion     string                     `json:"contract_version"`
	Metadata            fixedScenarioV3Metadata    `json:"metadata"`
	PublicScenario      domain.PublicScenario      `json:"public_scenario"`
	TeachingModel       domain.TeachingModel       `json:"teaching_model"`
	HypothesisCatalog   []domain.Hypothesis        `json:"hypothesis_catalog"`
	EvidenceGraph       []domain.EvidenceNode      `json:"evidence_graph"`
	ObservationCatalog  []domain.Observation       `json:"observation_catalog"`
	ToolCatalog         []domain.VirtualTool       `json:"tool_catalog"`
	HintLadder          []domain.HintStep          `json:"hint_ladder"`
	MisconceptionRules  []domain.MisconceptionRule `json:"misconception_rules"`
	SolutionRubric      domain.SolutionRubric      `json:"solution_rubric"`
	RootCause           domain.RootCause           `json:"root_cause"`
	DiagnosticRelations []string                   `json:"diagnostic_relations"`
	CanonicalAnswer     domain.CanonicalAnswer     `json:"canonical_answer"`
}

type fixedHiddenWorldQuestion struct {
	QuestionID     string                `json:"question_id"`
	Domain         string                `json:"domain"`
	Difficulty     string                `json:"difficulty"`
	ScenarioType   string                `json:"scenario_type"`
	Tags           []string              `json:"tags"`
	Source         string                `json:"source"`
	Version        int                   `json:"version"`
	Status         string                `json:"status"`
	ModelVersion   string                `json:"model_version"`
	PublicScenario domain.PublicScenario `json:"public_scenario"`
	HiddenWorld    domain.HiddenWorld    `json:"hidden_world"`
}

func seedDiagnosticScenarios(now time.Time) []domain.ScenarioQuestion {
	assets := []string{
		"fixed_scenario_v3/hw-db-index-001.json",
		"fixed_scenario_v3/hw-network-vip-001.json",
		"fixed_scenario_v3/hw-k8s-io-001.json",
		"fixed_scenario_v3/hw-cache-key-001.json",
	}
	items := make([]domain.ScenarioQuestion, 0, len(assets))
	for _, asset := range assets {
		items = append(items, loadFixedScenarioV3Seed(asset, now))
	}
	return items
}

func loadFixedScenarioV3Seed(asset string, now time.Time) domain.ScenarioQuestion {
	raw, err := fixedScenarioV3Assets.ReadFile(asset)
	if err != nil {
		panic(fmt.Sprintf("read fixed scenario.v3 seed %s: %v", asset, err))
	}
	var fixed fixedScenarioV3Question
	if err := json.Unmarshal(raw, &fixed); err != nil {
		panic(fmt.Sprintf("decode fixed scenario.v3 seed %s: %v", asset, err))
	}
	if fixed.ContractVersion != domain.ScenarioV3ContractVersion {
		panic(fmt.Sprintf("fixed scenario.v3 seed %s has contract_version %q", asset, fixed.ContractVersion))
	}
	publicScenario := fixed.PublicScenario
	hiddenWorld := domain.HiddenWorld{
		RootCause:           fixed.RootCause,
		CanonicalAnswer:     &fixed.CanonicalAnswer,
		DiagnosticRelations: append([]string{}, fixed.DiagnosticRelations...),
		Hypotheses:          append([]domain.Hypothesis{}, fixed.HypothesisCatalog...),
		EvidenceGraph:       append([]domain.EvidenceNode{}, fixed.EvidenceGraph...),
		Observations:        append([]domain.Observation{}, fixed.ObservationCatalog...),
		VirtualTools:        append([]domain.VirtualTool{}, fixed.ToolCatalog...),
		SolutionRubric:      fixed.SolutionRubric,
		MisconceptionRules:  append([]domain.MisconceptionRule{}, fixed.MisconceptionRules...),
		TeachingModel:       &fixed.TeachingModel,
	}
	return domain.ScenarioQuestion{
		ID:           fixed.Metadata.StableCode,
		Title:        fixed.PublicScenario.Title,
		Description:  fixed.PublicScenario.Description,
		Domain:       fixed.Metadata.Domain,
		Difficulty:   fixed.Metadata.Difficulty,
		ScenarioType: fixed.Metadata.ScenarioType,
		Tags:         append([]string{}, fixed.Metadata.Tags...),
		Content: domain.ScenarioContent{
			ModelVersion:    domain.HiddenWorldContractVersion,
			ContractVersion: fixed.ContractVersion,
			PublicScenario:  &publicScenario,
			HiddenWorld:     &hiddenWorld,
		},
		Status:    fixed.Metadata.Status,
		Source:    fixed.Metadata.Source,
		CreatedBy: "user-admin",
		Version:   fixed.Metadata.Version,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func loadFixedHiddenWorldSeed(asset string, now time.Time) domain.ScenarioQuestion {
	raw, err := fixedHiddenWorldAssets.ReadFile(asset)
	if err != nil {
		panic(fmt.Sprintf("read fixed HiddenWorld seed %s: %v", asset, err))
	}
	var fixed fixedHiddenWorldQuestion
	if err := json.Unmarshal(raw, &fixed); err != nil {
		panic(fmt.Sprintf("decode fixed HiddenWorld seed %s: %v", asset, err))
	}
	publicScenario := fixed.PublicScenario
	hiddenWorld := fixed.HiddenWorld
	return domain.ScenarioQuestion{
		ID:           fixed.QuestionID,
		Title:        fixed.PublicScenario.Title,
		Description:  fixed.PublicScenario.Description,
		Domain:       fixed.Domain,
		Difficulty:   fixed.Difficulty,
		ScenarioType: fixed.ScenarioType,
		Tags:         append([]string{}, fixed.Tags...),
		Content: domain.ScenarioContent{
			ModelVersion:   fixed.ModelVersion,
			PublicScenario: &publicScenario,
			HiddenWorld:    &hiddenWorld,
		},
		Status:    fixed.Status,
		Source:    fixed.Source,
		CreatedBy: "user-admin",
		Version:   fixed.Version,
		CreatedAt: now,
		UpdatedAt: now,
	}
}
