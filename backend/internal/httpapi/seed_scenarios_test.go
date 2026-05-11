package httpapi

import (
	"net/http"
	"testing"
	"time"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func TestStudentScenarioDetailHidesFixedSeedAnswerContent(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	question := dataStore.ListScenarios("", "", "")[0]
	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/scenarios/"+question.ID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("scenario detail status=%d message=%s", status, env.Message)
	}

	var view domain.ScenarioQuestionView
	mustDecodeData(t, env, &view)
	if !view.IsSanitized {
		t.Fatalf("expected public scenario view to be sanitized, got %+v", view)
	}
	if view.Content.RootCause != "" || len(view.Content.StandardProcedure) != 0 || len(view.Content.RevealStrategy.SurfaceClues) != 0 || len(view.Content.RevealStrategy.DeepClues) != 0 || len(view.Content.RevealStrategy.Distractors) != 0 {
		t.Fatalf("student view leaked protected content: %+v", view.Content)
	}
}
