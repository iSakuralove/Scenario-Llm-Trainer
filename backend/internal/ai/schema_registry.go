package ai

import (
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"situational-teaching/backend/internal/domain"
)

const (
	SchemaScenarioQuestion       = "scenario_question"
	SchemaScenarioContentPreview = "scenario_content_preview"
	SchemaInterviewFeedback      = "interview_feedback"
	SchemaScenarioReply          = "scenario_reply"
	SchemaSensitiveCheck         = "sensitive_check"
)

//go:embed schemas/*.schema.json
var schemaFS embed.FS

type JSONSchemaInfo struct {
	Name        string `json:"name"`
	SchemaName  string `json:"schema_name"`
	Version     string `json:"version"`
	Task        string `json:"task"`
	Description string `json:"description"`
	Target      string `json:"target"`
	Status      string `json:"status"`
}

type jsonSchema struct {
	ID                   string                `json:"$id"`
	Title                string                `json:"title"`
	Description          string                `json:"description"`
	Version              string                `json:"x-version"`
	Task                 string                `json:"x-task"`
	Type                 string                `json:"type"`
	Required             []string              `json:"required"`
	Properties           map[string]jsonSchema `json:"properties"`
	AdditionalProperties *bool                 `json:"additionalProperties"`
	Defs                 map[string]jsonSchema `json:"$defs"`
	Ref                  string                `json:"$ref"`
	Enum                 []string              `json:"enum"`
	Pattern              string                `json:"pattern"`
	Items                *jsonSchema           `json:"items"`
	MinItems             *int                  `json:"minItems"`
	MinLength            *int                  `json:"minLength"`
	Minimum              *float64              `json:"minimum"`
	Maximum              *float64              `json:"maximum"`
}

type schemaEntry struct {
	info   JSONSchemaInfo
	schema jsonSchema
	raw    []byte
	err    error
}

var schemaEntries = loadJSONSchemas()

func ListJSONSchemas() []JSONSchemaInfo {
	out := make([]JSONSchemaInfo, 0, len(schemaEntries))
	for _, entry := range schemaEntries {
		out = append(out, entry.info)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SchemaName < out[j].SchemaName
	})
	return out
}

func SchemaValidatorStatus() []JSONSchemaInfo {
	return ListJSONSchemas()
}

func ValidateJSONSchema(schemaName, rawJSON string) error {
	entry, ok := schemaEntries[schemaName]
	if !ok {
		return fmt.Errorf("unknown json schema %q", schemaName)
	}
	if entry.err != nil {
		return entry.err
	}
	var value interface{}
	if err := json.Unmarshal([]byte(extractJSONObject(rawJSON)), &value); err != nil {
		return fmt.Errorf("%s schema json parse failed: %w", schemaName, err)
	}
	if err := validateSchemaValue(value, entry.schema, entry.schema, schemaName); err != nil {
		return fmt.Errorf("%s schema validation failed: %w", schemaName, err)
	}
	return nil
}

func ValidateDomainJSONSchema(schemaName string, value interface{}) error {
	raw, err := json.Marshal(projectDomainValueForSchema(schemaName, value))
	if err != nil {
		return err
	}
	return ValidateJSONSchema(schemaName, string(raw))
}

func projectDomainValueForSchema(schemaName string, value interface{}) interface{} {
	switch schemaName {
	case SchemaScenarioQuestion:
		switch typed := value.(type) {
		case domain.ScenarioQuestion:
			return scenarioQuestionSchemaProjection(typed)
		case *domain.ScenarioQuestion:
			if typed != nil {
				return scenarioQuestionSchemaProjection(*typed)
			}
		case domain.ScenarioQuestionView:
			return scenarioQuestionSchemaProjection(domain.ScenarioQuestion{
				Title:        typed.Title,
				Description:  typed.Description,
				Domain:       typed.Domain,
				Difficulty:   typed.Difficulty,
				ScenarioType: typed.ScenarioType,
				Tags:         typed.Tags,
				Content:      typed.Content,
			})
		case *domain.ScenarioQuestionView:
			if typed != nil {
				return scenarioQuestionSchemaProjection(domain.ScenarioQuestion{
					Title:        typed.Title,
					Description:  typed.Description,
					Domain:       typed.Domain,
					Difficulty:   typed.Difficulty,
					ScenarioType: typed.ScenarioType,
					Tags:         typed.Tags,
					Content:      typed.Content,
				})
			}
		}
	case SchemaScenarioContentPreview:
		switch typed := value.(type) {
		case domain.ScenarioContent:
			return scenarioContentSchemaProjection(typed, false)
		case *domain.ScenarioContent:
			if typed != nil {
				return scenarioContentSchemaProjection(*typed, false)
			}
		}
	}
	return value
}

