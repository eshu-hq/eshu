// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
	"time"
)

const testWorkBudgetFile = `# comment
default	35	20000	5000

GET /api/v0/status/collectors	35	80000	400	status family: GREEN-derived guard
GET /api/v0/iac/resources	10	220000	120	iac inventory: GREEN-derived guard
`

func TestParseRouteWorkBudgetsReadsDefaultAndNamedRows(t *testing.T) {
	b, err := ParseRouteWorkBudgets(strings.NewReader(testWorkBudgetFile))
	if err != nil {
		t.Fatalf("ParseRouteWorkBudgets: %v", err)
	}
	if got, want := b.For("GET /api/v0/unlisted"), (WorkBudget{Calls: 35, Blks: 20000, Rows: 5000}); got != want {
		t.Errorf("default budget = %+v, want %+v", got, want)
	}
	if got, want := b.For("GET /api/v0/status/collectors"), (WorkBudget{Calls: 35, Blks: 80000, Rows: 400}); got != want {
		t.Errorf("named budget = %+v, want %+v", got, want)
	}
	if !b.Named("GET /api/v0/iac/resources") || b.Named("GET /api/v0/unlisted") {
		t.Errorf("Named() must be true only for explicitly listed routes")
	}
}

func TestParseRouteWorkBudgetsRejectsMalformedTables(t *testing.T) {
	cases := map[string]string{
		"missing default":       "GET /a\t1\t2\t3\treason\n",
		"duplicate default":     "default\t1\t2\t3\ndefault\t1\t2\t3\n",
		"named row no reason":   "default\t1\t2\t3\nGET /a\t1\t2\t3\n",
		"duplicate named route": "default\t1\t2\t3\nGET /a\t1\t2\t3\tr\nGET /a\t1\t2\t3\tr\n",
		"non-numeric":           "default\t1\tx\t3\n",
		"negative":              "default\t1\t-2\t3\n",
		"too few fields":        "default\t1\t2\n",
	}
	for name, body := range cases {
		if _, err := ParseRouteWorkBudgets(strings.NewReader(body)); err == nil {
			t.Errorf("%s: ParseRouteWorkBudgets accepted a malformed table", name)
		}
	}
}

func TestEvaluateWorkBudgetsNamesEveryExceededCounter(t *testing.T) {
	b, err := ParseRouteWorkBudgets(strings.NewReader("default\t35\t20000\t5000\n"))
	if err != nil {
		t.Fatal(err)
	}
	results := []RouteLatency{{
		Route: "GET /r", Exercised: true, Metered: true, P95: 700 * time.Millisecond,
		Work: WorkPerRequest{Calls: 25, Blks: 2_000_000, Rows: 70_000},
	}}

	breaches := EvaluateWorkBudgets(results, b)

	if len(breaches) != 1 {
		t.Fatalf("breaches = %d, want 1", len(breaches))
	}
	if got := strings.Join(breaches[0].Exceeded, ","); got != "blks,rows" {
		t.Errorf("Exceeded = %q, want blks,rows (calls 25 is within 35)", got)
	}
}

func TestEvaluateWorkBudgetsPassesWithinBudget(t *testing.T) {
	b, _ := ParseRouteWorkBudgets(strings.NewReader("default\t35\t20000\t5000\n"))
	results := []RouteLatency{{
		Route: "GET /r", Exercised: true, Metered: true,
		Work: WorkPerRequest{Calls: 25, Blks: 6000, Rows: 1200},
	}}
	if got := EvaluateWorkBudgets(results, b); len(got) != 0 {
		t.Fatalf("breaches = %+v, want none", got)
	}
}

func TestEvaluateWorkBudgetsSkipsNotExercisedRoutes(t *testing.T) {
	b, _ := ParseRouteWorkBudgets(strings.NewReader("default\t1\t1\t1\n"))
	results := []RouteLatency{{Route: "GET /r", Exercised: false, Status: 400}}
	if got := EvaluateWorkBudgets(results, b); len(got) != 0 {
		t.Fatalf("a not-exercised route carries no work evidence, got breaches %+v", got)
	}
}

func TestUnmeteredExercisedRoutesFlagsAnExercisedRouteWithoutCounters(t *testing.T) {
	results := []RouteLatency{
		{Route: "GET /metered", Exercised: true, Metered: true},
		{Route: "GET /unmetered", Exercised: true},
		{Route: "GET /skipped", Exercised: false},
	}
	got := UnmeteredExercisedRoutes(results)
	if len(got) != 1 || got[0] != "GET /unmetered" {
		t.Fatalf("UnmeteredExercisedRoutes = %v, want [GET /unmetered]: a route the meter never read must fail the run, not pass silently", got)
	}
}
