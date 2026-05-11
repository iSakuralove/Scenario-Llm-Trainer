package ai

import (
	"fmt"
	"sync"
)

type providerRateLimiter struct {
	mu       sync.Mutex
	limit    int64
	inFlight map[string]int64
	limited  map[string]bool
}

func newProviderRateLimiter(limit int64) *providerRateLimiter {
	return &providerRateLimiter{
		limit:    limit,
		inFlight: map[string]int64{},
		limited:  map[string]bool{},
	}
}

func (l *providerRateLimiter) begin(provider string) (func(), ProviderRateLimit, error) {
	if l == nil {
		return func() {}, ProviderRateLimit{Provider: provider, Status: "ok"}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	state := ProviderRateLimit{Provider: provider, Status: "ok", Limit: l.limit, InFlight: l.inFlight[provider]}
	if l.limit <= 0 || l.inFlight[provider] >= l.limit {
		state.Status = "limited"
		l.limited[provider] = true
		return func() {}, state, fmt.Errorf("provider rate limit exceeded: %s", provider)
	}
	l.inFlight[provider]++
	state.InFlight = l.inFlight[provider]
	l.limited[provider] = false
	release := func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.inFlight[provider] > 0 {
			l.inFlight[provider]--
		}
	}
	return release, state, nil
}

func (l *providerRateLimiter) snapshot(provider string) ProviderRateLimit {
	if l == nil {
		return ProviderRateLimit{Provider: provider, Status: "ok"}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	status := "ok"
	if l.limited[provider] {
		status = "limited"
	}
	return ProviderRateLimit{Provider: provider, Status: status, Limit: l.limit, InFlight: l.inFlight[provider]}
}
