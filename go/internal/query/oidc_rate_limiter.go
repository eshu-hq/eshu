// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"math"
	"net"
	"net/http"
	"strconv"
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

	// DefaultOIDCLoginProviderRatePerMin is the default per-provider rate limit
	// in requests per minute for the OIDC login endpoint. This is a coarse
	// per-IdP defense, not a per-user limit — all login attempts to a single
	// provider share one bucket. login has provider_config_id; callback does
	// not, so the callback path always skips this check.
	DefaultOIDCLoginProviderRatePerMin = 60

	// DefaultOIDCLoginProviderBurst is the default burst size for the per-provider
	// rate limiter.
	DefaultOIDCLoginProviderBurst = 10

	// staleEntryAge is the maximum age of a rate-limiter entry before eviction.
	staleEntryAge = 10 * time.Minute
)

// OIDCRateLimiter enforces per-IP and per-provider token-bucket rate limits on
// OIDC login and callback routes. It returns HTTP 429 when either bucket is
// exhausted, with a Retry-After header set to the longer of the two reset times.
// A background janitor evicts entries that have not been accessed in staleEntryAge.
type OIDCRateLimiter struct {
	ipRate        float64
	ipBurst       int
	providerRate  float64 // requests per second (converted from per-minute)
	providerBurst int
	instruments   *telemetry.Instruments

	mu       sync.Mutex
	ips      map[string]*rateLimiterPair
	providers map[string]*rateLimiterPair

	stopCh chan struct{}
}

type rateLimiterPair struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// NewOIDCRateLimiter creates an OIDC rate limiter with per-IP and per-provider
// token buckets. Rate of 0 disables that bucket. Burst of 0 means no initial
// burst allowance (strict token-bucket mode). Negative values are coerced to 0.
// A nil instruments argument disables the throttled counter.
func NewOIDCRateLimiter(ipRatePerSec float64, ipBurst int, providerRatePerMin float64, providerBurst int, instruments *telemetry.Instruments) *OIDCRateLimiter {
	if ipRatePerSec < 0 {
		ipRatePerSec = 0
	}
	if ipBurst < 0 {
		ipBurst = 0
	}
	if providerRatePerMin < 0 {
		providerRatePerMin = 0
	}
	if providerBurst < 0 {
		providerBurst = 0
	}
	rl := &OIDCRateLimiter{
		ipRate:        ipRatePerSec,
		ipBurst:       ipBurst,
		providerRate:  providerRatePerMin / 60.0,
		providerBurst: providerBurst,
		instruments:   instruments,
		ips:           make(map[string]*rateLimiterPair),
		providers:     make(map[string]*rateLimiterPair),
		stopCh:        make(chan struct{}),
	}
	if ipRatePerSec > 0 || providerRatePerMin > 0 {
		go rl.janitor()
	}
	return rl
}

// Stop shuts down the background janitor. No further requests should arrive.
func (rl *OIDCRateLimiter) Stop() {
	close(rl.stopCh)
}

func (rl *OIDCRateLimiter) janitor() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-rl.stopCh:
			return
		case now := <-ticker.C:
			rl.evictStale(now)
		}
	}
}

func (rl *OIDCRateLimiter) evictStale(now time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	for ip, pair := range rl.ips {
		if now.Sub(pair.lastUsed) > staleEntryAge {
			delete(rl.ips, ip)
		}
	}
	for prov, pair := range rl.providers {
		if now.Sub(pair.lastUsed) > staleEntryAge {
			delete(rl.providers, prov)
		}
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
		provider := QueryParam(r, "provider_config_id")

		if !rl.allow(ip, provider) {
			if rl.instruments != nil {
				rl.instruments.OIDCLoginThrottled.Add(r.Context(), 1)
			}
			w.Header().Set("Retry-After", strconv.Itoa(rl.retryAfter(ip, provider)))
			WriteError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// allow checks both the per-IP and per-provider rate limits. Returns true when
// the request is within both limits. The per-provider check is only applied when
// the provider identifier is non-empty (login endpoint with provider_config_id;
// callback carries only state+code and always skips this check).
func (rl *OIDCRateLimiter) allow(ip, provider string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.ipRate > 0 && !rl.ipLimiterPair(ip).limiter.Allow() {
		return false
	}
	if len(provider) == 0 || rl.providerRate <= 0 {
		return true
	}
	return rl.providerLimiterPair(provider).limiter.Allow()
}

// retryAfter returns the maximum wait time (in seconds) until the more
// restrictive bucket has tokens again. Always at least 1 second.
// Uses Reserve().Delay() then Cancel() to avoid consuming the reserved token.
func (rl *OIDCRateLimiter) retryAfter(ip, provider string) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	d := float64(0)
	{
		r := rl.ipLimiterPair(ip).limiter.Reserve()
		d = r.Delay().Seconds()
		r.Cancel()
	}
	if len(provider) > 0 && rl.providerRate > 0 {
		r := rl.providerLimiterPair(provider).limiter.Reserve()
		ud := r.Delay().Seconds()
		r.Cancel()
		if ud > d {
			d = ud
		}
	}
	sec := int(math.Ceil(d))
	if sec < 1 {
		sec = 1
	}
	return sec
}

func (rl *OIDCRateLimiter) ipLimiterPair(ip string) *rateLimiterPair {
	pair, ok := rl.ips[ip]
	if !ok {
		pair = &rateLimiterPair{
			limiter: rate.NewLimiter(rate.Limit(rl.ipRate), rl.ipBurst),
		}
		rl.ips[ip] = pair
	}
	pair.lastUsed = time.Now()
	return pair
}

func (rl *OIDCRateLimiter) providerLimiterPair(provider string) *rateLimiterPair {
	pair, ok := rl.providers[provider]
	if !ok {
		pair = &rateLimiterPair{
			limiter: rate.NewLimiter(rate.Limit(rl.providerRate), rl.providerBurst),
		}
		rl.providers[provider] = pair
	}
	pair.lastUsed = time.Now()
	return pair
}

// isOIDCRoute reports whether path is an OIDC login or callback route.
func isOIDCRoute(path string) bool {
	return path == "/api/v0/auth/oidc/login" || path == "/api/v0/auth/oidc/callback"
}

// extractClientIP extracts the client IP from RemoteAddr only. X-Forwarded-For
// is intentionally NOT trusted — without a trusted-proxy gate, an attacker
// can set XFF to a fresh value on every request and bypass the per-IP limit.
func extractClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
