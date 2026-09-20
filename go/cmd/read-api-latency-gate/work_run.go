// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// prepareWorkMeter opens a Postgres pool, proves pg_stat_statements is
// collecting, and refuses a stack that is not quiet while idle. The returned
// cleanup closes the pool. Every failure is a gate error: the work budget is
// what makes a blocking gate safe, so it is never skipped.
func prepareWorkMeter(ctx context.Context, dsn string, idle time.Duration, log io.Writer) (WorkMeter, func(), error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("connect postgres for the work meter: %w", err)
	}
	cleanup := pool.Close

	if err := EnsureWorkMeterExtension(ctx, pool); err != nil {
		cleanup()
		return nil, nil, err
	}
	meter := NewPgxWorkMeter(pool)
	rate, err := MeasureBackgroundCallRate(ctx, meter, idle)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("measure background statement rate: %w", err)
	}
	_, _ = fmt.Fprintf(log, "read-api-latency-gate: background statements while idle: %.2f/s over %s (limit %.1f/s)\n", rate, idle, maxBackgroundCallsPerSecond)
	if err := CheckMeterQuiet(rate); err != nil {
		cleanup()
		return nil, nil, err
	}
	return meter, cleanup, nil
}

// reportWorkResults prints one line per work-budget breach, showing all three
// counters against their budgets plus the route's p95 so the operator sees the
// whole picture at once, and returns the failure reasons for the run. It prints
// nothing when every metered route is within budget.
func reportWorkResults(w io.Writer, results []RouteLatency, budgets RouteWorkBudgets) []string {
	var failures []string

	if unmetered := UnmeteredExercisedRoutes(results); len(unmetered) > 0 {
		_, _ = fmt.Fprintf(w, "\nread-api-latency-gate: %d exercised route(s) were not metered: %s\n", len(unmetered), strings.Join(unmetered, ", "))
		failures = append(failures, fmt.Sprintf("%d exercised route(s) were not metered", len(unmetered)))
	}

	if breaches := EvaluateWorkBudgets(results, budgets); len(breaches) > 0 {
		_, _ = fmt.Fprintf(w, "\nread-api-latency-gate: %d route(s) exceeded their Postgres work budget:\n", len(breaches))
		for _, b := range breaches {
			_, _ = fmt.Fprintf(w, "  %s: %s, %s, %s | p95 %s\n", b.Route,
				workCounterLine("calls", b.Measured.Calls, b.Budget.Calls),
				workCounterLine("blks", b.Measured.Blks, b.Budget.Blks),
				workCounterLine("rows", b.Measured.Rows, b.Budget.Rows),
				b.P95.Round(time.Millisecond))
		}
		if unnamed := unnamedWorkBreaches(breaches, budgets); len(unnamed) > 0 {
			_, _ = fmt.Fprintf(w, "  %d of these routes have no named work budget and are held to the tight default row (%s). "+
				"If the reads are legitimate, name the route in testdata/benchmarks/read-api-route-budgets.txt, run the gate on GREEN "+
				"with -work-report, then re-render the table: bash scripts/refresh-read-api-work-budgets.sh "+
				"--out testdata/benchmarks/read-api-route-work-budgets.txt REPORT.json...\n",
				len(unnamed), strings.Join(unnamed, ", "))
		}
		failures = append(failures, fmt.Sprintf("%d route(s) exceeded their work budget", len(breaches)))
	}
	return failures
}

func workCounterLine(name string, measured float64, budget int) string {
	op := "<="
	if measured > float64(budget) {
		op = ">"
	}
	return fmt.Sprintf("%s %.1f %s %d", name, measured, op, budget)
}

// writeWorkReportFile writes the per-route work report to path, or does nothing
// when path is empty.
func writeWorkReportFile(path string, results []RouteLatency) error {
	if path == "" {
		return nil
	}
	f, err := os.Create(path) // #nosec G304 -- path is the -work-report CLI flag, operator-controlled
	if err != nil {
		return fmt.Errorf("create work report %s: %w", path, err)
	}
	if err := WriteWorkReport(f, results); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close work report %s: %w", path, err)
	}
	return nil
}

// unnamedWorkBreaches returns the routes in breaches that fall under the default
// row because the work table names no row for them.
func unnamedWorkBreaches(breaches []WorkBreach, budgets RouteWorkBudgets) []string {
	var unnamed []string
	for _, b := range breaches {
		if !budgets.Named(b.Route) {
			unnamed = append(unnamed, b.Route)
		}
	}
	return unnamed
}
