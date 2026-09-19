// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
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
	printReport(&buf, results, budgets, RouteWorkBudgets{})

	out := buf.String()
	if !strings.Contains(out, "BREACH") {
		t.Fatalf("printReport output does not mark the HardFailed route as BREACH:\n%s", out)
	}
	if strings.Contains(out, "OK") {
		t.Fatalf("printReport output wrongly marks the HardFailed route as OK:\n%s", out)
	}
}

// reportStatus returns the status column of the printReport line for route.
func reportStatus(t *testing.T, out, route string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, route+" ") {
			fields := strings.Fields(line)
			// route (2 tokens: method + path), p95, budget, status, ...
			if len(fields) < 5 {
				t.Fatalf("report line for %s has too few columns: %q", route, line)
			}
			return fields[4]
		}
	}
	t.Fatalf("no report line for %s in:\n%s", route, out)
	return ""
}

// TestPrintReportMarksWorkOnlyBreachAsBreach guards the same false-OK class as
// the HardFailed case for the work budget: a route that is fast but reads far
// more Postgres buffers than its budget fails the gate through
// EvaluateWorkBudgets, so the table must not print it as OK. The live RED run
// against 59c605e48 printed OK for all 14 routes the gate failed on.
func TestPrintReportMarksWorkOnlyBreachAsBreach(t *testing.T) {
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
			Route: "GET /api/v0/heavy", P95: 5 * time.Millisecond, Exercised: true, Status: 200, Metered: true,
			Work: WorkPerRequest{Calls: 1, Blks: float64(def.Blks) * 3, Rows: 1},
		},
		{
			Route: "GET /api/v0/light", P95: 5 * time.Millisecond, Exercised: true, Status: 200, Metered: true,
			Work: WorkPerRequest{Calls: 1, Blks: 1, Rows: 1},
		},
	}

	var buf bytes.Buffer
	printReport(&buf, results, budgets, workBudgets)

	out := buf.String()
	if got := reportStatus(t, out, "GET /api/v0/heavy"); got != "BREACH" {
		t.Errorf("work-only breach printed %q, want BREACH:\n%s", got, out)
	}
	if got := reportStatus(t, out, "GET /api/v0/light"); got != "OK" {
		t.Errorf("within-budget route printed %q, want OK:\n%s", got, out)
	}
}

// TestPrintReportMarksUnmeteredExercisedRouteAsBreach covers the other work
// gate verdict: an exercised route the meter never read fails the run, so it
// must not print as OK either.
func TestPrintReportMarksUnmeteredExercisedRouteAsBreach(t *testing.T) {
	budgets, err := ParseRouteBudgets(strings.NewReader("default\t1500\n"))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}
	workBudgets, err := ParseRouteWorkBudgets(strings.NewReader(testWorkBudgetFile))
	if err != nil {
		t.Fatalf("ParseRouteWorkBudgets: %v", err)
	}
	results := []RouteLatency{
		{Route: "GET /api/v0/unmetered", P95: 5 * time.Millisecond, Exercised: true, Status: 200},
	}

	var buf bytes.Buffer
	printReport(&buf, results, budgets, workBudgets)

	if got := reportStatus(t, buf.String(), "GET /api/v0/unmetered"); got != "BREACH" {
		t.Errorf("unmetered exercised route printed %q, want BREACH:\n%s", got, buf.String())
	}
}

// TestReadTableFingerprintsTheBytesItReturns pins the line each run prints for
// the budget tables it enforced: the digest is taken from the same bytes that
// are then parsed, so a table edited mid-run cannot make the log lie.
func TestReadTableFingerprintsTheBytesItReturns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "budgets.txt")
	content := []byte("default\t1500\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write table: %v", err)
	}
	sum := sha256.Sum256(content)
	want := path + " sha256:" + hex.EncodeToString(sum[:])

	table, err := readTable(path)
	if err != nil {
		t.Fatalf("readTable: %v", err)
	}
	if got := table.provenance(); got != want {
		t.Errorf("provenance = %q, want %q", got, want)
	}
	parsed, err := ParseRouteBudgets(table.reader())
	if err != nil {
		t.Fatalf("ParseRouteBudgets from the returned bytes: %v", err)
	}
	if got := parsed.For("GET /api/v0/anything"); got != 1500*time.Millisecond {
		t.Errorf("parsed default = %s, want 1.5s from the same bytes", got)
	}
	if _, err := readTable(path + ".missing"); err == nil {
		t.Errorf("readTable on a missing file returned no error")
	}
}

// TestPrintReportMarksNamedNotExercisedRouteAsBreach covers the remaining
// per-route verdict: a route the latency table budgets by name that comes back
// not-exercised fails the run (RequireNamedRoutesExercised), so its row must
// say so, while an unbudgeted not-exercised route stays a plain NOT_EXERCISED.
func TestPrintReportMarksNamedNotExercisedRouteAsBreach(t *testing.T) {
	budgets, err := ParseRouteBudgets(strings.NewReader("default\t1500\nGET /api/v0/named\t2000\tno catalog capability mapped\n"))
	if err != nil {
		t.Fatalf("ParseRouteBudgets: %v", err)
	}
	workBudgets, err := ParseRouteWorkBudgets(strings.NewReader(testWorkBudgetFile))
	if err != nil {
		t.Fatalf("ParseRouteWorkBudgets: %v", err)
	}
	results := []RouteLatency{
		{Route: "GET /api/v0/named", Exercised: false, Status: 403},
		{Route: "GET /api/v0/unbudgeted", Exercised: false, Status: 400},
	}

	var buf bytes.Buffer
	printReport(&buf, results, budgets, workBudgets)

	var named, unbudgeted string
	for _, line := range strings.Split(buf.String(), "\n") {
		switch {
		case strings.HasPrefix(line, "GET /api/v0/named "):
			named = line
		case strings.HasPrefix(line, "GET /api/v0/unbudgeted "):
			unbudgeted = line
		}
	}
	if !strings.Contains(named, "NOT_EXERCISED(403)") || !strings.Contains(named, "BREACH") {
		t.Errorf("named not-exercised route line = %q, want NOT_EXERCISED(403) and BREACH", named)
	}
	if !strings.Contains(unbudgeted, "NOT_EXERCISED(400)") || strings.Contains(unbudgeted, "BREACH") {
		t.Errorf("unbudgeted not-exercised route line = %q, want NOT_EXERCISED(400) without BREACH", unbudgeted)
	}
}
