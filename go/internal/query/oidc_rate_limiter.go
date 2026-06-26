// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"golang.org/x/time/rate"
)

const (
	// DefaultOIDCLoginRatePerSec is the default per-IP rate limit in requests
	// per second for the OIDC login endpoint.
	DefaultOIDCLoginRatePerSec = 10

	// DefaultOIDCLoginBurst is the default burst size for the per-IP rate limiter.
	DefaultOIDCLoginBurst = 20

	// DefaultOIDCLoginUserRatePerMin is the default per-user rate limit in
	// requests per minute for the OIDC login endpoint.
	DefaultOIDCLoginUserRatePerMin = 60

	// DefaultOIDCLoginUserBurst is the default burst size for the per-user rate limiter.
	DefaultOIDCLoginUserBurst = 10
)

// OIDCRateLimiter enforces per-IP and per-user token-bucket rate limits on
// OIDC login and callback routes. It returns HTTP 429 when either bucket is
// exhausted, with a Retry-After header set to the longer of the two reset times.
type OIDCRateLimiter struct {
	ipRate      float64
	ipBurst     int
	userRateMin float64
	userBurst   int
	instruments *telemetry.Instruments

	mu    sync.Mutex
	ips   map[string]*rateLimiterPair
	users map[string]*rateLimiterPair
}

type rateLimiterPair struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// NewOIDCRateLimiter creates an OIDC rate limiter with per-IP and per-user
// token buckets. Rate of 0 disables the limiter. Burst of 0 means no initial
// burst allowance (strict token-bucket mode). Negative values are coerced to 0.
// A nil instruments argument disables the throttled counter.
func NewOIDCRateLimiter(ipRatePerSec float64, ipBurst int, userRatePerMin float64, userBurst int, instruments *telemetry.Instruments) *OIDCRateLimiter {
	if ipRatePerSec < 0 {
		ipRatePerSec = 0
	}
	if ipBurst < 0 {
		ipBurst = 0
	}
	if userRatePerMin < 0 {
		userRatePerMin = 0
	}
	if userBurst < 0 {
		userBurst = 0
	}
	return &OIDCRateLimiter{
		ipRate:      ipRatePerSec,
		ipBurst:     ipBurst,
		userRateMin: userRatePerMin,
		userBurst:   userBurst,
		instruments: instruments,
		ips:         make(map[string]*rateLimiterPair),
		users:       make(map[string]*rateLimiterPair),
	}
}

// Middleware returns an http.Handler that rate-limits requests to OIDC login
// and callback paths. Non-OIDC paths pass through unmeasured.
func (rl *OIDCRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isOIDCRoute(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		ip := extractClientIP(r)
		user := QueryParam(r, "provider_config_id")

		if !rl.allow(ip, user) {
			if rl.instruments != nil {
				rl.instruments.OIDCLoginThrottled.Add(r.Context(), 1)
			}
			w.Header().Set("Retry-After", strconv.Itoa(int(rl.retryAfterSeconds(ip, user))))
			WriteError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// allow checks both the per-IP and per-user rate limits. Returns true when the
// request is within both limits. The per-user check is only applied when the
// user identifier is non-empty (login endpoint with provider_config_id).
func (rl *OIDCRateLimiter) allow(ip, user string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if !rl.ipLimiterPair(ip).limiter.Allow() {
		return false
	}
	if len(user) == 0 {
		return true
	}
	return rl.userLimiterPair(user).limiter.Allow()
}

// retryAfterSeconds returns the maximum wait time until either the per-IP or
// per-user bucket has tokens again.
func (rl *OIDCRateLimiter) retryAfterSeconds(ip, user string) float64 {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	d := rl.ipLimiterPair(ip).limiter.Reserve().Delay().Seconds()
	if len(user) > 0 {
		ud := rl.userLimiterPair(user).limiter.Reserve().Delay().Seconds()
		if ud > d {
			d = ud
		}
	}
	return d
}

func (rl *OIDCRateLimiter) ipLimiterPair(ip string) *rateLimiterPair {
	pair, ok := rl.ips[ip]
	if !ok {
		pair = &rateLimiterPair{
			limiter:  rate.NewLimiter(rate.Limit(rl.ipRate), rl.ipBurst),
			lastUsed: time.Now(),
		}
		rl.ips[ip] = pair
	}
	pair.lastUsed = time.Now()
	return pair
}

func (rl *OIDCRateLimiter) userLimiterPair(user string) *rateLimiterPair {
	pair, ok := rl.users[user]
	if !ok {
		pair = &rateLimiterPair{
			limiter:  rate.NewLimiter(rate.Limit(rl.userRateMin/60.0), rl.userBurst),
			lastUsed: time.Now(),
		}
		rl.users[user] = pair
	}
	pair.lastUsed = time.Now()
	return pair
}

// isOIDCRoute reports whether path is an OIDC login or callback route.
func isOIDCRoute(path string) bool {
	return path == "/api/v0/auth/oidc/login" || path == "/api/v0/auth/oidc/callback"
}

// extractClientIP extracts the client IP from X-Forwarded-For or RemoteAddr.
func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
