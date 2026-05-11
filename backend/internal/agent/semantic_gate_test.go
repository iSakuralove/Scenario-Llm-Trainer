package agent

import (
	"context"
	"strings"
	"testing"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
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
	if !(strings.Contains(result.AssistantContent, "可验证") || strings.Contains(result.AssistantContent, "观察点")) {
		t.Fatalf("expected structured allowed content, got %q", result.AssistantContent)
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
	if !(strings.Contains(result.AssistantContent, "释放线索内容") || strings.Contains(result.AssistantContent, "命中")) {
		t.Fatalf("expected structured clue reply skeleton, got %q", result.AssistantContent)
	}
}

func TestDiagnosticAgentDoesNotTreatSameFocusNewEvidenceAsRepeat(t *testing.T) {
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
		Session:  session,
		Question: question,
		Messages: []domain.ScenarioMessage{
			{TurnNumber: 1, UserContent: "我先看应用日志有没有报错", AssistantContent: "暂未命中新线索。"},
		},
		UserMessage: "我想确认异常开始时间是否和上线窗口重合",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Meta.AgentIntent == "repeat_probe" {
		t.Fatalf("same-focus new evidence should not be treated as repeat: %#v", result.Meta)
	}
	if result.Meta.SemanticDecision != "release_clue" || result.Meta.RevealedClueID != "c1" {
		t.Fatalf("expected new evidence to release frontier clue: %#v", result.Meta)
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
	if !(strings.Contains(result.AssistantContent, "证据链") || strings.Contains(result.AssistantContent, "判断依据")) {
		t.Fatalf("expected structured anti-guess reply, got %q", result.AssistantContent)
	}
}

func TestDiagnosticAgentUsesGuidedRedirectForBroadProbeInput(t *testing.T) {
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
		UserMessage: "我先从日志、指标和变更整体看一下",
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
	if !(strings.Contains(result.AssistantContent, "基础观察") || strings.Contains(result.AssistantContent, "接近关键线索")) {
		t.Fatalf("expected structured guided redirect content, got %q", result.AssistantContent)
	}
}

func TestDiagnosticAgentDoesNotReleaseClueForOffTrackInput(t *testing.T) {
	session := sampleSession()
	question := sampleQuestion()
	session.NoNewClueStreak = 2
	agent := NewDiagnosticAgent(DiagnosticConfig{Rewrite: echoRewrite})

	result, err := agent.Run(context.Background(), DiagnosticRequest{
		Session:     session,
		Question:    question,
		UserMessage: "你tm傻逼吗？我在骂你还给我释放线索",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Meta.RevealedClueID != "" || len(session.RevealedClueIDs) != 0 {
		t.Fatalf("off-track input must not release clues: meta=%#v session=%#v", result.Meta, session.RevealedClueIDs)
	}
	if session.NoNewClueStreak != 2 {
		t.Fatalf("off-track input should not advance no-new-clue streak, got %d", session.NoNewClueStreak)
	}
	if strings.Contains(result.AssistantContent, "基础观察") {
		t.Fatalf("off-track input should not expose guided clue, got %q", result.AssistantContent)
	}
	if !(strings.Contains(result.AssistantContent, "跑偏") || strings.Contains(result.AssistantContent, "可验证")) {
		t.Fatalf("expected structured rephrase guidance, got %q", result.AssistantContent)
	}
}

func TestDiagnosticAgentUsesHumorousRedirectForChattyOffTopic(t *testing.T) {
	session := sampleSession()
	question := sampleQuestion()
	session.NoNewClueStreak = 2
	agent := NewDiagnosticAgent(DiagnosticConfig{Rewrite: echoRewrite})

	result, err := agent.Run(context.Background(), DiagnosticRequest{
		Session:     session,
		Question:    question,
		UserMessage: "哈哈哈哈，OpenAI 来了也得给我线索吧",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Meta.AgentIntent != "chatty_off_topic" || result.Meta.SemanticDecision != "humorous_redirect" {
		t.Fatalf("expected humorous chatty redirect metadata: %#v", result.Meta)
	}
	if result.Meta.RevealedClueID != "" || len(session.RevealedClueIDs) != 0 {
		t.Fatalf("chatty off-topic input must not release clues: meta=%#v session=%#v", result.Meta, session.RevealedClueIDs)
	}
	if session.NoNewClueStreak != 2 {
		t.Fatalf("chatty off-topic input should not advance no-new-clue streak, got %d", session.NoNewClueStreak)
	}
	if strings.Contains(result.AssistantContent, "OpenAI") || !(strings.Contains(result.AssistantContent, "主线") || strings.Contains(result.AssistantContent, "观察点")) {
		t.Fatalf("expected non-confrontational humorous redirect skeleton, got %q", result.AssistantContent)
	}
}
