// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestOIDCRateLimiterIPBurst(t *testing.T) {
	rl := NewOIDCRateLimiter(10, 5, 0, 0, nil)
	defer rl.Stop()
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Fire burst+1 requests in rapid succession from the same IP.
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
	rl := NewOIDCRateLimiter(1, 0, 0, 0, nil)
	defer rl.Stop()
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

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
	rl := NewOIDCRateLimiter(10, 0, 0, 0, nil)
	defer rl.Stop()
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
	if retry := rec.Header().Get("Retry-After"); retry == "" || retry == "0" {
		t.Fatalf("expected non-zero Retry-After header, got %q", retry)
	}
}

func TestOIDCRateLimiterFastCloseAfterAllow(t *testing.T) {
	rl := NewOIDCRateLimiter(100, 100, 0, 0, nil)
	defer rl.Stop()
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
	rl := NewOIDCRateLimiter(100, 1, 0, 0, nil)
	defer rl.Stop()
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

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
	rl := NewOIDCRateLimiter(10, 1, 0, 0, nil)
	defer rl.Stop()
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login", nil)
	req1.RemoteAddr = "10.0.0.1:12345"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("ip .1 first: expected 200, got %d", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login", nil)
	req2.RemoteAddr = "10.0.0.2:12345"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("ip .2 first: expected 200, got %d", rec2.Code)
	}

	req3 := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login", nil)
	req3.RemoteAddr = "10.0.0.1:12345"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("ip .1 second: expected 429, got %d", rec3.Code)
	}
}

func TestOIDCRateLimiterProviderBucket(t *testing.T) {
	// Per-provider limit: 120/min = 2/sec, burst 1.
	rl := NewOIDCRateLimiter(100, 100, 120, 1, nil)
	defer rl.Stop()
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request from IP .1 with provider_a: allowed.
	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login?provider_config_id=provider_a", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("provider_a first: expected 200, got %d", rec.Code)
	}

	// Second from different IP .2, same provider: blocked (provider bucket exhausted).
	req2 := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login?provider_config_id=provider_a", nil)
	req2.RemoteAddr = "10.0.0.2:54321"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("provider_a second (different IP): expected 429, got %d", rec2.Code)
	}

	// Different provider_b from IP .2: allowed (independent provider bucket).
	req3 := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login?provider_config_id=provider_b", nil)
	req3.RemoteAddr = "10.0.0.2:54321"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("provider_b first: expected 200, got %d", rec3.Code)
	}
}

func TestOIDCRateLimiterThrottleCounter(t *testing.T) {
	// Verify the throttle counter is called when rate-limited without panicking
	// on nil instruments.
	rl := NewOIDCRateLimiter(10, 1, 0, 0, nil)
	defer rl.Stop()
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var blocked int
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			blocked++
		}
	}
	if blocked != 2 {
		t.Fatalf("expected 2 blocked requests (first allowed, then 2 blocked), got %d", blocked)
	}
}

func TestOIDCRateLimiterConcurrent(t *testing.T) {
	rl := NewOIDCRateLimiter(1000, 500, 0, 0, nil)
	defer rl.Stop()
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	var allowed, blocked int
	var mu sync.Mutex

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login", nil)
			req.RemoteAddr = "10.0.0.1:12345"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			mu.Lock()
			switch rec.Code {
			case http.StatusOK:
				allowed++
			case http.StatusTooManyRequests:
				blocked++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	// 500 burst, 100 concurrent — none should panic.
	t.Logf("concurrent: %d allowed, %d blocked", allowed, blocked)
}

func TestExtractClientIP(t *testing.T) {
	tests := []struct {
		name     string
		remote   string
		expected string
	}{
		{"RemoteAddr with port", "192.0.2.1:54321", "192.0.2.1"},
		{"RemoteAddr no port", "192.0.2.1", "192.0.2.1"},
		{"IPv6 with port", "[::1]:12345", "::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remote
			got := extractClientIP(req)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}
