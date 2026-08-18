package domain

import (
	"encoding/json"
	"strings"
)

const HiddenWorldContractVersion = "hiddenworld.v1"

// MarshalJSON 保证 hiddenworld.v1 的持久化形状只有
// model_version + public_scenario + hidden_world。
// 旧字段可以在进程内短暂作为迁移投影存在，但不能重新写回数据库或 API。
func (content ScenarioContent) MarshalJSON() ([]byte, error) {
	if content.ModelVersion == HiddenWorldContractVersion {
		return json.Marshal(struct {
			ModelVersion   string          `json:"model_version"`
			PublicScenario *PublicScenario `json:"public_scenario,omitempty"`
			HiddenWorld    *HiddenWorld    `json:"hidden_world,omitempty"`
		}{
			ModelVersion:   content.ModelVersion,
			PublicScenario: content.PublicScenario,
			HiddenWorld:    content.HiddenWorld,
		})
	}
	type plainScenarioContent ScenarioContent
	return json.Marshal(plainScenarioContent(content))
}

// UnmarshalJSON 在读取 hiddenworld.v1 后恢复旧 Go 运行时需要的只读投影。
// 阶段 3 删除旧语义 Agent、旧评分直接字段后，这个投影应整体移除。
func (content *ScenarioContent) UnmarshalJSON(raw []byte) error {
	type plainScenarioContent ScenarioContent
	var decoded plainScenarioContent
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*content = ScenarioContent(decoded).WithHiddenWorldCompatibility()
	return nil
}

func (content ScenarioContent) WithHiddenWorldCompatibility() ScenarioContent {
	if content.ModelVersion != HiddenWorldContractVersion || content.HiddenWorld == nil {
		return content
	}
	world := content.HiddenWorld
	content.RootCause = world.RootCause.Description
	content.RootCauseKeywords = hiddenWorldRootKeywords(world)
	content.KeyEvidence = make([]string, 0, len(world.EvidenceGraph))
	content.RevealStrategy = RevealStrategy{}
	for _, node := range world.EvidenceGraph {
		content.KeyEvidence = append(content.KeyEvidence, node.Content)
		clue := Clue{
			ClueID:            node.EvidenceID,
			TriggerKeywords:   hiddenWorldEvidenceKeywords(node),
			PrerequisiteClues: append([]string{}, node.Prerequisites...),
			Content:           node.Content,
		}
		if len(node.Prerequisites) == 0 {
			content.RevealStrategy.SurfaceClues = append(content.RevealStrategy.SurfaceClues, clue)
		} else {
			content.RevealStrategy.DeepClues = append(content.RevealStrategy.DeepClues, clue)
		}
	}
	content.StandardProcedure = append(content.StandardProcedure, world.SolutionRubric.RequiredActions...)
	content.StandardProcedure = append(content.StandardProcedure, world.SolutionRubric.VerificationSteps...)
	content.StandardProcedure = append(content.StandardProcedure, world.SolutionRubric.RollbackNotes...)
	if content.PublicScenario != nil {
		content.ArchitectureDiagram = content.PublicScenario.ArchitectureDiagram
	}
	return content
}

func hiddenWorldRootKeywords(world *HiddenWorld) []string {
	values := []string{world.RootCause.Category, world.RootCause.Component}
	accepted := map[string]bool{}
	for _, id := range world.RootCause.AcceptedHypotheses {
		accepted[id] = true
	}
	for _, hypothesis := range world.Hypotheses {
		if accepted[hypothesis.HypothesisID] {
			values = append(values, hypothesis.Label)
		}
	}
	return uniqueScenarioTerms(values)
}

func hiddenWorldEvidenceKeywords(node EvidenceNode) []string {
	values := []string{node.Category, node.Content}
	values = append(values, node.ObtainedBy...)
	for _, action := range node.ObtainedBy {
		values = append(values, strings.FieldsFunc(action, func(r rune) bool {
			return r == ':' || r == '.' || r == '_' || r == '-' || r == '/'
		})...)
	}
	return uniqueScenarioTerms(values)
}

func uniqueScenarioTerms(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
