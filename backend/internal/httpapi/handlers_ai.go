package httpapi

import (
	"net/http"
	"situational-teaching/backend/internal/domain"
)

func (s *Server) handleAI(w http.ResponseWriter, r *http.Request, user *domain.User, suffix string) {
	parts := split(suffix)
	if len(parts) == 2 && parts[0] == "safety" && parts[1] == "check" && r.Method == http.MethodPost {
		var req struct {
			Text  string `json:"text"`
			Field string `json:"field"`
		}
		if !decode(w, r, &req) {
			return
		}
		writeOK(w, s.sensitiveCheck(r, user, req.Field, req.Text))
		return
	}
	if len(parts) == 2 && parts[0] == "jobs" && r.Method == http.MethodGet {
		job, ok := s.store.GetAIJob(parts[1])
		if !ok || !canViewAIJob(job, user) {
			writeError(w, http.StatusNotFound, "ai job not found")
			return
		}
		writeOK(w, s.aiJobPayload(job, user))
		return
	}
	if len(parts) == 3 && parts[0] == "jobs" && parts[2] == "events" && r.Method == http.MethodGet {
		job, ok := s.store.GetAIJob(parts[1])
		if !ok || !canViewAIJob(job, user) {
			writeError(w, http.StatusNotFound, "ai job not found")
			return
		}
		s.writeAIJobEvents(w, r, user, job.ID)
		return
	}
	if len(parts) == 3 && parts[0] == "jobs" && parts[2] == "cancel" && r.Method == http.MethodPost {
		job, ok := s.store.GetAIJob(parts[1])
		if !ok || !canViewAIJob(job, user) {
			writeError(w, http.StatusNotFound, "ai job not found")
			return
		}
		canceled, err := s.cancelAIJob(job)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeOK(w, s.aiJobPayload(canceled, user))
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}
