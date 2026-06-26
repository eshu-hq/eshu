// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOIDCRateLimiterIPBurst(t *testing.T) {
	rl := NewOIDCRateLimiter(10, 5, 60, 10, nil)
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Fire burst+1 requests in rapid succession from the same IP.
	// The limiter allows burst requests then rejects the next.
	for i := 0; i < 6; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login?provider_config_id=okta", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if i < 5 {
			if rec.Code != http.StatusOK {
				t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
			}
		} else {
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("request %d: expected 429 after burst, got %d", i, rec.Code)
			}
		}
	}
}

func TestOIDCRateLimiterNonOIDCRoutePassesThrough(t *testing.T) {
	rl := NewOIDCRateLimiter(1, 0, 60, 0, nil)
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Non-OIDC route should never be rate-limited, even with a restrictive limiter.
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/profile", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("non-OIDC route was rate-limited: request %d got %d", i, rec.Code)
		}
	}
}

func TestOIDCRateLimiterRetryAfterHeader(t *testing.T) {
	rl := NewOIDCRateLimiter(10, 0, 60, 0, nil)
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if retry := rec.Header().Get("Retry-After"); retry == "" {
		t.Fatal("expected Retry-After header")
	}
}

func TestOIDCRateLimiterFastCloseAfterAllow(t *testing.T) {
	// When the rate allows, the request reaches the underlying handler.
	rl := NewOIDCRateLimiter(100, 100, 600, 100, nil)
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/callback?code=abc&state=xyz", nil)
	req.RemoteAddr = "10.0.0.2:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 from underlying handler, got %d", rec.Code)
	}
}

func TestOIDCRateLimiterIPTokenRefill(t *testing.T) {
	// After a token bucket refills, previously rate-limited IPs can send again.
	rl := NewOIDCRateLimiter(100, 1, 600, 100, nil)
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Burst of 2: first allowed, second blocked.
	req1 := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login", nil)
	req1.RemoteAddr = "10.0.0.5:12345"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login", nil)
	req2.RemoteAddr = "10.0.0.5:12345"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d", rec2.Code)
	}

	// Wait for refill. At 100 req/sec, 1 token refills every 10ms. The failed
	// Allow on the second request still advances the limiter's time, so we
	// need a full token's worth of time to pass.
	time.Sleep(25 * time.Millisecond)

	req3 := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login", nil)
	req3.RemoteAddr = "10.0.0.5:12345"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("after refill: expected 200, got %d", rec3.Code)
	}
}

func TestOIDCRateLimiterDifferentIPsIndependent(t *testing.T) {
	// Rate limits are per-IP; different IPs get their own buckets.
	rl := NewOIDCRateLimiter(10, 1, 60, 1, nil)
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request from IP .1 consumes the burst.
	req1 := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login", nil)
	req1.RemoteAddr = "10.0.0.1:12345"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("ip .1 first: expected 200, got %d", rec1.Code)
	}

	// Second request from IP .2 should succeed with its own bucket.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login", nil)
	req2.RemoteAddr = "10.0.0.2:12345"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("ip .2 first: expected 200, got %d", rec2.Code)
	}

	// Second request from IP .1 is blocked.
	req3 := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login", nil)
	req3.RemoteAddr = "10.0.0.1:12345"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("ip .1 second: expected 429, got %d", rec3.Code)
	}
}

func TestExtractClientIP(t *testing.T) {
	tests := []struct {
		name     string
		xff      string
		remote   string
		expected string
	}{
		{"X-Forwarded-For present", "203.0.113.1, 10.0.0.1", "10.0.0.99:12345", "203.0.113.1"},
		{"X-Forwarded-For single", "198.51.100.1", "10.0.0.99:12345", "198.51.100.1"},
		{"RemoteAddr only", "", "192.0.2.1:54321", "192.0.2.1"},
		{"RemoteAddr no port", "", "192.0.2.1", "192.0.2.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			req.RemoteAddr = tt.remote
			got := extractClientIP(req)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}
