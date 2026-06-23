package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Limiter struct {
	rdb redis.Cmdable
}

type Config struct {
	SessionPerIPPerMin  int
	ReportPerIPPerMin   int
	GlobalSessionPerSec int
}

func New(rdb redis.Cmdable) *Limiter {
	return &Limiter{rdb: rdb}
}

// AllowSession checks session creation rate per IP (fixed window, 1 min).
func (l *Limiter) AllowSession(ctx context.Context, ip string, cfg Config) error {
	if cfg.SessionPerIPPerMin <= 0 {
		return nil
	}
	key := fmt.Sprintf("rl:session:%s", ip)
	return l.checkFixedWindow(ctx, key, cfg.SessionPerIPPerMin, 60*time.Second)
}

// AllowReport checks report rate per IP.
func (l *Limiter) AllowReport(ctx context.Context, ip string, cfg Config) error {
	if cfg.ReportPerIPPerMin <= 0 {
		return nil
	}
	key := fmt.Sprintf("rl:report:%s", ip)
	return l.checkFixedWindow(ctx, key, cfg.ReportPerIPPerMin, 60*time.Second)
}

// AllowGlobal checks global session creation rate.
func (l *Limiter) AllowGlobal(ctx context.Context, cfg Config) error {
	if cfg.GlobalSessionPerSec <= 0 {
		return nil
	}
	return l.checkFixedWindow(ctx, "rl:global:session", cfg.GlobalSessionPerSec, 1*time.Second)
}

func (l *Limiter) checkFixedWindow(ctx context.Context, key string, limit int, window time.Duration) error {
	n, err := l.rdb.Incr(ctx, key).Result()
	if err != nil {
		return nil // Redis down → allow through
	}
	if n == 1 {
		l.rdb.Expire(ctx, key, window)
	}
	if n > int64(limit) {
		return fmt.Errorf("rate limit exceeded")
	}
	return nil
}
