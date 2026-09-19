// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// gateSeededViolationBudgets is a minimal budget table for the RED/GREEN
// pair below: a 200ms budget on the one route under test, well above any
// "fast" handler's real latency and well below the injected "slow" handler's
// sleep, so the two cases are unambiguous.
const gateSeededViolationBudgets = "default\t200\n"

// TestGateCatchesInjectedSlowRouteRED is the seeded-violation half of the
// RED/GREEN pair issue #6797 requires: an intentionally slow handler (a
// stand-in for #6793/#6794's regression shape) must make the full
// sweep-then-evaluate path this gate runs in production report a breach.
// If this test ever goes green on an unmodified slow handler, the gate's
// core detection mechanism (SweepRoutes -> EvaluateBudgets) is broken.
func TestGateCatchesInjectedSlowRouteRED(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond) // seeded violation: exceeds the 200ms budget
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	results, err := SweepRoutes(SweepOptions{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Routes:     []string{"GET /seeded-slow-route"},
		Iterations: 3,
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("SweepRoutes: %v", err)
	}
	budgets, err := ParseRouteBudgets(strings.NewReader(gateSeededViolationBudgets))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}

	breaches := EvaluateBudgets(results, budgets)
	if len(breaches) != 1 {
		t.Fatalf("RED case: breaches = %d, want 1 — an injected 300ms handler against a 200ms budget must be reported as a breach", len(breaches))
	}
	if breaches[0].Route != "GET /seeded-slow-route" {
		t.Errorf("breach route = %q, want GET /seeded-slow-route", breaches[0].Route)
	}
}

// TestGateAcceptsFastRouteGREEN is the other half of the pair: the identical
// path, with the injected sleep removed, must NOT report a breach. Without
// this half, the RED test alone could not distinguish "the gate correctly
// detects slow routes" from "the gate reports every route as a breach."
func TestGateAcceptsFastRouteGREEN(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // no injected sleep
	}))
	defer srv.Close()

	results, err := SweepRoutes(SweepOptions{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Routes:     []string{"GET /seeded-slow-route"},
		Iterations: 3,
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("SweepRoutes: %v", err)
	}
	budgets, err := ParseRouteBudgets(strings.NewReader(gateSeededViolationBudgets))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}

	breaches := EvaluateBudgets(results, budgets)
	if len(breaches) != 0 {
		t.Fatalf("GREEN case: breaches = %d, want 0 (%+v) — the fix (removing the injected sleep) must clear the breach", len(breaches), breaches)
	}
}

// workBudgetsForSeededViolation is the budget row the work-metric RED/GREEN
// pairs below run against: the buffer, row and call counts measured for the #6794
// status family (docs/internal/evidence/6797-read-api-work-metric-shim.md).
const workBudgetsForSeededViolation = "default\t35\t20000\t5000\n"

// sweepWithFakeWork runs the production sweep-then-evaluate path against a fast
// 200 handler with a fake meter that reports perRequest work, so the RED/GREEN
// difference is purely the counters, not the (identical, fast) latency.
func sweepWithFakeWork(t *testing.T, perRequest WorkCounters) []WorkBreach {
	t.Helper()
	const iterations = 4
	var events []string
	var mu sync.Mutex
	srv := meteredServer(&events, &mu, http.StatusOK)
	defer srv.Close()
	meter := &fakeMeter{events: &events, counters: WorkCounters{
		Calls: perRequest.Calls * iterations,
		Rows:  perRequest.Rows * iterations,
		Blks:  perRequest.Blks * iterations,
	}}

	results, err := SweepRoutes(SweepOptions{
		BaseURL: srv.URL, APIKey: "test-key", Routes: []string{"GET /seeded-work-route"},
		Iterations: iterations, Timeout: 5 * time.Second, Meter: meter,
	})
	if err != nil {
		t.Fatalf("SweepRoutes: %v", err)
	}
	budgets, err := ParseRouteWorkBudgets(strings.NewReader(workBudgetsForSeededViolation))
	if err != nil {
		t.Fatalf("ParseRouteWorkBudgets: %v", err)
	}
	return EvaluateWorkBudgets(results, budgets)
}

// TestGateCatchesPlanShapeRegressionByBuffersRED is the #6794 shape: the
// statement count stays within budget (25 of 35) while buffers and rows are
// orders of magnitude over. Latency is identical and fast, so only the work
// budget can fail this — which is the point of the metric.
func TestGateCatchesPlanShapeRegressionByBuffersRED(t *testing.T) {
	breaches := sweepWithFakeWork(t, WorkCounters{Calls: 25, Blks: 2_000_000, Rows: 70_000})

	if len(breaches) != 1 {
		t.Fatalf("RED case: breaches = %d, want exactly 1", len(breaches))
	}
	if got := strings.Join(breaches[0].Exceeded, ","); got != "blks,rows" {
		t.Errorf("Exceeded = %q, want blks,rows (calls 25 is within 35)", got)
	}
}

// TestGateAcceptsPostFixWorkGREEN is the mirror: the post-fix counters clear
// the same budget, so the RED test is not just "everything breaches".
func TestGateAcceptsPostFixWorkGREEN(t *testing.T) {
	if breaches := sweepWithFakeWork(t, WorkCounters{Calls: 25, Blks: 6000, Rows: 1200}); len(breaches) != 0 {
		t.Fatalf("GREEN case: breaches = %+v, want none", breaches)
	}
}

// TestGateCatchesNPlusOneByCallsRED proves a genuine N+1 is also caught: 900
// statements per request against a budget of 35.
func TestGateCatchesNPlusOneByCallsRED(t *testing.T) {
	breaches := sweepWithFakeWork(t, WorkCounters{Calls: 900, Blks: 6000, Rows: 1200})

	if len(breaches) != 1 || strings.Join(breaches[0].Exceeded, ",") != "calls" {
		t.Fatalf("RED case: breaches = %+v, want exactly one breach on calls", breaches)
	}
}
