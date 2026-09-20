// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
	"time"
)

const testExemptRoute = "GET /api/v0/exempt-route"

// withExemption temporarily replaces LatencyExemptions for a test and restores
// the real table afterward, so tests never depend on execution order.
func withExemption(t *testing.T, exemptions map[string]LatencyExemption) {
	t.Helper()
	orig := LatencyExemptions
	LatencyExemptions = exemptions
	t.Cleanup(func() { LatencyExemptions = orig })
}

// TestEvaluateBudgetsSkipsExemptRouteOverLatencyBudget pins the exemption's
// whole purpose: a route named in LatencyExemptions does not fail the run on
// latency alone, because the ceiling is a known, tracked, temporary gap
// (issue #6858), not a real regression this gate should block on.
func TestEvaluateBudgetsSkipsExemptRouteOverLatencyBudget(t *testing.T) {
	withExemption(t, map[string]LatencyExemption{
		testExemptRoute: {Issue: "#6858", Reason: "graph-backed aggregate; moving off the graph is tracked separately"},
	})
	budgets, err := ParseRouteBudgets(strings.NewReader("default\t1500\n"))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}
	results := []RouteLatency{
		{Route: testExemptRoute, P95: 3 * time.Second, Exercised: true},
	}
	breaches := EvaluateBudgets(results, budgets)
	if len(breaches) != 0 {
		t.Fatalf("EvaluateBudgets breaches = %d, want 0 — an exempt route over its latency budget must not fail the run: %+v", len(breaches), breaches)
	}
}

// TestEvaluateBudgetsStillBreaksNonExemptRouteOverLatencyBudget is the control:
// a route absent from LatencyExemptions must still breach normally.
func TestEvaluateBudgetsStillBreaksNonExemptRouteOverLatencyBudget(t *testing.T) {
	withExemption(t, map[string]LatencyExemption{
		testExemptRoute: {Issue: "#6858", Reason: "graph-backed aggregate"},
	})
	budgets, err := ParseRouteBudgets(strings.NewReader("default\t1500\n"))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}
	results := []RouteLatency{
		{Route: "GET /api/v0/not-exempt", P95: 3 * time.Second, Exercised: true},
	}
	breaches := EvaluateBudgets(results, budgets)
	if len(breaches) != 1 {
		t.Fatalf("EvaluateBudgets breaches = %d, want 1 — a non-exempt route over budget must still fail the run", len(breaches))
	}
}

// TestEvaluateBudgetsStillBreaksExemptRouteOnHardFailed proves the exemption
// covers only the latency ceiling: a 5xx on an exempt route must still
// breach, exactly like TestEvaluateBudgetsTreatsHardFailedAsBreachRegardlessOfP95.
func TestEvaluateBudgetsStillBreaksExemptRouteOnHardFailed(t *testing.T) {
	withExemption(t, map[string]LatencyExemption{
		testExemptRoute: {Issue: "#6858", Reason: "graph-backed aggregate"},
	})
	budgets, err := ParseRouteBudgets(strings.NewReader("default\t1500\n"))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}
	results := []RouteLatency{
		{Route: testExemptRoute, P95: 1 * time.Millisecond, Exercised: true, HardFailed: true},
	}
	breaches := EvaluateBudgets(results, budgets)
	if len(breaches) != 1 || !breaches[0].HardFailed {
		t.Fatalf("EvaluateBudgets breaches = %+v, want one HardFailed breach — an exemption must never suppress a 5xx", breaches)
	}
}

