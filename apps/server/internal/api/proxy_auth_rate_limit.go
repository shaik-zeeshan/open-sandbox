package api

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type proxyAuthRateLimitConfig struct {
	RequestsPerSecond rate.Limit
	Burst             int
	IdleTTL           time.Duration
}

type proxyAuthRateLimiter struct {
	mu       sync.Mutex
	limit    rate.Limit
	burst    int
	idleTTL  time.Duration
	limiters map[string]*proxyAuthVisitorLimiter
}

type proxyAuthVisitorLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func loadProxyAuthRateLimitConfig() proxyAuthRateLimitConfig {
	requestsPerSecond := parsePositiveFloatEnv("SANDBOX_PROXY_AUTH_RATE_LIMIT_RPS", 120)
	burst := parsePositiveIntEnv("SANDBOX_PROXY_AUTH_RATE_LIMIT_BURST", 240)
	idleTTL := parsePositiveDurationEnv("SANDBOX_PROXY_AUTH_RATE_LIMIT_IDLE_TTL", 10*time.Minute)

	return proxyAuthRateLimitConfig{
		RequestsPerSecond: rate.Limit(requestsPerSecond),
		Burst:             burst,
		IdleTTL:           idleTTL,
	}
}

func newProxyAuthRateLimiter(cfg proxyAuthRateLimitConfig) *proxyAuthRateLimiter {
	if cfg.RequestsPerSecond <= 0 || cfg.Burst <= 0 {
		return nil
	}
	if cfg.IdleTTL <= 0 {
		cfg.IdleTTL = 10 * time.Minute
	}

	return &proxyAuthRateLimiter{
		limit:    cfg.RequestsPerSecond,
		burst:    cfg.Burst,
		idleTTL:  cfg.IdleTTL,
		limiters: make(map[string]*proxyAuthVisitorLimiter),
	}
}

func (l *proxyAuthRateLimiter) Allow(visitorKey string) bool {
	if l == nil {
		return true
	}

	key := strings.TrimSpace(visitorKey)
	if key == "" {
		key = "anonymous"
	}

	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.idleTTL > 0 {
		for existingKey, visitor := range l.limiters {
			if now.Sub(visitor.lastSeen) > l.idleTTL {
				delete(l.limiters, existingKey)
			}
		}
	}

	visitor, ok := l.limiters[key]
	if !ok {
		visitor = &proxyAuthVisitorLimiter{limiter: rate.NewLimiter(l.limit, l.burst), lastSeen: now}
		l.limiters[key] = visitor
	} else {
		visitor.lastSeen = now
	}

	return visitor.limiter.AllowN(now, 1)
}

func parsePositiveFloatEnv(name string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func parsePositiveIntEnv(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func parsePositiveDurationEnv(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
