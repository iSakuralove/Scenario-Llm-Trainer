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
	assets := []string{"fixed_hiddenworld/hw-db-index-001.json"}
	items := make([]domain.ScenarioQuestion, 0, len(assets))
	for _, asset := range assets {
		items = append(items, loadFixedHiddenWorldSeed(asset, now))
	}
	return items
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
