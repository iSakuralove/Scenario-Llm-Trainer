package ai

import (
	"encoding/json"
	"fmt"
	"strings"

	"situational-teaching/backend/internal/domain"
)

type OutputProcessRequest struct {
	Task           string
	Schema         string
	OutputMode     string
	RawOutput      string
	DomainValue    interface{}
	Target         interface{}
	Stream         bool
	SafetyPolicy   string
	ForbiddenTerms []string
	Validate       func(interface{}) error
}

type OutputProcessResult struct {
	NormalizedOutput string
	Output           OutputTelemetry
	Validation       ValidationResult
	Safety           SafetyVerdict
	DisplayAllowed   bool
}

type OutputProcessor struct{}

func NewOutputProcessor() OutputProcessor {
	return OutputProcessor{}
}

func (OutputProcessor) Process(req OutputProcessRequest) (OutputProcessResult, error) {
	result := OutputProcessResult{
		Output: OutputTelemetry{ParseStatus: "skipped"},
		Validation: ValidationResult{
			Required: req.Schema != "",
			Schema:   req.Schema,
			Status:   "skipped",
		},
		Safety:         SafetyVerdict{Policy: defaultString(req.SafetyPolicy, SafetyPolicyDefault), Status: "passed"},
		DisplayAllowed: true,
	}
	if req.OutputMode == OutputModeJSON || req.Schema != "" || strings.TrimSpace(req.RawOutput) != "" {
		normalized, repaired, err := normalizeJSONOutput(req.RawOutput)
		if req.DomainValue == nil || strings.TrimSpace(req.RawOutput) != "" {
			if err != nil {
				result.Output.ParseStatus = "failed"
				result.Validation.Status = "failed"
				result.Validation.Detail = sanitizeErrorMessage(err.Error())
				return result, fmt.Errorf("json parse failed: %s", result.Validation.Detail)
			}
			result.NormalizedOutput = normalized
			result.Output = OutputTelemetry{ParseStatus: "parsed", RepairUsed: repaired}
			if req.Schema != "" {
				if err := ValidateJSONSchema(req.Schema, normalized); err != nil {
					result.Validation.Status = "failed"
					result.Validation.Detail = sanitizeErrorMessage(err.Error())
					return result, fmt.Errorf("schema validation failed: %s", result.Validation.Detail)
				}
			}
			if req.Target != nil {
				if err := json.Unmarshal([]byte(normalized), req.Target); err != nil {
					result.Validation.Status = "failed"
					result.Validation.Detail = sanitizeErrorMessage(err.Error())
					return result, fmt.Errorf("json decode failed: %s", result.Validation.Detail)
				}
			}
		}
	}
	if req.DomainValue != nil && result.Output.ParseStatus == "skipped" {
		result.Output.ParseStatus = "parsed"
		if req.Schema != "" {
			if err := ValidateDomainJSONSchema(req.Schema, req.DomainValue); err != nil {
				result.Validation.Status = "failed"
				result.Validation.Detail = sanitizeErrorMessage(err.Error())
				return result, fmt.Errorf("schema validation failed: %s", result.Validation.Detail)
			}
		}
	}
	if req.Schema != "" {
		result.Validation.Status = "passed"
		result.Validation.Detail = "schema validation passed"
	}
	if req.Validate != nil {
		if err := req.Validate(req.DomainValue); err != nil {
			result.Validation.Status = "failed"
			result.Validation.Detail = sanitizeErrorMessage(err.Error())
			return result, fmt.Errorf("domain validation failed: %s", result.Validation.Detail)
		}
		result.Validation.Detail = "schema and domain validation passed"
	}
	return result, nil
}

func normalizeJSONOutput(raw string) (string, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false, fmt.Errorf("empty json")
	}
	extracted := extractJSONObject(trimmed)
	if extracted == "" {
		return "", false, fmt.Errorf("response is not json")
	}
	var value interface{}
	if err := json.Unmarshal([]byte(extracted), &value); err != nil {
		return "", extracted != trimmed, err
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return "", extracted != trimmed, err
	}
	return string(normalized), extracted != trimmed || string(normalized) != trimmed, nil
}

type SafetyFilterRequest struct {
	Task           string
	Text           string
	Policy         string
	ForbiddenTerms []string
}

