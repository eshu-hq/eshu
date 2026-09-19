// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// WorkPerRequest is the Postgres work one request to a route costs, averaged
// over the counted sample: statements (calls), rows returned or affected, and
// buffer blocks touched (shared, local and temp, read and hit). Unlike latency
// it does not depend on runner CPU speed, which is why the gate budgets it: a
// query-plan regression (the #6794 shape) multiplies buffers by an order of
// magnitude while barely moving the statement count.
type WorkPerRequest struct {
	Calls float64
	Rows  float64
	Blks  float64
}

// WorkBudget is the per-request ceiling for each counter in WorkPerRequest.
type WorkBudget struct {
	Calls int
	Blks  int
	Rows  int
}

// WorkBreach is one route whose measured work exceeded at least one budget
// counter. Exceeded names every counter over its ceiling, in calls, blks, rows
// order; P95 is carried so the report can show latency next to the work.
type WorkBreach struct {
	Route    string
	Measured WorkPerRequest
	Budget   WorkBudget
	Exceeded []string
	P95      time.Duration
}

type workBudgetRow struct {
	budget WorkBudget
	reason string
}

// RouteWorkBudgets holds the parsed per-route work budgets and the required
// default.
type RouteWorkBudgets struct {
	byRoute map[string]workBudgetRow
	def     WorkBudget
}

// For returns the budget for route: its named row, else the default.
func (b RouteWorkBudgets) For(route string) WorkBudget {
	if row, ok := b.byRoute[route]; ok {
		return row.budget
	}
	return b.def
}

// Named reports whether the table lists route explicitly instead of leaving it
// to the default row.
func (b RouteWorkBudgets) Named(route string) bool {
	_, ok := b.byRoute[route]
	return ok
}

// ParseRouteWorkBudgets reads the tab-separated work budget table:
//
//   - blank lines and lines starting with "#" are ignored.
//   - "default<TAB>calls<TAB>blks<TAB>rows" is required exactly once.
//   - every other line is "route<TAB>calls<TAB>blks<TAB>rows<TAB>reason"; the
//     reason is required so an explicit override is reviewable.
//   - a route named more than once is an error, and every number must be a
//     non-negative integer.
//
// The numbers are rendered from GREEN calibration reports by
// scripts/refresh-read-api-work-budgets.sh; they are never edited by hand.
func ParseRouteWorkBudgets(r io.Reader) (RouteWorkBudgets, error) {
	budgets := RouteWorkBudgets{byRoute: map[string]workBudgetRow{}}
	haveDefault := false

	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 4 {
			return RouteWorkBudgets{}, fmt.Errorf("work budget line %d: expected \"<route>\\t<calls>\\t<blks>\\t<rows>[\\t<reason>]\", got %q", lineNo, line)
		}
		key := strings.TrimSpace(fields[0])
		budget, err := parseWorkBudgetNumbers(fields[1:4])
		if err != nil {
			return RouteWorkBudgets{}, fmt.Errorf("work budget line %d: %w", lineNo, err)
		}

		if key == "default" {
			if haveDefault {
				return RouteWorkBudgets{}, fmt.Errorf("work budget line %d: duplicate \"default\" row", lineNo)
			}
			budgets.def = budget
			haveDefault = true
			continue
		}
		if len(fields) < 5 || strings.TrimSpace(fields[4]) == "" {
			return RouteWorkBudgets{}, fmt.Errorf("work budget line %d: route %q requires a fifth \\t-separated reason column", lineNo, key)
		}
		if _, dup := budgets.byRoute[key]; dup {
			return RouteWorkBudgets{}, fmt.Errorf("work budget line %d: duplicate route %q", lineNo, key)
		}
		budgets.byRoute[key] = workBudgetRow{budget: budget, reason: strings.TrimSpace(fields[4])}
	}
	if err := scanner.Err(); err != nil {
		return RouteWorkBudgets{}, fmt.Errorf("read work budgets: %w", err)
	}
	if !haveDefault {
		return RouteWorkBudgets{}, fmt.Errorf("work budget table missing required \"default\" row")
	}
	return budgets, nil
}

func parseWorkBudgetNumbers(fields []string) (WorkBudget, error) {
	values := make([]int, len(fields))
	for i, f := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			return WorkBudget{}, fmt.Errorf("non-numeric budget %q: %w", f, err)
		}
		if n < 0 {
			return WorkBudget{}, fmt.Errorf("negative budget %d", n)
		}
		values[i] = n
	}
	return WorkBudget{Calls: values[0], Blks: values[1], Rows: values[2]}, nil
}

// EvaluateWorkBudgets returns one WorkBreach per exercised, metered route whose
// measured work exceeds any counter of its budget, in results order. A route
// that was not exercised carries no work evidence and is skipped; an exercised
// route the meter never read is reported separately by
// UnmeteredExercisedRoutes, not folded in here.
func EvaluateWorkBudgets(results []RouteLatency, budgets RouteWorkBudgets) []WorkBreach {
	var breaches []WorkBreach
	for _, r := range results {
		if !r.Exercised || !r.Metered {
			continue
		}
		budget := budgets.For(r.Route)
		var exceeded []string
		if r.Work.Calls > float64(budget.Calls) {
			exceeded = append(exceeded, "calls")
		}
		if r.Work.Blks > float64(budget.Blks) {
			exceeded = append(exceeded, "blks")
		}
		if r.Work.Rows > float64(budget.Rows) {
			exceeded = append(exceeded, "rows")
		}
		if len(exceeded) > 0 {
			breaches = append(breaches, WorkBreach{Route: r.Route, Measured: r.Work, Budget: budget, Exceeded: exceeded, P95: r.P95})
		}
	}
	return breaches
}

// UnmeteredExercisedRoutes returns every exercised route whose work counters
// were never read. The work gate must never pass a route on latency alone
// because its meter was skipped, so the caller fails the run on any entry.
func UnmeteredExercisedRoutes(results []RouteLatency) []string {
	var unmetered []string
	for _, r := range results {
		if r.Exercised && !r.Metered {
			unmetered = append(unmetered, r.Route)
		}
	}
	return unmetered
}
