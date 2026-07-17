package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func TestInterviewSubmitReturnsAgentTraceAndAudit(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	sessionID := createInterviewSessionForTest(t, handler, token)

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions/"+sessionID+"/submit", token, map[string]string{
		"type":    "text",
		"content": "我会先看慢查询日志，再用 EXPLAIN 验证索引覆盖，最后灰度补索引并准备回滚验证。",
	})
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("submit status=%d env=%+v", status, env)
	}

	var payload struct {
		Evaluation    domain.InterviewEvaluation `json:"evaluation"`
		SessionStatus string                     `json:"session_status"`
		Session       domain.InterviewSession    `json:"session"`
	}
	mustDecodeData(t, env, &payload)
	if payload.Evaluation.AgentTrace == nil {
		t.Fatal("expected interview evaluation to include agent trace")
	}
	if payload.Evaluation.AgentTrace.Agent != "interview_agent" {
		t.Fatalf("expected interview_agent trace, got %+v", payload.Evaluation.AgentTrace)
	}
	if payload.Evaluation.AgentTrace.ToolCount < 1 || len(payload.Evaluation.AgentTrace.Steps) < 1 {
		t.Fatalf("expected trace steps, got %+v", payload.Evaluation.AgentTrace)
	}

	traceText := mustInterviewTraceText(payload.Evaluation.AgentTrace)
	for _, forbidden := range []string{"reference_answer", "standard_procedure", "tool_args"} {
		if strings.Contains(strings.ToLower(traceText), forbidden) {
			t.Fatalf("trace should not leak forbidden term %q in %s", forbidden, traceText)
		}
	}

	foundAudit := false
	for _, event := range dataStore.ListAuditEvents(20) {
		if event.Action != "agent.interview_run" {
			continue
		}
		foundAudit = true
		if event.ResourceType != "interview_session" {
			t.Fatalf("unexpected audit resource type: %+v", event)
		}
		if event.Metadata["agent"] != "interview_agent" {
			t.Fatalf("unexpected audit metadata: %+v", event.Metadata)
		}
		if strings.TrimSpace(event.Metadata["tool_count"]) == "" {
			t.Fatalf("expected tool_count metadata: %+v", event.Metadata)
		}
	}
	if !foundAudit {
		t.Fatal("expected agent.interview_run audit event")
	}
}

