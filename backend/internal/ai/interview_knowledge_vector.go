package ai

import (
	"fmt"
	"strings"

	"situational-teaching/backend/internal/domain"
)

const (
	InterviewKnowledgeVectorDocOverview  = "overview"
	InterviewKnowledgeVectorDocPrinciple = "principle"
	InterviewKnowledgeVectorDocPitfall   = "pitfall"
	InterviewKnowledgeVectorDocFollowUp  = "follow_up"
)

type InterviewKnowledgeVectorDocument struct {
	AtomID         string            `json:"atom_id"`
	AtomVersion    int               `json:"atom_version"`
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

func BuildInterviewKnowledgeVectorDocuments(atom domain.InterviewKnowledgeAtom) []InterviewKnowledgeVectorDocument {
	if strings.TrimSpace(atom.Status) != "published" || strings.TrimSpace(atom.ID) == "" {
		return nil
	}
	version := atom.CurrentVersion
	if version <= 0 {
		version = 1
	}
	baseMetadata := map[string]string{
		"domain":        atom.Domain,
		"difficulty":    atom.Difficulty,
		"category":      atom.Category,
		"question_role": atom.QuestionRole,
		"source_ref":    atom.SourceRef,
	}
	docs := []InterviewKnowledgeVectorDocument{}
	add := func(docType, key, text string, metadata map[string]string) {
		text = normalizeVectorText(text)
		if text == "" {
			return
		}
		merged := map[string]string{}
		for k, v := range baseMetadata {
			if strings.TrimSpace(v) != "" {
				merged[k] = v
			}
		}
		for k, v := range metadata {
			if strings.TrimSpace(v) != "" {
				merged[k] = v
			}
		}
		docs = append(docs, InterviewKnowledgeVectorDocument{
			AtomID:      atom.ID,
			AtomVersion: version,
			DocType:     docType,
			DocKey:      key,
			DocText:     text,
			TextHash:    hashVectorText(text),
			Metadata:    merged,
			Status:      "active",
		})
	}

	add(InterviewKnowledgeVectorDocOverview, "overview", strings.Join(nonEmptyStrings(
		"题目："+atom.Title,
		"考察点："+atom.Subject,
		"领域："+atom.Domain,
		"难度："+atom.Difficulty,
		"分类："+atom.Category,
		"题目角色："+atom.QuestionRole,
		"标签："+strings.Join(atom.Tags, " "),
		"来源："+atom.SourceRef,
	), "\n"), nil)
	for i, principle := range atom.Principles {
		add(InterviewKnowledgeVectorDocPrinciple, fmt.Sprintf("principle:%d", i+1), "答题原则："+principle, map[string]string{"index": fmt.Sprintf("%d", i+1)})
	}
	for i, pitfall := range atom.Pitfalls {
		add(InterviewKnowledgeVectorDocPitfall, fmt.Sprintf("pitfall:%d", i+1), "常见误区："+pitfall, map[string]string{"index": fmt.Sprintf("%d", i+1)})
	}
	for i, followUp := range atom.FollowUpPaths {
		add(InterviewKnowledgeVectorDocFollowUp, fmt.Sprintf("follow_up:%d", i+1), "追问路径："+followUp, map[string]string{"index": fmt.Sprintf("%d", i+1)})
	}
	return docs
}
