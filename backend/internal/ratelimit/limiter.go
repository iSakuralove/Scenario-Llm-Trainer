package ratelimit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type Limiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) bool
	Enabled() bool
}

type NoopLimiter struct{}

func NewNoopLimiter() NoopLimiter {
	return NoopLimiter{}
}

func (NoopLimiter) Allow(context.Context, string, int, time.Duration) bool {
	return true
}

func (NoopLimiter) Enabled() bool {
	return false
}

type RedisLimiter struct {
	client *redis.Client
}

func NewRedisLimiter(ctx context.Context, redisURL string) (*RedisLimiter, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return &RedisLimiter{client: client}, nil
}

func (r *RedisLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) bool {
	if r == nil || r.client == nil {
		return true
	}
	value, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		return true
	}
	if value == 1 {
		_ = r.client.Expire(ctx, key, window).Err()
	}
	return value <= int64(limit)
}

func (r *RedisLimiter) Enabled() bool {
	return r != nil && r.client != nil
}

func (r *RedisLimiter) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}
