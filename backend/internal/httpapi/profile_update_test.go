package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func TestProfileUpdatePersistsTargetRole(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPut, "/api/v1/users/me/profile", token, map[string]any{
		"target_level":      "senior",
		"target_role":       "后端开发工程师",
		"preferred_domains": []string{"database", "cache"},
		"resume_summary":    "后端开发工程师，具备三年 Java 服务开发经验，熟悉 MySQL、Redis、Docker 和消息队列，参与过高并发系统建设。",
		"project_summary":   "负责订单平台重构与缓存一致性治理，将接口延迟降低 30%，并补齐监控告警、发布验证和回滚流程。",
	})
	if status != http.StatusOK {
		t.Fatalf("profile update status=%d message=%s", status, env.Message)
	}
	var updated domain.User
	mustDecodeData(t, env, &updated)
	if updated.Profile.TargetRole != "后端开发工程师" {
		t.Fatalf("expected target_role to persist, got %+v", updated.Profile)
	}
	if updated.Profile.TargetLevel != "senior" {
		t.Fatalf("expected target_level to persist, got %+v", updated.Profile)
	}
}

func TestProfileUpdateRejectsInvalidResumeWithoutOverwritingExistingContent(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	validResume := "后端开发工程师，具备三年 Java 服务开发经验，熟悉 MySQL、Redis、Docker 和消息队列，参与过高并发系统建设。"
	validProject := "负责订单平台重构与缓存一致性治理，将接口延迟降低 30%，并补齐监控告警、发布验证和回滚流程。"

	status, env := requestJSON(t, handler, http.MethodPut, "/api/v1/users/me/profile", token, map[string]any{
		"resume_summary":  validResume,
		"project_summary": validProject,
	})
	if status != http.StatusOK {
		t.Fatalf("initial profile update status=%d message=%s", status, env.Message)
	}
	status, env = requestJSON(t, handler, http.MethodPut, "/api/v1/users/me/profile", token, map[string]any{
		"resume_summary":  strings.Repeat("哈", 80),
		"project_summary": "",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("invalid profile update status=%d message=%s", status, env.Message)
	}

	user, ok := dataStore.FindUserByIdentifier("demo")
	if !ok {
		t.Fatal("demo user not found")
	}
	if user.Profile.ResumeSummary != validResume || user.Profile.ProjectSummary != validProject {
		t.Fatalf("invalid update overwrote profile: %+v", user.Profile)
	}
}
