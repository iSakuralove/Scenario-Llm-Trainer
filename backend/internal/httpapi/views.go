package httpapi

import (
	"fmt"
	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
	"time"
)

func hasAnyRole(user *domain.User, roles ...string) bool {
	if user == nil {
		return false
	}
	for _, role := range roles {
		if user.Role == role {
			return true
		}
	}
	return false
}
func canViewScenario(question *domain.ScenarioQuestion, user *domain.User) bool {
	if question.Status == "active" {
		return true
	}
	if user == nil {
		return false
	}
	return hasAnyRole(user, domain.RoleInstructor, domain.RoleAdmin) || user.ID == question.CreatedBy
}
func canViewAIJob(job *domain.AIJob, user *domain.User) bool {
	if job == nil || user == nil {
		return false
	}
	return user.Role == domain.RoleAdmin || job.UserID == user.ID
}
func scenarioPublicView(question *domain.ScenarioQuestion) domain.ScenarioQuestionView {
	content := sanitizeScenarioContent(question.Content)
	preparedQuestion := *question
	preparedQuestion.Title = ai.SanitizeFields(question.Title)
	preparedQuestion.Description = ai.SanitizeFields(question.Description)
	preparedQuestion.Tags = sanitizeTextSlice(question.Tags)
	if content.PublicScenario != nil {
		publicScenario := sanitizePublicScenario(*content.PublicScenario)
		content = domain.ScenarioContent{
			ModelVersion:   content.ModelVersion,
			PublicScenario: &publicScenario,
		}
		return scenarioQuestionViewFrom(question, preparedQuestion.Title, preparedQuestion.Description, preparedQuestion.Tags, content, true)
	}
	preparedQuestion.Content = content
	content = ai.PrepareScenarioContent(content, preparedQuestion)
	content.RootCause = ""
	content.RootCauseKeywords = nil
	content.KeyEvidence = nil
	content.StandardProcedure = nil
	content.RevealStrategy = domain.RevealStrategy{}
	return scenarioQuestionViewFrom(question, preparedQuestion.Title, preparedQuestion.Description, preparedQuestion.Tags, content, true)
}

func sanitizePublicScenario(scenario domain.PublicScenario) domain.PublicScenario {
	scenario.Title = ai.SanitizeFields(scenario.Title)
	scenario.Description = ai.SanitizeFields(scenario.Description)
	scenario.Environment = ai.SanitizeFields(scenario.Environment)
	scenario.InitialSymptoms = sanitizeTextSlice(scenario.InitialSymptoms)
	scenario.ArchitectureDiagram = ai.SanitizeFields(scenario.ArchitectureDiagram)
	return scenario
}
func scenarioFullView(question *domain.ScenarioQuestion) domain.ScenarioQuestionView {
	content := ai.PrepareScenarioContent(question.Content, *question)
	return scenarioQuestionViewFrom(question, question.Title, question.Description, append([]string{}, question.Tags...), content, false)
}
func scenarioDetailView(question *domain.ScenarioQuestion, user *domain.User) domain.ScenarioQuestionView {
	if canViewFullScenario(question, user) {
		return scenarioFullView(question)
	}
	return scenarioPublicView(question)
}
func canViewFullScenario(question *domain.ScenarioQuestion, user *domain.User) bool {
	if question == nil || user == nil {
		return false
	}
	return user.Role == domain.RoleAdmin || user.Role == domain.RoleInstructor || user.ID == question.CreatedBy
}
func scenarioView(question *domain.ScenarioQuestion, user *domain.User) domain.ScenarioQuestionView {
	return scenarioDetailView(question, user)
}

type scenarioSessionResponse struct {
	ID               string                      `json:"id"`
	UserID           string                      `json:"user_id"`
	QuestionID       string                      `json:"question_id"`
	Status           string                      `json:"status"`
	CurrentTurn      int                         `json:"current_turn"`
	MaxTurns         int                         `json:"max_turns"`
	RevealedClueIDs  []string                    `json:"revealed_clue_ids"`
	UserAnswer       string                      `json:"user_answer,omitempty"`
	EvaluationResult *domain.ScenarioEvaluation  `json:"evaluation_result,omitempty"`
	Score            *domain.ScenarioScore       `json:"score,omitempty"`
	QuestionSnapshot domain.ScenarioQuestionView `json:"question_snapshot"`
	StateRevision    int                         `json:"state_revision"`
	StartedAt        time.Time                   `json:"started_at"`
	LastActiveAt     time.Time                   `json:"last_active_at"`
	EndedAt          *time.Time                  `json:"ended_at,omitempty"`
}