func scenarioQuestionSchemaProjection(question domain.ScenarioQuestion) map[string]interface{} {
	return map[string]interface{}{
		"title":         question.Title,
		"description":   question.Description,
		"domain":        question.Domain,
		"difficulty":    question.Difficulty,
		"scenario_type": question.ScenarioType,
		"tags":          question.Tags,
		"content":       scenarioContentSchemaProjection(question.Content, true),
	}
}

func scenarioContentSchemaProjection(content domain.ScenarioContent, requireFull bool) map[string]interface{} {
	out := map[string]interface{}{
		"root_cause":                content.RootCause,
		"key_evidence":              content.KeyEvidence,
		"standard_procedure":        content.StandardProcedure,
		"architecture_diagram":      "",
		"architecture_diagram_spec": content.ArchitectureDiagramSpec,
		"reveal_strategy":           revealStrategySchemaProjection(content.RevealStrategy, requireFull),
	}
	if requireFull || len(content.RootCauseKeywords) > 0 {
		out["root_cause_keywords"] = content.RootCauseKeywords
	}
	if len(content.ReferenceLinks) > 0 {
		out["reference_links"] = content.ReferenceLinks
	}
	return out
}

func revealStrategySchemaProjection(strategy domain.RevealStrategy, requireFull bool) map[string]interface{} {
	out := map[string]interface{}{
		"surface_clues": clueSchemaProjections(strategy.SurfaceClues),
	}
	if requireFull || len(strategy.DeepClues) > 0 {
		out["deep_clues"] = clueSchemaProjections(strategy.DeepClues)
	}
	if requireFull || len(strategy.Distractors) > 0 {
		out["distractors"] = clueSchemaProjections(strategy.Distractors)
	}
	return out
}

func clueSchemaProjections(clues []domain.Clue) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(clues))
	for _, clue := range clues {
		item := map[string]interface{}{
			"clue_id":          clue.ClueID,
			"trigger_keywords": clue.TriggerKeywords,
			"content":          clue.Content,
			"is_distractor":    clue.IsDistractor,
		}
		if len(clue.PrerequisiteClues) > 0 {
			item["prerequisite_clues"] = clue.PrerequisiteClues
		}
		if clue.RecommendedNextAsk != "" {
			item["recommended_next_ask"] = clue.RecommendedNextAsk
		}
		out = append(out, item)
	}
	return out
}

func loadJSONSchemas() map[string]schemaEntry {
	definitions := []struct {
		name        string
		file        string
		target      string
		description string
	}{
		{SchemaScenarioQuestion, "schemas/scenario_question.schema.json", "SC-03 scenario content", "Scenario generation JSON Schema"},
		{SchemaScenarioContentPreview, "schemas/scenario_content_preview.schema.json", "CM-02 community preview", "Community preview JSON Schema"},
		{SchemaInterviewFeedback, "schemas/interview_feedback.schema.json", "IV-05 interview feedback", "Interview feedback JSON Schema"},
		{SchemaScenarioReply, "schemas/scenario_reply.schema.json", "DG-02 scenario reply", "Scenario reply JSON Schema"},
		{SchemaSensitiveCheck, "schemas/sensitive_check.schema.json", "SR-02 AI-03 sensitive detection", "Sensitive content detection JSON Schema"},
	}
	entries := make(map[string]schemaEntry, len(definitions))
	for _, definition := range definitions {
		raw, err := schemaFS.ReadFile(definition.file)
		entry := schemaEntry{
			raw: raw,
			info: JSONSchemaInfo{
				Name:        definition.name,
				SchemaName:  definition.name,
				Version:     "unknown",
				Task:        "",
				Description: definition.description,
				Target:      definition.target,
				Status:      "ok",
			},
			err: err,
		}
		if err == nil {
			if parseErr := json.Unmarshal(raw, &entry.schema); parseErr != nil {
				entry.err = parseErr
			} else {
				entry.info.Version = defaultString(entry.schema.Version, "1.0.0")
				entry.info.Task = entry.schema.Task
				entry.info.Description = defaultString(entry.schema.Description, definition.description)
			}
		}
		if entry.err != nil {
			entry.info.Status = "degraded"
		}
		entries[definition.name] = entry
	}
	return entries
}

