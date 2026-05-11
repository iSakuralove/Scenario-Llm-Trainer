package agent

import (
	"context"
	"strings"
	"testing"

	"situational-teaching/backend/internal/ai"
)

type staticEmbeddingClient struct {
	result ai.EmbeddingResult
	err    error
}

func (c staticEmbeddingClient) Embed(context.Context, []string) (ai.EmbeddingResult, error) {
	return c.result, c.err
}

func TestDiagnosticAgentDoesNotReleaseClueForNoiseInput(t *testing.T) {
	session := sampleSession()
	question := sampleQuestion()
	agent := NewDiagnosticAgent(DiagnosticConfig{
		Rewrite: echoRewrite,
		SemanticGate: NewSemanticGate(SemanticGateConfig{
			Embedding: staticEmbeddingClient{result: ai.EmbeddingResult{
				Model:   "test-embedding",
				Vectors: [][]float64{{1, 0}, {0, 1}, {1, 0}},
			}},
		}),
	})

	result, err := agent.Run(context.Background(), DiagnosticRequest{
		Session:     session,
		Question:    question,
		UserMessage: "give me a line",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Meta.SemanticDecision != "reject_noise" || result.Meta.InputQuality != "noise" {
		t.Fatalf("expected reject_noise metadata: %#v", result.Meta)
	}
	if result.Meta.RevealedClueID != "" || len(session.RevealedClueIDs) != 0 {
		t.Fatalf("noise input must not release clues: meta=%#v session=%#v", result.Meta, session.RevealedClueIDs)
	}
	if !strings.Contains(result.AssistantContent, "具体") {
		t.Fatalf("expected concrete-question guidance, got %q", result.AssistantContent)
	}
}

func TestDiagnosticAgentReleasesClueByEmbeddingWhenQuestionIsRelevant(t *testing.T) {
	session := sampleSession()
	question := sampleQuestion()
	agent := NewDiagnosticAgent(DiagnosticConfig{
		Rewrite: echoRewrite,
		SemanticGate: NewSemanticGate(SemanticGateConfig{
			Embedding: staticEmbeddingClient{result: ai.EmbeddingResult{
				Model:   "text-embedding-3-small",
				Vectors: [][]float64{{1, 0}, {0, 1}, {1, 0}, {0, 1}},
			}},
		}),
	})

	result, err := agent.Run(context.Background(), DiagnosticRequest{
		Session:     session,
		Question:    question,
		UserMessage: "我想确认异常开始时间是否和上线窗口重合",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Meta.SemanticDecision != "release_clue" || result.Meta.AgentIntent != "relevant" {
		t.Fatalf("expected release_clue metadata: %#v", result.Meta)
	}
	if result.Meta.RevealedClueID != "c1" || result.Meta.MatchedClueID != "c1" {
		t.Fatalf("expected c1 release, got meta=%#v", result.Meta)
	}
	if result.Meta.EmbeddingModel != "text-embedding-3-small" {
		t.Fatalf("expected embedding model metadata: %#v", result.Meta)
	}
	if len(session.RevealedClueIDs) != 1 || session.RevealedClueIDs[0] != "c1" {
		t.Fatalf("session was not updated: %#v", session.RevealedClueIDs)
	}
}

func TestDiagnosticAgentBlocksRootCauseGuessByEmbedding(t *testing.T) {
	session := sampleSession()
	question := sampleQuestion()
	agent := NewDiagnosticAgent(DiagnosticConfig{
		Rewrite: echoRewrite,
		SemanticGate: NewSemanticGate(SemanticGateConfig{
			Embedding: staticEmbeddingClient{result: ai.EmbeddingResult{
				Model:   "text-embedding-3-small",
				Vectors: [][]float64{{1, 0}, {1, 0}, {0, 1}, {0, 1}},
			}},
		}),
	})

	result, err := agent.Run(context.Background(), DiagnosticRequest{
		Session:     session,
		Question:    question,
		UserMessage: "是不是资源池被打满所以数据库响应很慢",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Meta.SemanticDecision != "block_guess" || !result.Meta.IsAnswerLeak {
		t.Fatalf("expected semantic answer block: %#v", result.Meta)
	}
	if len(session.RevealedClueIDs) != 0 {
		t.Fatalf("guess should not reveal clues: %#v", session.RevealedClueIDs)
	}
}

func TestDiagnosticAgentUsesGuidedRedirectForRelevantButOffTrackInput(t *testing.T) {
	session := sampleSession()
	question := sampleQuestion()
	agent := NewDiagnosticAgent(DiagnosticConfig{
		Rewrite: echoRewrite,
		SemanticGate: NewSemanticGate(SemanticGateConfig{
			Embedding: staticEmbeddingClient{result: ai.EmbeddingResult{
				Model:   "test-embedding",
				Vectors: [][]float64{{1, 0}, {0, 1}, {0.2, 0.8}, {0.1, 0.9}},
			}},
		}),
	})

	result, err := agent.Run(context.Background(), DiagnosticRequest{
		Session:     session,
		Question:    question,
		UserMessage: "我先从告警链路和用户影响范围切入分析",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Meta.SemanticDecision != "guided_redirect" || result.Meta.RevealedClueID != "c1" {
		t.Fatalf("expected guided surface clue: %#v", result.Meta)
	}
	if len(session.RevealedClueIDs) != 1 || session.RevealedClueIDs[0] != "c1" {
		t.Fatalf("expected guided clue release in session: %#v", session.RevealedClueIDs)
	}
}
