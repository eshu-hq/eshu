// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestPrintReportMarksHardFailedRouteAsBreachEvenWhenFast guards against a
// real bug this gate shipped: printReport's status column compared only p95
// against budget, ignoring HardFailed, so a route that answered a 5xx in
// under a millisecond printed "OK" in the human-readable table even though
// EvaluateBudgets (and the gate's exit code) correctly failed the run on it.
// Hit live (issue #6797 live-gate run against 59c605e48): component-extensions
// and iac/resources both showed "OK" here while the same run's breach summary
// correctly reported them as failures.
func TestPrintReportMarksHardFailedRouteAsBreachEvenWhenFast(t *testing.T) {
	budgets, err := ParseRouteBudgets(strings.NewReader("default\t1500\n"))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}

	results := []RouteLatency{
		{
			Route:      "GET /api/v0/component-extensions",
			P95:        961666 * time.Nanosecond,
			Exercised:  true,
			Status:     500,
			HardFailed: true,
		},
	}

	var buf bytes.Buffer
	printReport(&buf, results, budgets)

	out := buf.String()
	if !strings.Contains(out, "BREACH") {
		t.Fatalf("printReport output does not mark the HardFailed route as BREACH:\n%s", out)
	}
	if strings.Contains(out, "OK") {
		t.Fatalf("printReport output wrongly marks the HardFailed route as OK:\n%s", out)
	}
}