func validateSchemaValue(value interface{}, schema jsonSchema, root jsonSchema, path string) error {
	if schema.Ref != "" {
		resolved, err := resolveSchemaRef(schema.Ref, root)
		if err != nil {
			return err
		}
		return validateSchemaValue(value, resolved, root, path)
	}

	switch schema.Type {
	case "object":
		object, ok := value.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%s must be object", path)
		}
		for _, field := range schema.Required {
			fieldValue, ok := object[field]
			if !ok || fieldValue == nil {
				return fmt.Errorf("%s.%s is required", path, field)
			}
		}
		if schema.AdditionalProperties != nil && !*schema.AdditionalProperties {
			for field := range object {
				if _, ok := schema.Properties[field]; !ok {
					return fmt.Errorf("%s.%s is not allowed", path, field)
				}
			}
		}
		requiredFields := make(map[string]bool, len(schema.Required))
		for _, field := range schema.Required {
			requiredFields[field] = true
		}
		for field, fieldSchema := range schema.Properties {
			if fieldValue, ok := object[field]; ok {
				if fieldValue == nil && !requiredFields[field] {
					continue
				}
				if err := validateSchemaValue(fieldValue, fieldSchema, root, path+"."+field); err != nil {
					return err
				}
			}
		}
	case "array":
		items, ok := value.([]interface{})
		if !ok {
			return fmt.Errorf("%s must be array", path)
		}
		if schema.MinItems != nil && len(items) < *schema.MinItems {
			return fmt.Errorf("%s requires at least %d item(s)", path, *schema.MinItems)
		}
		if schema.Items != nil {
			for index, item := range items {
				if err := validateSchemaValue(item, *schema.Items, root, fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be string", path)
		}
		if schema.MinLength != nil && len([]rune(strings.TrimSpace(text))) < *schema.MinLength {
			return fmt.Errorf("%s must not be empty", path)
		}
		if len(schema.Enum) > 0 && !oneOf(text, schema.Enum...) {
			return fmt.Errorf("%s must be one of %s", path, strings.Join(schema.Enum, ", "))
		}
		if schema.Pattern != "" {
			matched, err := regexp.MatchString(schema.Pattern, text)
			if err != nil {
				return fmt.Errorf("%s has invalid schema pattern: %w", path, err)
			}
			if !matched {
				return fmt.Errorf("%s must match pattern %s", path, schema.Pattern)
			}
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be boolean", path)
		}
	case "number", "integer":
		number, ok := schemaNumber(value)
		if !ok {
			return fmt.Errorf("%s must be %s", path, schema.Type)
		}
		if schema.Type == "integer" && number != float64(int64(number)) {
			return fmt.Errorf("%s must be integer", path)
		}
		if schema.Minimum != nil && number < *schema.Minimum {
			return fmt.Errorf("%s must be >= %v", path, *schema.Minimum)
		}
		if schema.Maximum != nil && number > *schema.Maximum {
			return fmt.Errorf("%s must be <= %v", path, *schema.Maximum)
		}
	case "":
		return nil
	default:
		return fmt.Errorf("%s uses unsupported schema type %q", path, schema.Type)
	}
	return nil
}

func schemaNumber(value interface{}) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case int32:
		return float64(number), true
	default:
		return 0, false
	}
}

func resolveSchemaRef(ref string, root jsonSchema) (jsonSchema, error) {
	const prefix = "#/$defs/"
	if !strings.HasPrefix(ref, prefix) {
		return jsonSchema{}, fmt.Errorf("unsupported schema ref %q", ref)
	}
	name := strings.TrimPrefix(ref, prefix)
	schema, ok := root.Defs[name]
	if !ok {
		return jsonSchema{}, fmt.Errorf("schema ref %q not found", ref)
	}
	return schema, nil
}
