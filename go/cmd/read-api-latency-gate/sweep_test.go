// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestSweepRoutesComputesTrueNearestRankP95 pins the review-mandated fix:
// with 20 samples and exactly one outlier, nearest-rank p95
// (ceil(0.95*20)-1 = index 18 of 20, 0-based) is the SECOND-highest sample,
// not the maximum. The prior implementation (idx = int(n*0.95 + 0.9999))
// returned index 19 — the max — for n=20, so a single noisy request (a cold
// connection, a GC pause, anything) failed the whole route's budget. This
// test would have caught that: it asserts p95 is NOT dominated by the one
// slow sample.
func TestSweepRoutesComputesTrueNearestRankP95(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		// Warmup requests (see TestSweepRoutesDiscardsWarmupRequests) come
		// first and are excluded from the counted sample; make the LAST of
		// the 20 counted requests the one outlier.
		if n == warmupRequests+20 {
			time.Sleep(50 * time.Millisecond)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	results, err := SweepRoutes(SweepOptions{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Routes:     []string{"GET /health"},
		Iterations: 20,
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("SweepRoutes: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].P95 >= 40*time.Millisecond {
		t.Fatalf("p95 = %v, want < 40ms — a single outlier among 20 samples must not dominate nearest-rank p95 (index 18 of 20, the second-highest sample)", results[0].P95)
	}
}

// TestSweepRoutesDiscardsWarmupRequests pins the other half of that fix: the
// first warmupRequests probes (a cold connection, cold Postgres/NornicDB
// caches) are issued but excluded from the counted sample set, so they
// cannot skew p95.
func TestSweepRoutesDiscardsWarmupRequests(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n <= warmupRequests {
			time.Sleep(50 * time.Millisecond) // slow warmup, must be discarded
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	results, err := SweepRoutes(SweepOptions{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Routes:     []string{"GET /health"},
		Iterations: 20,
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("SweepRoutes: %v", err)
	}
	if results[0].P95 >= 40*time.Millisecond {
		t.Fatalf("p95 = %v, want < 40ms — the slow warmup requests must be discarded, not counted", results[0].P95)
	}
	if got := int(atomic.LoadInt32(&calls)); got != warmupRequests+20 {
		t.Errorf("total requests issued = %d, want %d (warmup + iterations)", got, warmupRequests+20)
	}
}

func TestSweepRoutesSendsBearerAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, err := SweepRoutes(SweepOptions{
		BaseURL:    srv.URL,
		APIKey:     "my-secret-key",
		Routes:     []string{"GET /health"},
		Iterations: 1,
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("SweepRoutes: %v", err)
	}
	if gotAuth != "Bearer my-secret-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer my-secret-key")
	}
}

// TestSweepRoutesMarksClientErrorAsNotExercised is the vacuity fix: a 4xx
// response means this route needed a selector or scope this gate did not
// (or could not) supply — that is not evidence the route is fast, so it must
// not silently count as a passing latency sample. A 4xx on the first warmup
// probe marks the route Exercised=false without spending the iteration
// budget on a route that will keep rejecting the request.
func TestSweepRoutesMarksClientErrorAsNotExercised(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	results, err := SweepRoutes(SweepOptions{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Routes:     []string{"GET /needs-args"},
		Iterations: 20,
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("SweepRoutes: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Exercised {
		t.Fatalf("Exercised = true, want false for a 400 response")
	}
	if results[0].Status != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", results[0].Status, http.StatusBadRequest)
	}
	if got := int(atomic.LoadInt32(&calls)); got != 1 {
		t.Errorf("calls = %d, want 1 (a known-rejecting route should not spend the warmup or iteration budget)", got)
	}
}

// TestSweepRoutesAppliesQueryArgs proves the other half of the vacuity fix:
// when opts.QueryArgs supplies a representative selector for a route, that
// selector is appended to the request URL so the route can run its real
// query instead of 400ing on a missing required parameter.
func TestSweepRoutesAppliesQueryArgs(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, err := SweepRoutes(SweepOptions{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Routes:     []string{"GET /needs-scope"},
		QueryArgs:  map[string]string{"GET /needs-scope": "scope_id=seed-scope-git-0000"},
		Iterations: 1,
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("SweepRoutes: %v", err)
	}
	if gotQuery != "scope_id=seed-scope-git-0000" {
		t.Errorf("query = %q, want %q", gotQuery, "scope_id=seed-scope-git-0000")
	}
}

// TestSweepRoutesTreatsTimeoutAsMaxLatencySample documents the other half of
// the same correction: #6793's /api/v0/iac/resources route did not return an
// error status on the broken live instance, it hung until the load balancer
// killed the connection (status 000, ~40s). A client-side timeout on that
// same shape must show up as a very large — and therefore budget-breaching —
// latency sample, not as a sweep crash that hides the regression entirely.
func TestSweepRoutesTreatsTimeoutAsMaxLatencySample(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	results, err := SweepRoutes(SweepOptions{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Routes:     []string{"GET /slow"},
		Iterations: 1,
		Timeout:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("SweepRoutes: %v (a client-side timeout must be measured, not treated as a sweep failure)", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if !results[0].Exercised {
		t.Fatalf("Exercised = false, want true — a timeout means the backend was actually hit")
	}
	if results[0].P95 < 50*time.Millisecond {
		t.Errorf("p95 = %v, want >= 50ms (the configured timeout, since the handler never responds in time)", results[0].P95)
	}
}

// TestSweepRoutesTreats5xxAsHardFailure pins that a 5xx must never be able to
// hide behind a fast latency sample. A regression that makes a budgeted
// route fail FAST (a query error, an early 500) must still fail the gate —
// so a 5xx forces HardFailed=true regardless of how quickly it answered.
func TestSweepRoutesTreats5xxAsHardFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError) // fast failure, no sleep
		_, _ = w.Write([]byte(`{"error":"component extension registry is unavailable"}`))
	}))
	defer srv.Close()

	results, err := SweepRoutes(SweepOptions{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Routes:     []string{"GET /broken"},
		Iterations: 1,
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("SweepRoutes: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if !results[0].Exercised {
		t.Fatalf("Exercised = false, want true for a 500 response")
	}
	if !results[0].HardFailed {
		t.Fatalf("HardFailed = false, want true — a 5xx must never pass just because it answered fast")
	}
	if !strings.Contains(results[0].HardFailedBody, "component extension registry is unavailable") {
		t.Fatalf("HardFailedBody = %q, want it to contain the response body so an operator can see why the route failed", results[0].HardFailedBody)
	}
}

// TestSweepRoutesTruncatesHardFailedBody guards the byte cap: an operator
// needs enough of the error envelope to diagnose it, but an unbounded body
// (or a pathological handler that streams megabytes on error) must not blow
// up the report.
func TestSweepRoutesTruncatesHardFailedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("x", 10000)))
	}))
	defer srv.Close()

	results, err := SweepRoutes(SweepOptions{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Routes:     []string{"GET /broken"},
		Iterations: 1,
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("SweepRoutes: %v", err)
	}
	if got := len(results[0].HardFailedBody); got > hardFailedBodyCap {
		t.Fatalf("len(HardFailedBody) = %d, want at most %d", got, hardFailedBodyCap)
	}
}

