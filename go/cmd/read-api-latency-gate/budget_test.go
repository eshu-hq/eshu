// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// shippedRouteBudgetsPath is the committed budget table this gate ships,
// relative to this package directory.
const shippedRouteBudgetsPath = "../../../testdata/benchmarks/read-api-route-budgets.txt"

func TestShippedRouteBudgetsFileParses(t *testing.T) {
	f, err := os.Open(shippedRouteBudgetsPath)
	if err != nil {
		t.Fatalf("open %s: %v", shippedRouteBudgetsPath, err)
	}
	defer f.Close()

	budgets, err := ParseRouteBudgets(f)
	if err != nil {
		t.Fatalf("ParseRouteBudgets(%s): %v", shippedRouteBudgetsPath, err)
	}

	if got := budgets.For("GET /api/v0/some/route/not/in/the/table"); got <= 0 {
		t.Errorf("default budget = %v, want > 0", got)
	}
	if got, unset := budgets.For("GET /api/v0/infra/resources/count"), budgets.For("GET /api/v0/some/route/not/in/the/table"); got == unset {
		t.Errorf("GET /api/v0/infra/resources/count budget = %v, want an explicit override distinct from the default %v", got, unset)
	}
}

// testBudgetFile uses the shipped format: route<TAB>budget_ms<TAB>reason.
// Every named row requires a reason; "default" does not.
const testBudgetFile = `
# comment line, ignored
default	1000

GET /api/v0/infra/resources/count	3000	no catalog capability mapped
GET /api/v0/status/collectors	2000	no catalog capability mapped
`

func TestParseRouteBudgetsReadsDeclaredAndDefault(t *testing.T) {
	budgets, err := ParseRouteBudgets(strings.NewReader(testBudgetFile))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}

	if got, want := budgets.For("GET /api/v0/infra/resources/count"), 3000*time.Millisecond; got != want {
		t.Errorf("declared budget = %v, want %v", got, want)
	}
	if got, want := budgets.For("GET /api/v0/status/collectors"), 2000*time.Millisecond; got != want {
		t.Errorf("declared budget = %v, want %v", got, want)
	}
}

func TestParseRouteBudgetsFallsBackToDefault(t *testing.T) {
	budgets, err := ParseRouteBudgets(strings.NewReader(testBudgetFile))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}
	if got, want := budgets.For("GET /api/v0/some/new/route"), 1000*time.Millisecond; got != want {
		t.Errorf("default budget = %v, want %v", got, want)
	}
}

func TestParseRouteBudgetsRejectsMissingDefault(t *testing.T) {
	_, err := ParseRouteBudgets(strings.NewReader("GET /api/v0/foo\t100\treason\n"))
	if err == nil {
		t.Fatalf("expected error for missing default row")
	}
}

func TestParseRouteBudgetsRejectsNonNumericBudget(t *testing.T) {
	_, err := ParseRouteBudgets(strings.NewReader("default\tnot-a-number\n"))
	if err == nil {
		t.Fatalf("expected error for non-numeric budget")
	}
}

func TestParseRouteBudgetsRequiresReasonForNamedRow(t *testing.T) {
	_, err := ParseRouteBudgets(strings.NewReader("default\t1000\nGET /api/v0/foo\t500\n"))
	if err == nil {
		t.Fatalf("expected error for a named route row with no reason column")
	}
}

func TestParseRouteBudgetsRejectsDuplicateRoute(t *testing.T) {
	_, err := ParseRouteBudgets(strings.NewReader(
		"default\t1000\nGET /api/v0/foo\t500\tfirst\nGET /api/v0/foo\t900\tsecond, silently overriding the first\n",
	))
	if err == nil {
		t.Fatalf("expected error for a duplicate route row; a later row must not silently override an earlier one")
	}
}

