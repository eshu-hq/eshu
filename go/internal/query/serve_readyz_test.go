// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestCoreReadinessProbeConcurrent hammers the backend-gated /readyz probe
// from parallel callers: the probe fans out to one goroutine per dependency
// and aggregates over a channel, so this runs under -race to catch any
// shared-state slip. Nil backends answer the permanent 503 with no
// Retry-After on every call.
func TestCoreReadinessProbeConcurrent(t *testing.T) {
	t.Parallel()
	mux, err := query.NewCoreTestMux()
	if err != nil {
		t.Fatalf("NewCoreTestMux: %v", err)
	}
	const callers = 16
	var wg sync.WaitGroup
	codes := make([]int, callers)
	retryAfter := make([]string, callers)
	for i := range callers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			codes[i] = rec.Code
			retryAfter[i] = rec.Header().Get("Retry-After")
		}(i)
	}
	wg.Wait()
	for i := range callers {
		if codes[i] != http.StatusServiceUnavailable {
			t.Errorf("caller %d: /readyz = %d, want permanent 503 (nil backends)", i, codes[i])
		}
		if retryAfter[i] != "" {
			t.Errorf("caller %d: Retry-After = %q, want empty (permanent 503)", i, retryAfter[i])
		}
	}
}

// TestCoreReadinessProbeStaysPublic asserts /readyz bypasses the auth
// chain: no credential is required for either verdict.
func TestCoreReadinessProbeStaysPublic(t *testing.T) {
	t.Parallel()
	mux, err := query.NewCoreTestMux()
	if err != nil {
		t.Fatalf("NewCoreTestMux: %v", err)
	}
	authed := query.WrapCoreAuth(mux, query.CoreServeDeps{AuthEnforcementConfigured: true})
	rec := httptest.NewRecorder()
	authed.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("authed /readyz = %d, want 503 without credentials (public probe)", rec.Code)
	}
}

// TestCoreReadinessProbeHealthIsShallow asserts /health answers 200 without
// touching backends (nil stores): it is the shallow process check, not a
// backend verdict.
func TestCoreReadinessProbeHealthIsShallow(t *testing.T) {
	t.Parallel()
	mux, err := query.NewCoreTestMux()
	if err != nil {
		t.Fatalf("NewCoreTestMux: %v", err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/health = %d, want 200 with nil stores", rec.Code)
	}
}

// TestCoreReadinessCombinerNeedsBothBackends pins the AND semantics: a
// failing graph verifier keeps /readyz at 503 even with no Postgres
// configured (each backend down independently fails the probe). The
// both-up 200 is demonstrated live in D4.4, not faked here.
func TestCoreReadinessCombinerNeedsBothBackends(t *testing.T) {
	t.Parallel()
	router, err := query.BuildCoreRouter(query.CoreServeDeps{})
	if err != nil {
		t.Fatalf("BuildCoreRouter: %v", err)
	}
	verify := func(context.Context) error { return errCoreProbeDown }
	mux := http.NewServeMux()
	query.MountCoreMux(mux, router, nil, verify)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz with failing graph = %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q, want empty", got)
	}
}

var errCoreProbeDown = errors.New("backend down")
