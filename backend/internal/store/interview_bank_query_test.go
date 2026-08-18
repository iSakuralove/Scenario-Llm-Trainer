package store

import (
	"testing"

	"situational-teaching/backend/internal/domain"
)

func TestInterviewKnowledgeAtomMatchesQuery(t *testing.T) {
	atom := domain.InterviewKnowledgeAtom{
		ID:       "java-hashmap-1",
		Title:    "HashMap 扩容机制",
		Subject:  "请解释 HashMap 扩容",
		Domain:   "java",
		Category: "java",
		Tags:     []string{"hashmap", "collections"},
	}
	if !interviewKnowledgeAtomMatchesQuery(atom, "hashmap") {
		t.Fatal("expected tag/id query hit")
	}
	if !interviewKnowledgeAtomMatchesQuery(atom, "扩容") {
		t.Fatal("expected title query hit")
	}
	if interviewKnowledgeAtomMatchesQuery(atom, "redis") {
		t.Fatal("expected miss")
	}
	if !interviewKnowledgeAtomMatchesFilter(atom, domain.InterviewKnowledgeAtomFilter{Query: "java", Category: "java"}) {
		t.Fatal("expected combined filter hit")
	}
}
