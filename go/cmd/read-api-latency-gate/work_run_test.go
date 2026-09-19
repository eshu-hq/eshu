// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestReportWorkResultsPrintsAllThreeCountersAndP95ForABreach(t *testing.T) {
	budgets, _ := ParseRouteWorkBudgets(strings.NewReader("default\t35\t20000\t5000\n"))
	results := []RouteLatency{{
		Route: "GET /r", Exercised: true, Metered: true, P95: 700 * time.Millisecond,
		Work: WorkPerRequest{Calls: 25, Blks: 2_000_000, Rows: 70_000},
	}}

	var buf bytes.Buffer
	failures := reportWorkResults(&buf, results, budgets)

	out := buf.String()
	for _, want := range []string{"GET /r", "calls 25.0 <= 35", "blks 2000000.0 > 20000", "rows 70000.0 > 5000", "p95 700ms"} {
		if !strings.Contains(out, want) {
			t.Errorf("breach output missing %q:\n%s", want, out)
		}
	}
	if len(failures) != 1 {
		t.Fatalf("failures = %v, want exactly one work-budget failure", failures)
	}
}

func TestReportWorkResultsFailsAnExercisedRouteTheMeterNeverRead(t *testing.T) {
	budgets, _ := ParseRouteWorkBudgets(strings.NewReader("default\t35\t20000\t5000\n"))
	results := []RouteLatency{{Route: "GET /r", Exercised: true}}

	var buf bytes.Buffer
	failures := reportWorkResults(&buf, results, budgets)

	if len(failures) != 1 || !strings.Contains(failures[0], "not metered") {
		t.Fatalf("failures = %v, want one 'not metered' failure: a route must never pass on latency alone because its meter was skipped", failures)
	}
}

func TestReportWorkResultsIsQuietWhenEverythingIsWithinBudget(t *testing.T) {
	budgets, _ := ParseRouteWorkBudgets(strings.NewReader("default\t35\t20000\t5000\n"))
	results := []RouteLatency{{
		Route: "GET /r", Exercised: true, Metered: true,
		Work: WorkPerRequest{Calls: 25, Blks: 6000, Rows: 1200},
	}}

	var buf bytes.Buffer
	if failures := reportWorkResults(&buf, results, budgets); len(failures) != 0 || buf.Len() != 0 {
		t.Fatalf("failures = %v, output = %q, want none", failures, buf.String())
	}
}