func scenarioSessionView(session *domain.ScenarioSession) scenarioSessionResponse {
	if session == nil {
		return scenarioSessionResponse{}
	}
	return scenarioSessionResponse{
		ID:               session.ID,
		UserID:           session.UserID,
		QuestionID:       session.QuestionID,
		Status:           session.Status,
		CurrentTurn:      session.CurrentTurn,
		MaxTurns:         session.MaxTurns,
		RevealedClueIDs:  append([]string{}, session.RevealedClueIDs...),
		UserAnswer:       session.UserAnswer,
		EvaluationResult: session.EvaluationResult,
		Score:            session.Score,
		QuestionSnapshot: scenarioPublicView(&session.QuestionSnapshot),
		StateRevision:    session.StateRevision,
		StartedAt:        session.StartedAt,
		LastActiveAt:     session.LastActiveAt,
		EndedAt:          session.EndedAt,
	}
}
func scenarioSessionViews(sessions []domain.ScenarioSession) []scenarioSessionResponse {
	views := make([]scenarioSessionResponse, 0, len(sessions))
	for i := range sessions {
		views = append(views, scenarioSessionView(&sessions[i]))
	}
	return views
}
func scenarioQuestionViewFrom(question *domain.ScenarioQuestion, title, description string, tags []string, content domain.ScenarioContent, isSanitized bool) domain.ScenarioQuestionView {
	return domain.ScenarioQuestionView{
		ID: question.ID, Title: title, Description: description,
		Domain: question.Domain, Difficulty: question.Difficulty, ScenarioType: question.ScenarioType,
		Tags: tags, Content: content, Status: question.Status, Source: question.Source,
		CreatedBy: question.CreatedBy, Version: question.Version, CreatedAt: question.CreatedAt,
		UpdatedAt: question.UpdatedAt, IsSanitized: isSanitized,
	}
}
func interviewQuestionView(question *domain.InterviewQuestion, user *domain.User) *domain.InterviewQuestion {
	if question == nil {
		return nil
	}
	copy := *question
	if user == nil || user.Role == domain.RoleStudent {
		copy.ReferenceAnswer = ""
		copy.ReferenceKeywords = nil
	}
	return &copy
}
func generatedScenario(domainName, difficulty, scenarioType string, tags []string, userID string) domain.ScenarioQuestion {
	if domainName == "" {
		domainName = "database"
	}
	if difficulty == "" {
		difficulty = "L2"
	}
	if scenarioType == "" {
		scenarioType = "troubleshooting"
	}
	if len(tags) == 0 {
		tags = []string{"AI生成", domainName}
	}
	title := fmt.Sprintf("%s 方向 %s 演示情景题", domainName, difficulty)
	root := "配置变更后缺少必要验证，导致核心链路出现异常。"
	return domain.ScenarioQuestion{
		Title:        title,
		Description:  "这是一道由 MVP mock LLM Router 生成的演示题。用户需要通过日志、指标、变更和依赖链路逐步收集线索。",
		Domain:       domainName,
		Difficulty:   difficulty,
		ScenarioType: scenarioType,
		Tags:         tags,
		Status:       "active",
		Source:       "llm_generated",
		CreatedBy:    userID,
		Version:      1,
		Content: domain.ScenarioContent{
			RootCause:           root,
			RootCauseKeywords:   []string{"配置变更", "验证", "核心链路"},
			KeyEvidence:         []string{"异常开始时间与配置发布时间一致", "回滚配置后指标恢复", "下游服务本身无异常"},
			StandardProcedure:   []string{"确认异常窗口", "聚合日志与指标", "比对最近变更", "验证依赖链路", "灰度回滚并观察"},
			ArchitectureDiagram: "graph TD\nA[Client] --> B[API]\nB --> C[Core Service]\nC --> D[Dependency]\nC --> E[Config Center]",
			ReferenceLinks:      []string{"变更管理", "故障复盘"},
			RevealStrategy: domain.RevealStrategy{
				SurfaceClues: []domain.Clue{
					{ClueID: "c1", TriggerKeywords: []string{"日志", "时间", "窗口"}, Content: "异常开始时间与一次配置发布高度重合。", RecommendedNextAsk: "继续询问变更内容。"},
					{ClueID: "c2", TriggerKeywords: []string{"指标", "监控", "依赖"}, Content: "下游依赖服务自身指标正常，异常主要集中在核心服务调用分支。", RecommendedNextAsk: "继续询问配置或回滚。"},
				},
				DeepClues: []domain.Clue{
					{ClueID: "c3", TriggerKeywords: []string{"配置", "变更", "回滚"}, PrerequisiteClues: []string{"c1"}, Content: "灰度回滚配置后，错误率从 8% 降至 0.5%。", RecommendedNextAsk: "可以提交根因判断。"},
				},
				Distractors: []domain.Clue{
					{ClueID: "d1", TriggerKeywords: []string{"网络", "CPU"}, Content: "网络和 CPU 指标都在正常范围内。", IsDistractor: true},
				},
			},
		},
	}
}
func countValidRevealed(strategy domain.RevealStrategy, revealed []string) int {
	valid := map[string]bool{}
	for _, clue := range strategy.SurfaceClues {
		valid[clue.ClueID] = true
	}
	for _, clue := range strategy.DeepClues {
		valid[clue.ClueID] = true
	}
	count := 0
	for _, clueID := range revealed {
		if valid[clueID] {
			count++
		}
	}
	return count
}
func finalInterviewReport(evaluation domain.InterviewEvaluation) string {
	if evaluation.TotalScore >= 85 {
		return "整体表现优秀，能够覆盖关键定位路径，并具备较好的落地意识。"
	}
	if evaluation.TotalScore >= 70 {
		return "整体达到岗位要求，建议继续强化底层原理与应急取舍。"
	}
	return "当前回答还有明显缺口，建议围绕关键命令、验证路径和回滚方案进行专项练习。"
}
