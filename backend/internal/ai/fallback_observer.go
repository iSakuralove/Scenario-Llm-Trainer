package ai

import "context"

// FallbackEvent 描述一次 provider 回退切换（A 失败 → 即将尝试 B）。
type FallbackEvent struct {
	Task            string `json:"task"`
	FromProvider    string `json:"from_provider"`
	FromModel       string `json:"from_model,omitempty"`
	FromErrorType   string `json:"from_error_type,omitempty"`
	ToProvider      string `json:"to_provider"`
	ToModel         string `json:"to_model,omitempty"`
	AttemptIndex    int    `json:"attempt_index"`
	FailedLatencyMS int64  `json:"failed_latency_ms"`
}

// FallbackObserver 在 provider 切换瞬间被回调；实现方应快速返回（如仅更新 job 字段），
// 不要在回调里做网络请求或长耗时操作，避免拖慢回退链。
type FallbackObserver interface {
	OnProviderFallback(event FallbackEvent)
}

type fallbackObserverContextKey struct{}

// WithFallbackObserver 把回退观察者挂到 ctx 上，Router.call 在每次 provider 切换时取出并通知。
func WithFallbackObserver(ctx context.Context, observer FallbackObserver) context.Context {
	if ctx == nil || observer == nil {
		return ctx
	}
	return context.WithValue(ctx, fallbackObserverContextKey{}, observer)
}

// FallbackObserverFromContext 返回 ctx 上的观察者；没有时返回安全空实现。
func FallbackObserverFromContext(ctx context.Context) FallbackObserver {
	if ctx == nil {
		return noopFallbackObserver{}
	}
	if observer, ok := ctx.Value(fallbackObserverContextKey{}).(FallbackObserver); ok && observer != nil {
		return observer
	}
	return noopFallbackObserver{}
}

type noopFallbackObserver struct{}

func (noopFallbackObserver) OnProviderFallback(FallbackEvent) {}