func TestInterviewSubmitSSEExposesSafeAgentStages(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	sessionID := createInterviewSessionForTest(t, handler, token)

	body, err := json.Marshal(map[string]string{
		"type":    "text",
		"content": "我会先定位接口耗时和慢查询日志，再使用 EXPLAIN 和执行计划确认索引问题，最后给出灰度修复与回滚方案。",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/interviews/sessions/"+sessionID+"/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("sse status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw := rr.Body.String()
	for _, step := range []string{`"step":"received"`, `"step":"agent_intent"`, `"step":"agent_eval"`, `"step":"agent_retrieval"`, `"step":"agent_followup"`, `"step":"agent_reply"`, `"step":"agent_safety"`, `"step":"completed"`} {
		if !strings.Contains(raw, step) {
			t.Fatalf("expected SSE to include %s, got %s", step, raw)
		}
	}
	if !strings.Contains(raw, `"agent":"interview_agent"`) {
		t.Fatalf("expected finish payload to include interview agent trace, got %s", raw)
	}
	for _, forbidden := range []string{"reference_answer", "standard_procedure", "tool_args"} {
		if strings.Contains(strings.ToLower(raw), forbidden) {
			t.Fatalf("expected SSE output to hide %s, got %s", forbidden, raw)
		}
	}
}

func TestInterviewSubmitSSEShortCircuitsIrrelevantAnswer(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	sessionID := createInterviewSessionForTest(t, handler, token)

	body, err := json.Marshal(map[string]string{
		"type":    "text",
		"content": strings.Repeat("我只是在随便输入一些电影音乐旅行计划，和当前数据库面试题没有关系。", 8),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/interviews/sessions/"+sessionID+"/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("sse status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw := rr.Body.String()
	if !strings.Contains(raw, "请认真回答面试问题") {
		t.Fatalf("expected friendly guidance, got %s", raw)
	}
	for _, unexpected := range []string{"正在准备评分", "正在执行评分维度检查", "正在判断是否需要追问", "正在生成面试反馈", "总分"} {
		if strings.Contains(raw, unexpected) {
			t.Fatalf("irrelevant answer should not emit %q in SSE: %s", unexpected, raw)
		}
	}
}

func TestInterviewSubmitEndsAfterRepeatedIrrelevantAnswers(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	sessionID := createInterviewSessionForTest(t, handler, token)
	irrelevant := strings.Repeat("我今天想聊电影音乐和周末旅行安排，这些内容和当前技术面试题没有关系。", 8)

	for i := 1; i <= 3; i++ {
		status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions/"+sessionID+"/submit", token, map[string]string{
			"type":    "text",
			"content": irrelevant,
		})
		if status != http.StatusOK || env.Code != http.StatusOK {
			t.Fatalf("irrelevant attempt %d status=%d env=%+v", i, status, env)
		}
		var payload struct {
			Evaluation    domain.InterviewEvaluation `json:"evaluation"`
			SessionStatus string                     `json:"session_status"`
			Session       domain.InterviewSession    `json:"session"`
		}
		mustDecodeData(t, env, &payload)
		if payload.SessionStatus == "final_evaluated" {
			t.Fatalf("attempt %d should not finish interview: %+v", i, payload)
		}
		if !payload.Evaluation.FollowUpTriggered || !strings.Contains(payload.Evaluation.FollowUpQuestion, "请认真回答面试问题") {
			t.Fatalf("attempt %d should return friendly guidance: %+v", i, payload.Evaluation)
		}
	}

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions/"+sessionID+"/submit", token, map[string]string{
		"type":    "text",
		"content": irrelevant,
	})
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("fourth attempt status=%d env=%+v", status, env)
	}
	var finalPayload struct {
		Evaluation    domain.InterviewEvaluation `json:"evaluation"`
		SessionStatus string                     `json:"session_status"`
		Session       domain.InterviewSession    `json:"session"`
	}
	mustDecodeData(t, env, &finalPayload)
	if finalPayload.SessionStatus != "final_evaluated" {
		t.Fatalf("expected final_evaluated after fourth irrelevant answer: %+v", finalPayload)
	}
	if finalPayload.Session.FinalReport != "继续沉淀" || finalPayload.Evaluation.TotalScore != 0 || len(finalPayload.Evaluation.DimensionScores) != 0 {
		t.Fatalf("expected invalidated final evaluation without detailed score data: %+v", finalPayload)
	}
	if !strings.Contains(strings.Join(finalPayload.Evaluation.Deficiencies, " "), "面试官认为你还没有准备好") {
		t.Fatalf("expected not-ready feedback: %+v", finalPayload.Evaluation.Deficiencies)
	}

	reportStatus, reportEnv := requestJSON(t, handler, http.MethodGet, "/api/v1/interviews/sessions/"+sessionID+"/report", token, nil)
	if reportStatus != http.StatusOK || reportEnv.Code != http.StatusOK {
		t.Fatalf("report status=%d env=%+v", reportStatus, reportEnv)
	}
	var report struct {
		RadarData   []map[string]interface{} `json:"radar_data"`
		FinalScore  int                      `json:"final_score"`
		FinalReport string                   `json:"final_report"`
	}
	mustDecodeData(t, reportEnv, &report)
	if report.FinalReport != "继续沉淀" || report.FinalScore != 0 || len(report.RadarData) != 0 {
		t.Fatalf("expected invalidated report without radar data: %+v", report)
	}
}

func TestEvaluateIrrelevantInterviewAnswerAcceptsTechnicalChineseAnswer(t *testing.T) {
	question := &domain.InterviewQuestion{
		Title:             "load average 高但 CPU 不高怎么排查",
		Description:       "Linux 主机 load average 很高，但 CPU 使用率并不高。请说明可能原因、验证命令和处理策略。",
		ReferenceAnswer:   "应解释 load 与可运行/不可中断进程关系，使用 vmstat、iostat、ps 定位 IO wait 与 D 状态进程。",
		ReferenceKeywords: []string{"load", "D 状态", "IO wait", "vmstat", "iostat", "ps"},
	}
	// 第二轮追问常见：围绕验证步骤展开，不一定复述开场英文关键词。
	answer := "我会先确认 load average 是否持续偏高，然后用 vmstat 看 r 与 b 队列，再用 iostat 看磁盘利用率。若发现大量 D 状态进程，就定位对应文件系统或 NFS，并准备限流与扩容回滚。"
	decision := evaluateIrrelevantInterviewAnswer(question, answer, 0)
	if decision.Irrelevant {
		t.Fatalf("technical chinese answer should not be treated as irrelevant: %+v", decision)
	}
}

func TestEvaluateIrrelevantInterviewAnswerStillRejectsChitchat(t *testing.T) {
	question := &domain.InterviewQuestion{
		Title:             "如何定位 MySQL 慢查询",
		Description:       "线上接口突然变慢，你怀疑是 MySQL 查询问题。请说明你的定位路径、关键命令、可能修复方案和回滚考虑。",
		ReferenceAnswer:   "应从链路耗时、慢查询日志、EXPLAIN、索引覆盖、执行计划变化、灰度建索引与回滚方案等方面回答。",
		ReferenceKeywords: []string{"慢查询", "EXPLAIN", "索引", "执行计划", "回滚", "灰度"},
	}
	answer := strings.Repeat("我今天只想聊电影音乐旅行计划和周末逛街安排，这些内容和当前技术面试题没有关系。", 4)
	decision := evaluateIrrelevantInterviewAnswer(question, answer, 0)
	if !decision.Irrelevant {
		t.Fatalf("chitchat answer should still be treated as irrelevant: %+v", decision)
	}
}

func mustInterviewTraceText(trace *domain.AgentTrace) string {
	if trace == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(trace.RunID)
	builder.WriteString(trace.Agent)
	builder.WriteString(trace.Mode)
	for _, step := range trace.Steps {
		builder.WriteString(step.Name)
		builder.WriteString(step.Kind)
		builder.WriteString(step.Status)
		builder.WriteString(step.Summary)
		for key, value := range step.Metadata {
			builder.WriteString(key)
			builder.WriteString(value)
		}
	}
	return builder.String()
}