func TestEvaluateBudgetsReportsBreaches(t *testing.T) {
	budgets, err := ParseRouteBudgets(strings.NewReader(testBudgetFile))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}

	results := []RouteLatency{
		{Route: "GET /api/v0/infra/resources/count", P95: 18900 * time.Millisecond, Exercised: true},
		{Route: "GET /api/v0/status/collectors", P95: 1500 * time.Millisecond, Exercised: true},
		{Route: "GET /api/v0/some/new/route", P95: 1200 * time.Millisecond, Exercised: true},
	}

	breaches := EvaluateBudgets(results, budgets)
	if len(breaches) != 2 {
		t.Fatalf("EvaluateBudgets breaches = %d, want 2 (%+v)", len(breaches), breaches)
	}

	byRoute := map[string]BudgetBreach{}
	for _, b := range breaches {
		byRoute[b.Route] = b
	}
	if _, ok := byRoute["GET /api/v0/infra/resources/count"]; !ok {
		t.Errorf("expected breach for infra/resources/count")
	}
	if _, ok := byRoute["GET /api/v0/some/new/route"]; !ok {
		t.Errorf("expected breach for default-budgeted route")
	}
	if _, ok := byRoute["GET /api/v0/status/collectors"]; ok {
		t.Errorf("did not expect breach for status/collectors within budget")
	}
}

func TestEvaluateBudgetsSkipsNotExercisedRoutes(t *testing.T) {
	budgets, err := ParseRouteBudgets(strings.NewReader(testBudgetFile))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}
	results := []RouteLatency{
		{Route: "GET /api/v0/some/new/route", P95: 0, Exercised: false, Status: 400},
	}
	breaches := EvaluateBudgets(results, budgets)
	if len(breaches) != 0 {
		t.Fatalf("EvaluateBudgets breaches = %d, want 0 for a not-exercised route (a 400 is not latency evidence)", len(breaches))
	}
}

// TestCatalogBudgetWinsWhenTighter proves the acceptance requirement: the
// capability-catalog-declared production p95 (times catalogCIMultiplier) is
// the effective budget when it is TIGHTER than the TSV-configured value.
// GET /api/v0/capabilities maps to capability_catalog.list, whose real
// committed catalog declares production p95_latency_ms=400 — 400*1.5=600ms,
// tighter than any plausible TSV/default value in the shipped table.
func TestCatalogBudgetWinsWhenTighter(t *testing.T) {
	f, err := os.Open(shippedRouteBudgetsPath)
	if err != nil {
		t.Fatalf("open %s: %v", shippedRouteBudgetsPath, err)
	}
	defer f.Close()
	budgets, err := ParseRouteBudgets(f)
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}
	catalog, err := capabilitycatalog.Load()
	if err != nil {
		t.Fatalf("capabilitycatalog.Load: %v", err)
	}
	budgets = budgets.WithCatalog(catalog)

	const route = "GET /api/v0/capabilities"
	got := budgets.For(route)
	want := time.Duration(float64(400*time.Millisecond) * catalogCIMultiplier)
	if got != want {
		t.Fatalf("For(%s) = %v, want %v (catalog p95 400ms * %vx multiplier)", route, got, want, catalogCIMultiplier)
	}
}

// TestConfiguredBudgetWinsWhenCatalogLooser proves the other half: a route
// whose catalog-declared p95 is LOOSER than the TSV/goal-target value must
// not silently loosen the effective budget. GET /api/v0/infra/resources/count
// maps to platform_impact.infra_resource_aggregate, whose committed catalog
// declares production p95_latency_ms=2500 (2500*1.5=3750ms) — looser than
// this gate's configured 2000ms target for the #6793 family.
func TestConfiguredBudgetWinsWhenCatalogLooser(t *testing.T) {
	f, err := os.Open(shippedRouteBudgetsPath)
	if err != nil {
		t.Fatalf("open %s: %v", shippedRouteBudgetsPath, err)
	}
	defer f.Close()
	budgets, err := ParseRouteBudgets(f)
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}
	catalog, err := capabilitycatalog.Load()
	if err != nil {
		t.Fatalf("capabilitycatalog.Load: %v", err)
	}
	budgets = budgets.WithCatalog(catalog)

	const route = "GET /api/v0/infra/resources/count"
	got := budgets.For(route)
	if got != 2000*time.Millisecond {
		t.Fatalf("For(%s) = %v, want 2000ms (the configured target; the looser catalog value 2500ms*%vx must not win)", route, got, catalogCIMultiplier)
	}
}

