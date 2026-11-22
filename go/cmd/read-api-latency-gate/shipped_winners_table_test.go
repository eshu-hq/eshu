// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"testing"
)

// shippedWinnersWorkBudgetsPath is the committed work-budget table the winners
// leg enforces (issue #11170). The legacy leg enforces
// shippedRouteWorkBudgetsPath; both are rendered by
// scripts/refresh-read-api-work-budgets.sh, one per leg.
const shippedWinnersWorkBudgetsPath = "../../../testdata/benchmarks/read-api-route-work-budgets-winners.txt"

// TestShippedWinnersLatencyRoutesNameWorkRows is the winners-leg twin of
// TestShippedLatencyRoutesNameWorkRows: every route the latency table names
// must name a work row in the winners table too, or the leg holds it to the
// tight default row.
func TestShippedWinnersLatencyRoutesNameWorkRows(t *testing.T) {
	t.Parallel()

	latency, err := openRouteBudgets(shippedRouteBudgetsPath)
	if err != nil {
		t.Fatal(err)
	}
	work, err := openRouteWorkBudgets(shippedWinnersWorkBudgetsPath)
	if err != nil {
		t.Fatal(err)
	}
	if missing := latencyRoutesWithoutWorkRows(latency, work); len(missing) != 0 {
		t.Fatalf("latency-budgeted routes without winners work rows: %q", missing)
	}
}

// impactAggregateRoutes are the two routes the winners read model exists to
// make cheap (issue #10880 slice 2b(i)).
var impactAggregateRoutes = []string{
	"GET /api/v0/supply-chain/impact/findings/count",
	"GET /api/v0/supply-chain/impact/inventory",
}

// meteredImpactResult builds the metered result of one impact route with the
// per-request Postgres work a calibration run measured.
func meteredImpactResult(route string, blks float64) RouteLatency {
	return RouteLatency{
		Route: route, Exercised: true, Metered: true, Status: http.StatusOK,
		Work: WorkPerRequest{Calls: 2, Rows: 2, Blks: blks},
	}
}

// TestWinnersTableGuardsTheImpactReadModel is the seeded RED/GREEN pair that
// proves the winners-leg table guards the read model (#10880 AC4). The legacy
// numbers are what the two impact aggregate routes measured with the winners
// flag off on the seeded 20,000-finding corpus: 102,546 blocks per request,
// read by the read-time dedup. The winners numbers are the same routes with
// the flag on: 801 blocks. Evaluating the legacy measurement against the
// winners table must breach both routes; the winners measurement must not.
func TestWinnersTableGuardsTheImpactReadModel(t *testing.T) {
	t.Parallel()

	work, err := openRouteWorkBudgets(shippedWinnersWorkBudgetsPath)
	if err != nil {
		t.Fatal(err)
	}

	var legacy, winners []RouteLatency
	for _, route := range impactAggregateRoutes {
		legacy = append(legacy, meteredImpactResult(route, 102546))
		winners = append(winners, meteredImpactResult(route, 801))
	}

	breached := map[string]bool{}
	for _, b := range EvaluateWorkBudgets(legacy, work) {
		breached[b.Route] = true
	}
	for _, route := range impactAggregateRoutes {
		if !breached[route] {
			t.Errorf("legacy work (102546 blks) did not breach the winners table for %s: the table does not guard the read model", route)
		}
	}
	if green := EvaluateWorkBudgets(winners, work); len(green) != 0 {
		t.Errorf("winners work (801 blks) breached the winners table: %+v", green)
	}
}
