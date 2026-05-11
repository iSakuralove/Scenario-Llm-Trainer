package ai

import (
	"sync"
	"time"
)

type providerHealthStore struct {
	mu       sync.Mutex
	statuses map[string]ProviderHealth
}

func newProviderHealthStore() *providerHealthStore {
	return &providerHealthStore{statuses: map[string]ProviderHealth{}}
}

func (s *providerHealthStore) record(provider string, err error) ProviderHealth {
	if s == nil {
		return ProviderHealth{Provider: provider, Status: "unknown", LastCheckedAt: time.Now()}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	current := s.statuses[provider]
	current.Provider = provider
	current.LastCheckedAt = now
	if err == nil {
		current.Status = "ok"
		current.ConsecutiveFailures = 0
		current.LastErrorType = ""
		current.LastError = ""
	} else {
		current.Status = "degraded"
		current.ConsecutiveFailures++
		current.LastErrorType = classifyRouterError(err)
		current.LastError = sanitizeErrorMessage(err.Error())
	}
	s.statuses[provider] = current
	return current
}

func (s *providerHealthStore) snapshot(provider string) ProviderHealth {
	if s == nil {
		return ProviderHealth{Provider: provider, Status: "unknown"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.statuses[provider]
	if current.Provider == "" {
		current.Provider = provider
		current.Status = "unknown"
	}
	return current
}

func (s *providerHealthStore) snapshotAll() map[string]ProviderHealth {
	out := map[string]ProviderHealth{}
	if s == nil {
		return out
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for provider, health := range s.statuses {
		out[provider] = health
	}
	return out
}
