package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestNoopLimiterAlwaysAllows(t *testing.T) {
	limiter := NewNoopLimiter()
	if limiter.Enabled() {
		t.Fatal("noop limiter should be disabled")
	}
	if !limiter.Allow(context.Background(), "key", 1, time.Minute) {
		t.Fatal("noop limiter should allow")
	}
}