type SafetyFilterResult struct {
	Status           string
	Blocked          bool
	RewriteUsed      bool
	RiskLevel        string
	FindingsCount    int
	SanitizedPreview string
	Detail           string
}

type SafetyFilter struct{}

func NewSafetyFilter() SafetyFilter {
	return SafetyFilter{}
}

func (SafetyFilter) Apply(req SafetyFilterRequest) SafetyFilterResult {
	text := strings.TrimSpace(req.Text)
	result := SafetyFilterResult{
		Status:           "passed",
		RiskLevel:        "none",
		SanitizedPreview: truncate(SanitizeFields(text), 240),
	}
	if text == "" {
		return result
	}
	rewritten, changed := SafetyRewrite(text, req.ForbiddenTerms)
	if changed {
		result.RewriteUsed = true
		result.SanitizedPreview = truncate(SanitizeFields(rewritten), 240)
		if containsForbiddenTerm(text, req.ForbiddenTerms) {
			result.Status = "blocked"
			result.Blocked = true
			result.RiskLevel = "high"
			result.FindingsCount = 1
			result.Detail = "output matched forbidden answer leak policy"
			return result
		}
		result.Status = "rewritten"
		result.RiskLevel = "medium"
		result.FindingsCount = 1
		result.Detail = "output contained sensitive fields and was sanitized"
		return result
	}
	if result.SanitizedPreview != text {
		result.RewriteUsed = true
		result.Status = "rewritten"
		result.RiskLevel = "medium"
		result.FindingsCount = 1
		result.Detail = "output contained sensitive fields and was sanitized"
	}
	return result
}

func containsForbiddenTerm(text string, forbidden []string) bool {
	for _, item := range forbidden {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.Contains(strings.ToLower(text), strings.ToLower(item)) || RootCauseMatch(text, item, nil) >= 82 {
			return true
		}
	}
	return false
}

func SanitizeScenarioContentFields(content domain.ScenarioContent) domain.ScenarioContent {
	content.RootCause = SanitizeFields(content.RootCause)
	for i, value := range content.RootCauseKeywords {
		content.RootCauseKeywords[i] = SanitizeFields(value)
	}
	for i, value := range content.KeyEvidence {
		content.KeyEvidence[i] = SanitizeFields(value)
	}
	for i, value := range content.StandardProcedure {
		content.StandardProcedure[i] = SanitizeFields(value)
	}
	for i, value := range content.ReferenceLinks {
		content.ReferenceLinks[i] = SanitizeFields(value)
	}
	content.ArchitectureDiagram = SanitizeFields(content.ArchitectureDiagram)
	content.ArchitectureDiagramSpec = SanitizeScenarioDiagramSpec(content.ArchitectureDiagramSpec)
	content.RevealStrategy.SurfaceClues = sanitizeClues(content.RevealStrategy.SurfaceClues)
	content.RevealStrategy.DeepClues = sanitizeClues(content.RevealStrategy.DeepClues)
	content.RevealStrategy.Distractors = sanitizeClues(content.RevealStrategy.Distractors)
	return content
}

func SanitizeScenarioDiagramSpec(spec *domain.ScenarioDiagramSpec) *domain.ScenarioDiagramSpec {
	if spec == nil {
		return nil
	}
	copy := *spec
	copy.Nodes = append([]domain.ScenarioDiagramNode{}, spec.Nodes...)
	for i := range copy.Nodes {
		copy.Nodes[i].Label = SanitizeFields(copy.Nodes[i].Label)
	}
	copy.Edges = append([]domain.ScenarioDiagramEdge{}, spec.Edges...)
	for i := range copy.Edges {
		copy.Edges[i].Label = SanitizeFields(copy.Edges[i].Label)
	}
	return &copy
}

func sanitizeClues(clues []domain.Clue) []domain.Clue {
	out := append([]domain.Clue{}, clues...)
	for i, clue := range out {
		clue.Content = SanitizeFields(clue.Content)
		clue.RecommendedNextAsk = SanitizeFields(clue.RecommendedNextAsk)
		for j, value := range clue.TriggerKeywords {
			clue.TriggerKeywords[j] = SanitizeFields(value)
		}
		out[i] = clue
	}
	return out
}
