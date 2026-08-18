package store

import (
	"time"

	"situational-teaching/backend/internal/domain"
)

type Store interface {
	CreateUser(username, email, passwordHash string) (*domain.User, error)
	ListUsers() []domain.User
	FindUserByIdentifier(identifier string) (*domain.User, bool)
	GetUser(id string) (*domain.User, bool)
	UpdateUserRole(userID string, role string) (*domain.User, error)
	UpdateUserPassword(userID string, passwordHash string) (*domain.User, error)
	UpgradeUserPasswordHash(userID string, passwordHash string) (*domain.User, error)
	UpdateProfile(userID string, targetLevel string, preferredDomains []string) (*domain.User, error)
	SaveUserProfile(userID string, profile domain.UserProfile) (*domain.User, error)

	ListScenarios(domainName, difficulty, tag string) []domain.ScenarioQuestion
	GetScenario(id string) (*domain.ScenarioQuestion, bool)
	AddScenario(scenario domain.ScenarioQuestion) domain.ScenarioQuestion
	CreateScenarioSession(userID, questionID string) (*domain.ScenarioSession, error)
	GetScenarioSession(id string) (*domain.ScenarioSession, bool)
	SaveScenarioSession(session *domain.ScenarioSession)
	AddScenarioMessage(message domain.ScenarioMessage) domain.ScenarioMessage
	ListScenarioMessages(sessionID string) []domain.ScenarioMessage
	ListScenarioSessionsForUser(userID string) []domain.ScenarioSession
	GetScenarioAgentTurn(sessionID, requestID string) (domain.ScenarioAgentTurnRecord, bool)
	CommitScenarioAgentTurn(commit domain.ScenarioAgentTurnCommit) (domain.ScenarioAgentTurnCommitResult, error)

	FindInterviewQuestion(domainName, difficulty, questionType string) (*domain.InterviewQuestion, bool)
	GetInterviewQuestion(id string) (*domain.InterviewQuestion, bool)
	CreateInterviewSession(userID string, question *domain.InterviewQuestion) *domain.InterviewSession
	GetInterviewSession(id string) (*domain.InterviewSession, bool)
	SaveInterviewSession(session *domain.InterviewSession)
	ListInterviewSessionsForUser(userID string) []domain.InterviewSession
	DeleteInterviewSession(id string) bool
	SaveInterviewKnowledgeAtomVersioned(atom domain.InterviewKnowledgeAtom, versionType, adminID, changeNote string) (domain.InterviewKnowledgeAtom, domain.InterviewKnowledgeAtomVersion, error)
	GetInterviewKnowledgeAtom(id string) (*domain.InterviewKnowledgeAtom, bool)
	ListInterviewKnowledgeAtoms(filter domain.InterviewKnowledgeAtomFilter) []domain.InterviewKnowledgeAtom
	ListInterviewKnowledgeAtomVersions(atomID string) []domain.InterviewKnowledgeAtomVersion
	UpdateInterviewKnowledgeAtomIndexStatus(atomID, vectorStatus string, lastIndexedAt *time.Time) (domain.InterviewKnowledgeAtom, error)
	SaveInterviewKnowledgeBatch(batch domain.InterviewKnowledgeBatch) domain.InterviewKnowledgeBatch
	ListInterviewKnowledgeBatches(limit int) []domain.InterviewKnowledgeBatch
	InterviewKnowledgeSummary() domain.InterviewKnowledgeSummary
	SaveInterviewRetrievalLog(log domain.InterviewRetrievalLog) domain.InterviewRetrievalLog
	ListInterviewRetrievalLogs(filter domain.InterviewRetrievalLogFilter) []domain.InterviewRetrievalLog
	InterviewRetrievalAnalytics(filter domain.InterviewRetrievalLogFilter) domain.InterviewRetrievalAnalytics
	CreateInterviewBankOpsAction(action domain.InterviewBankOpsAction) (domain.InterviewBankOpsAction, error)
	GetInterviewBankOpsAction(id string) (*domain.InterviewBankOpsAction, bool)
	UpdateInterviewBankOpsActionStatus(actionID, nextStatus, note, adminID string) (domain.InterviewBankOpsAction, domain.InterviewBankOpsActionHistoryEntry, error)
	ListInterviewBankOpsActionHistory(actionID string) []domain.InterviewBankOpsActionHistoryEntry
	ListInterviewBankOpsActions(filter domain.InterviewBankOpsActionFilter) []domain.InterviewBankOpsAction

	AddCommunityPost(post domain.CommunityPost) domain.CommunityPost
	GetCommunityPost(id string) (*domain.CommunityPost, bool)
	SaveCommunityPost(post *domain.CommunityPost) domain.CommunityPost
	ListCommunityPosts() []domain.CommunityPost
	DeleteCommunityPost(id string) bool

	CreateAsset(asset domain.Asset) (domain.Asset, error)
	GetAsset(id string) (*domain.Asset, bool)
	ListAssetsForUser(userID string) []domain.Asset
	DeleteAsset(id string) bool

	CreateAIJob(job domain.AIJob) (domain.AIJob, error)
	GetAIJob(id string) (*domain.AIJob, bool)
	SaveAIJob(job *domain.AIJob) (domain.AIJob, error)
	ListAIJobs(limit int) []domain.AIJob
	CountAIJobs() int

	ListPromptTemplates() []domain.PromptTemplate
	GetPromptTemplate(name string) (*domain.PromptTemplate, bool)
	SavePromptTemplate(template domain.PromptTemplate) (domain.PromptTemplate, error)
	GetAIConfig() domain.AIConfig
	SaveAIConfig(config domain.AIConfig) (domain.AIConfig, error)
	RecordAuditEvent(event domain.AuditEvent) domain.AuditEvent
	ListAuditEvents(limit int) []domain.AuditEvent

	RecordScenarioScore(userID, domainName string, score int)
	RecordInterviewScore(userID, domainName string, score int)
}

type VectorStoreProvider interface {
	VectorStore() VectorStore
}
