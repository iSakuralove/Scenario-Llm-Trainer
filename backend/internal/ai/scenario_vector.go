package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"situational-teaching/backend/internal/domain"
)

const (
	VectorDocProblemContext = "problem_context"
	VectorDocRootCause      = "root_cause"
	VectorDocEvidence       = "evidence"
	VectorDocProcedureStep  = "procedure_step"
	VectorDocClue           = "clue"
	VectorDocDistractor     = "distractor"
)

type ScenarioVectorDocument struct {
	QuestionID     string            `json:"question_id"`
	SourceVersion  int               `json:"source_version"`
	DocType        string            `json:"doc_type"`
	DocKey         string            `json:"doc_key"`
	DocText        string            `json:"doc_text"`
	TextHash       string            `json:"text_hash"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	EmbeddingModel string            `json:"embedding_model,omitempty"`
	EmbeddingDim   int               `json:"embedding_dim,omitempty"`
	Vector         []float64         `json:"-"`
	Status         string            `json:"status"`
}

func BuildScenarioVectorDocuments(question domain.ScenarioQuestion) []ScenarioVectorDocument {
	if strings.TrimSpace(question.Status) != "active" || strings.TrimSpace(question.ID) == "" {
		return nil
	}
	docs := []ScenarioVectorDocument{}
	add := func(docType, key, text string, metadata map[string]string) {
		text = normalizeVectorText(text)
		if text == "" {
			return
		}
		docs = append(docs, ScenarioVectorDocument{
			QuestionID:    question.ID,
			SourceVersion: question.Version,
			DocType:       docType,
			DocKey:        key,
			DocText:       text,
			TextHash:      hashVectorText(text),
			Metadata:      metadata,
			Status:        question.Status,
		})
	}

	add(VectorDocProblemContext, "problem", strings.Join(nonEmptyStrings(
		"题目："+question.Title,
		"描述："+question.Description,
		"领域："+question.Domain,
		"难度："+question.Difficulty,
		"类型："+question.ScenarioType,
		"标签："+strings.Join(question.Tags, " "),
	), "\n"), map[string]string{"domain": question.Domain, "difficulty": question.Difficulty})
	add(VectorDocRootCause, "root", strings.Join(nonEmptyStrings(
		"根因："+question.Content.RootCause,
		"关键词："+strings.Join(question.Content.RootCauseKeywords, " "),
		"关键证据："+strings.Join(question.Content.KeyEvidence, "；"),
	), "\n"), map[string]string{"domain": question.Domain, "difficulty": question.Difficulty})

	for i, evidence := range question.Content.KeyEvidence {
		add(VectorDocEvidence, fmt.Sprintf("evidence:%d", i+1), evidence, map[string]string{"index": fmt.Sprintf("%d", i+1)})
	}
	for i, step := range question.Content.StandardProcedure {
		add(VectorDocProcedureStep, fmt.Sprintf("procedure:%d", i+1), step, map[string]string{"index": fmt.Sprintf("%d", i+1)})
	}
	for _, clue := range question.Content.RevealStrategy.SurfaceClues {
		add(VectorDocClue, "surface:"+clue.ClueID, clueVectorText(clue), clueMetadata(clue, "surface"))
	}
	for _, clue := range question.Content.RevealStrategy.DeepClues {
		add(VectorDocClue, "deep:"+clue.ClueID, clueVectorText(clue), clueMetadata(clue, "deep"))
	}
	for _, clue := range question.Content.RevealStrategy.Distractors {
		add(VectorDocDistractor, "distractor:"+clue.ClueID, clueVectorText(clue), clueMetadata(clue, "distractor"))
	}
	return docs
}

func clueVectorText(clue domain.Clue) string {
	return strings.Join(nonEmptyStrings(
		"线索："+clue.Content,
		"触发词："+strings.Join(clue.TriggerKeywords, " "),
		"前置线索："+strings.Join(clue.PrerequisiteClues, " "),
		"推荐追问："+clue.RecommendedNextAsk,
	), "\n")
}

func clueMetadata(clue domain.Clue, level string) map[string]string {
	return map[string]string{
		"clue_id":       clue.ClueID,
		"clue_level":    level,
		"is_distractor": fmt.Sprintf("%t", clue.IsDistractor || level == "distractor"),
	}
}

func normalizeVectorText(text string) string {
	fields := strings.Fields(strings.TrimSpace(text))
	return strings.Join(fields, " ")
}

func hashVectorText(text string) string {
	sum := sha256.Sum256([]byte(normalizeVectorText(text)))
	return hex.EncodeToString(sum[:])
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
