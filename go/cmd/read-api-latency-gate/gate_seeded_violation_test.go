// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
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
