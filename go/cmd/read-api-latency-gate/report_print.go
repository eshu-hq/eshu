// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"time"
)

func printReport(w io.Writer, results []RouteLatency, budgets RouteBudgets, workBudgets RouteWorkBudgets) {
	workBreached := make(map[string]bool)
	for _, b := range EvaluateWorkBudgets(results, workBudgets) {
		workBreached[b.Route] = true
	}
	for _, route := range UnmeteredExercisedRoutes(results) {
		workBreached[route] = true
	}
	namedMissing := make(map[string]bool)
	for _, route := range RequireNamedRoutesExercised(results, budgets) {
		namedMissing[route] = true
	}
	_, _ = fmt.Fprintf(w, "%-70s %10s %10s %13s %8s %10s %8s\n", "route", "p95", "budget", "status", "calls", "blks", "rows")
	for _, r := range results {
		if !r.Exercised {
			status := fmt.Sprintf("NOT_EXERCISED(%d)", r.Status)
			if namedMissing[r.Route] {
				// A route the latency table budgets by name must be exercised
				// (RequireNamedRoutesExercised fails the run otherwise).
				status += " BREACH"
			}
			_, _ = fmt.Fprintf(w, "%-70s %10s %10s %13s\n", r.Route, "-", "-", status)
			continue
		}
		budget := budgets.For(r.Route)
		status := "OK"
		// A HardFailed route (any 5xx sample) always breaches regardless of
		// p95 (see EvaluateBudgets) -- this status column must agree with
		// that verdict, not just compare p95 against budget, or a route the
		// gate actually fails on prints as a false "OK" here (issue #6797
		// live-gate incident: component-extensions and iac/resources showed
		// "OK" in this table while the same run's breach summary correctly
		// failed the gate on them). The same holds for the work budget: a
		// route over its Postgres work budget, or one the meter never read,
		// fails the run through EvaluateWorkBudgets and
		// UnmeteredExercisedRoutes, so it is marked here too.
		switch {
		case r.HardFailed:
			status = "BREACH"
		case workBreached[r.Route]:
			// Checked before the latency-exemption case: a route can be both
			// exempt from its ceiling and over its work budget, and the work
			// budget is this gate's actual regression-catching mechanism, so
			// that must never read as the advisory BREACH-EXEMPT below.
			status = "BREACH"
		case r.P95 > budget:
			// LatencyExemptions makes this ceiling advisory for a tracked
			// route (see EvaluateBudgets) only when nothing else about the
			// route has failed; the column still names it, so a reader never
			// mistakes BREACH-EXEMPT for a pass.
			if ex, exempt := LatencyExemptions[r.Route]; exempt {
				status = fmt.Sprintf("BREACH-EXEMPT(%s)", ex.Issue)
			} else {
				status = "BREACH"
			}
		}
		calls, blks, rows := "-", "-", "-"
		if r.Metered {
			calls = fmt.Sprintf("%.1f", r.Work.Calls)
			blks = fmt.Sprintf("%.0f", r.Work.Blks)
			rows = fmt.Sprintf("%.0f", r.Work.Rows)
		}
		_, _ = fmt.Fprintf(w, "%-70s %10s %10s %13s %8s %10s %8s\n", r.Route, r.P95.Round(time.Millisecond), budget, status, calls, blks, rows)
	}
}