// TestPrintReportMarksExemptBreachDistinctlyFromOK pins the table's own
// contract: an exempt route over its latency budget must never print OK, and
// must name the tracking issue so nobody reads BREACH-EXEMPT as a pass.
func TestPrintReportMarksExemptBreachDistinctlyFromOK(t *testing.T) {
	withExemption(t, map[string]LatencyExemption{
		testExemptRoute: {Issue: "#6858", Reason: "graph-backed aggregate"},
	})
	budgets, err := ParseRouteBudgets(strings.NewReader("default\t1500\n"))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}
	workBudgets, err := ParseRouteWorkBudgets(strings.NewReader(testWorkBudgetFile))
	if err != nil {
		t.Fatalf("ParseRouteWorkBudgets: %v", err)
	}
	results := []RouteLatency{
		{Route: testExemptRoute, P95: 3 * time.Second, Exercised: true, Status: 200, Metered: true},
	}
	var buf strings.Builder
	printReport(&buf, results, budgets, workBudgets)
	got := reportStatus(t, buf.String(), testExemptRoute)
	if got != "BREACH-EXEMPT(#6858)" {
		t.Errorf("printReport status = %q, want %q", got, "BREACH-EXEMPT(#6858)")
	}
}

// TestValidateLatencyExemptionsRejectsMissingIssue guards the exemption list
// itself: an entry that names no issue is meaningless (nothing tracks when it
// should be removed), so the gate must refuse to start rather than silently
// accept an untracked, permanent exemption.
func TestValidateLatencyExemptionsRejectsMissingIssue(t *testing.T) {
	for name, exemptions := range map[string]map[string]LatencyExemption{
		"empty issue":  {testExemptRoute: {Issue: "", Reason: "graph-backed aggregate"}},
		"empty reason": {testExemptRoute: {Issue: "#6858", Reason: ""}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateLatencyExemptions(exemptions); err == nil {
				t.Fatalf("ValidateLatencyExemptions(%+v) = nil, want an error for a missing issue or reason", exemptions)
			}
		})
	}
}

// TestValidateLatencyExemptionsAcceptsAWellFormedEntry is the control for
// TestValidateLatencyExemptionsRejectsMissingIssue.
func TestValidateLatencyExemptionsAcceptsAWellFormedEntry(t *testing.T) {
	exemptions := map[string]LatencyExemption{
		testExemptRoute: {Issue: "#6858", Reason: "graph-backed aggregate"},
	}
	if err := ValidateLatencyExemptions(exemptions); err != nil {
		t.Fatalf("ValidateLatencyExemptions(%+v) = %v, want nil", exemptions, err)
	}
}

// TestLatencyExemptionsIsEmptyUnlessExplicitlyGranted proves the mechanism
// defaults to exempting nothing: an unlisted route is never treated as
// exempt, so adding the map itself cannot silently widen coverage.
func TestLatencyExemptionsIsEmptyUnlessExplicitlyGranted(t *testing.T) {
	if _, exempt := LatencyExemptions["GET /api/v0/some-route-nobody-listed"]; exempt {
		t.Fatalf("an unlisted route must never be exempt")
	}
}

// TestLatencyExemptionsCurrentGrantMatchesTheDocumentedOne pins the one
// standing grant this gate ships with, so a silent addition or a dropped
// issue reference fails a test instead of drifting unnoticed.
func TestLatencyExemptionsCurrentGrantMatchesTheDocumentedOne(t *testing.T) {
	if len(LatencyExemptions) != 1 {
		t.Fatalf("LatencyExemptions has %d entries, want exactly 1 (GET /api/v0/iac/resources, #6858); update this test deliberately if the grant set changes", len(LatencyExemptions))
	}
	got, ok := LatencyExemptions["GET /api/v0/iac/resources"]
	if !ok {
		t.Fatalf("LatencyExemptions is missing GET /api/v0/iac/resources")
	}
	if got.Issue != "#6858" {
		t.Errorf("LatencyExemptions[\"GET /api/v0/iac/resources\"].Issue = %q, want \"#6858\"", got.Issue)
	}
	if err := ValidateLatencyExemptions(LatencyExemptions); err != nil {
		t.Errorf("the shipped LatencyExemptions table fails its own validation: %v", err)
	}
}

