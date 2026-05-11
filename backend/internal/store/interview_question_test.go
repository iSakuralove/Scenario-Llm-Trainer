package store

import "testing"

func TestMemoryFindInterviewQuestionRequiresExactMatch(t *testing.T) {
	store := NewMemoryStore(func(password string) string { return "hash:" + password })

	question, ok := store.FindInterviewQuestion("database", "L3", "scenario_analysis")
	if !ok {
		t.Fatal("expected database L3 scenario interview question")
	}
	if question.Domain != "database" || question.Difficulty != "L3" || question.QuestionType != "scenario_analysis" {
		t.Fatalf("expected exact database L3 scenario question, got domain=%q difficulty=%q type=%q", question.Domain, question.Difficulty, question.QuestionType)
	}

	matches := []struct {
		domain       string
		difficulty   string
		questionType string
	}{
		{domain: "security", difficulty: "L4", questionType: "scenario_analysis"},
		{domain: "cloud-native", difficulty: "L4", questionType: "scenario_analysis"},
		{domain: "architecture", difficulty: "L5", questionType: "principle"},
	}

	for _, tc := range matches {
		question, ok := store.FindInterviewQuestion(tc.domain, tc.difficulty, tc.questionType)
		if !ok {
			t.Fatalf("expected question for domain=%s difficulty=%s type=%s", tc.domain, tc.difficulty, tc.questionType)
		}
		if question.Domain != tc.domain || question.Difficulty != tc.difficulty || question.QuestionType != tc.questionType {
			t.Fatalf("unexpected track match for %s/%s/%s: %+v", tc.domain, tc.difficulty, tc.questionType, question)
		}
	}

	if question, ok := store.FindInterviewQuestion("security", "L5", "scenario_analysis"); ok {
		t.Fatalf("expected no fallback for unsupported interview track, got %q", question.ID)
	}
}

func TestMemoryFindInterviewQuestionRejectsEmptyTrackFields(t *testing.T) {
	store := NewMemoryStore(func(password string) string { return "hash:" + password })

	cases := []struct {
		name         string
		domain       string
		difficulty   string
		questionType string
	}{
		{name: "empty domain", domain: "", difficulty: "L3", questionType: "scenario_analysis"},
		{name: "empty difficulty", domain: "database", difficulty: "", questionType: "scenario_analysis"},
		{name: "empty question type", domain: "database", difficulty: "L3", questionType: ""},
		{name: "whitespace domain", domain: "  ", difficulty: "L3", questionType: "scenario_analysis"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if question, ok := store.FindInterviewQuestion(tc.domain, tc.difficulty, tc.questionType); ok {
				t.Fatalf("expected empty track fields to return no question, got %q", question.ID)
			}
		})
	}
}