func TestCatalogProductionP95ReturnsFalseForUnknownCapability(t *testing.T) {
	catalog, err := capabilitycatalog.Load()
	if err != nil {
		t.Fatalf("capabilitycatalog.Load: %v", err)
	}
	if _, ok := catalogProductionP95(catalog, "not_a_real_capability.id"); ok {
		t.Fatalf("expected ok=false for an unknown capability id")
	}
}

func TestParseRouteBudgetsRejectsDuplicateDefault(t *testing.T) {
	_, err := ParseRouteBudgets(strings.NewReader("default\t1000\ndefault\t99999\n"))
	if err == nil {
		t.Fatalf("expected error for a duplicate \"default\" row; a later row must not silently loosen every unnamed route")
	}
}

func TestEvaluateBudgetsTreatsHardFailedAsBreachRegardlessOfP95(t *testing.T) {
	budgets, err := ParseRouteBudgets(strings.NewReader(testBudgetFile))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}
	results := []RouteLatency{
		{Route: "GET /api/v0/status/collectors", P95: 1 * time.Millisecond, Exercised: true, HardFailed: true},
	}
	breaches := EvaluateBudgets(results, budgets)
	if len(breaches) != 1 {
		t.Fatalf("EvaluateBudgets breaches = %d, want 1 — a HardFailed route must breach even with a 1ms p95", len(breaches))
	}
	if !breaches[0].HardFailed {
		t.Errorf("breaches[0].HardFailed = false, want true — callers (printReport, the breach summary) rely on this to distinguish a 5xx breach from a real latency breach")
	}
}

func TestRequireNamedRoutesExercisedFlagsNotExercisedNamedRoute(t *testing.T) {
	budgets, err := ParseRouteBudgets(strings.NewReader(testBudgetFile))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}
	results := []RouteLatency{
		{Route: "GET /api/v0/infra/resources/count", Exercised: false, Status: 403},
		{Route: "GET /api/v0/status/collectors", Exercised: true, P95: 500 * time.Millisecond},
	}
	missing := RequireNamedRoutesExercised(results, budgets)
	if len(missing) != 1 || missing[0] != "GET /api/v0/infra/resources/count" {
		t.Fatalf("RequireNamedRoutesExercised = %v, want [GET /api/v0/infra/resources/count] — a route this table explicitly budgets must not silently drop out of coverage", missing)
	}
}

func TestRequireNamedRoutesExercisedIgnoresUnnamedRoutes(t *testing.T) {
	budgets, err := ParseRouteBudgets(strings.NewReader(testBudgetFile))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}
	results := []RouteLatency{
		{Route: "GET /api/v0/some/unnamed/route", Exercised: false, Status: 400},
	}
	missing := RequireNamedRoutesExercised(results, budgets)
	if len(missing) != 0 {
		t.Fatalf("RequireNamedRoutesExercised = %v, want none — a route with no explicit budget row is allowed to be not-exercised", missing)
	}
}

// TestEveryRouteCapabilityResolvesInTheCatalog: RouteCapability is a
// hand-maintained map. If a capability id is renamed or removed, For() falls
// back to the looser configured budget with no failure, so every mapped id has
// to exist in the loaded catalog.
func TestEveryRouteCapabilityResolvesInTheCatalog(t *testing.T) {
	catalog, err := capabilitycatalog.Load()
	if err != nil {
		t.Fatalf("capabilitycatalog.Load: %v", err)
	}
	known := map[string]bool{}
	for _, e := range catalog.Entries {
		known[e.Capability] = true
	}
	for route, capability := range RouteCapability {
		if !known[capability] {
			t.Errorf("RouteCapability[%q] = %q is not a capability in the catalog", route, capability)
		}
	}
}