// TestExemptedLatencyBreachesReportsOnlyExemptOverBudgetRoutes covers the
// reporting counterpart directly: it must return the exempt route that
// breached, skip a HardFailed route (that is a real failure, not an
// advisory), and skip a route within budget.
func TestExemptedLatencyBreachesReportsOnlyExemptOverBudgetRoutes(t *testing.T) {
	withExemption(t, map[string]LatencyExemption{
		testExemptRoute: {Issue: "#6858", Reason: "graph-backed aggregate"},
	})
	budgets, err := ParseRouteBudgets(strings.NewReader("default\t1500\n"))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}
	results := []RouteLatency{
		{Route: testExemptRoute, P95: 3 * time.Second, Exercised: true},
		{Route: testExemptRoute, P95: 1 * time.Millisecond, Exercised: true, HardFailed: true},
		{Route: "GET /api/v0/within-budget", P95: 1 * time.Millisecond, Exercised: true},
		{Route: "GET /api/v0/not-exempt", P95: 3 * time.Second, Exercised: true},
	}
	exempted := ExemptedLatencyBreaches(results, budgets)
	if len(exempted) != 1 || exempted[0].Route != testExemptRoute || exempted[0].P95 != 3*time.Second {
		t.Fatalf("ExemptedLatencyBreaches = %+v, want exactly one entry for %s at 3s", exempted, testExemptRoute)
	}
}

// TestExemptRouteStillBreachesOnWorkBudget proves the exemption's scope
// cannot silently widen past the latency ceiling: a route exempt from its
// latency budget still fails the run through EvaluateWorkBudgets when its
// Postgres work exceeds budget, exactly as an unexempt route would.
func TestExemptRouteStillBreachesOnWorkBudget(t *testing.T) {
	withExemption(t, map[string]LatencyExemption{
		testExemptRoute: {Issue: "#6858", Reason: "graph-backed aggregate"},
	})
	workBudgets, err := ParseRouteWorkBudgets(strings.NewReader(testWorkBudgetFile))
	if err != nil {
		t.Fatalf("ParseRouteWorkBudgets: %v", err)
	}
	def := workBudgets.For("GET /api/v0/unlisted")
	results := []RouteLatency{
		{
			Route: testExemptRoute, P95: 3 * time.Second, Exercised: true, Metered: true,
			Work: WorkPerRequest{Calls: 1, Blks: float64(def.Blks) * 3, Rows: 1},
		},
	}
	if breaches := EvaluateWorkBudgets(results, workBudgets); len(breaches) != 1 {
		t.Fatalf("EvaluateWorkBudgets on an exempt route with a work-budget breach = %d breaches, want 1 — the latency exemption must not touch the work budget", len(breaches))
	}
}

// TestPrintReportShowsPlainBreachWhenExemptRouteAlsoBreaksWorkBudget pins the
// switch's case order: an exempt route's advisory latency status must never
// mask a real work-budget breach on the same route. Only when the route is
// over its ceiling and NOTHING else is wrong does BREACH-EXEMPT apply.
func TestPrintReportShowsPlainBreachWhenExemptRouteAlsoBreaksWorkBudget(t *testing.T) {
	withExemption(t, map[string]LatencyExemption{
		testExemptRoute: {Issue: "#6858", Reason: "graph-backed aggregate"},
	})
	budgets, err := ParseRouteBudgets(strings.NewReader("default\t1500\n"))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}
	workBudgets, err := ParseRouteWorkBudgets(strings.NewReader(testWorkBudgetFile))
	if err != nil {
		t.Fatalf("ParseRouteWorkBudgets: %v", err)
	}
	def := workBudgets.For("GET /api/v0/unlisted")
	results := []RouteLatency{
		{
			Route: testExemptRoute, P95: 3 * time.Second, Exercised: true, Status: 200, Metered: true,
			Work: WorkPerRequest{Calls: 1, Blks: float64(def.Blks) * 3, Rows: 1},
		},
	}
	var buf strings.Builder
	printReport(&buf, results, budgets, workBudgets)
	got := reportStatus(t, buf.String(), testExemptRoute)
	if got != "BREACH" {
		t.Errorf("printReport status = %q, want %q — a work-budget breach on an exempt route must not read as advisory", got, "BREACH")
	}
}