// TestSweepRoutesKeepsFirstHardFailedBody guards against a later 5xx sample
// overwriting the FIRST captured body: the first failure is usually the most
// informative (a later one may just be repeated backend contention noise),
// and overwriting it would silently discard the evidence an operator needs.
func TestSweepRoutesKeepsFirstHardFailedBody(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, "failure #%d", call)
	}))
	defer srv.Close()

	results, err := SweepRoutes(SweepOptions{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Routes:     []string{"GET /broken"},
		Iterations: 3,
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("SweepRoutes: %v", err)
	}
	if results[0].HardFailedBody != "failure #3" {
		t.Fatalf("HardFailedBody = %q, want %q (the first COUNTED-sample failure; warmupRequests=2 precede it)", results[0].HardFailedBody, "failure #3")
	}
}

// TestSweepRoutesFailsOnConnectionFailure keeps the one case that must still
// abort loudly: the target server is not reachable at all. That is a gate
// setup problem (eshu-api never came up), not a per-route latency signal.
func TestSweepRoutesFailsOnConnectionFailure(t *testing.T) {
	_, err := SweepRoutes(SweepOptions{
		BaseURL:    "http://127.0.0.1:1", // reserved, nothing listens here
		APIKey:     "test-key",
		Routes:     []string{"GET /health"},
		Iterations: 1,
		Timeout:    2 * time.Second,
	})
	if err == nil {
		t.Fatalf("expected error for a connection failure")
	}
}
