// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// TestBuildReportTreatsRecentCoordinatorFailureAsDegraded proves that when the
// recent-failure window is populated and reports active failures, the health
// state is degraded and both the recent and cumulative counts are surfaced.
func TestBuildReportTreatsRecentCoordinatorFailureAsDegraded(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 5, 21, 14, 15, 0, 0, time.UTC),
			Queue: status.QueueSnapshot{
				Outstanding: 0,
				InFlight:    0,
				Pending:     0,
				Retrying:    0,
				Failed:      0,
				DeadLetter:  0,
			},
			Coordinator: &status.CoordinatorSnapshot{
				RunStatusCounts: []status.NamedCount{
					{Name: "failed", Count: 4200},
				},
				CompletenessCounts: []status.NamedCount{
					{Name: "blocked", Count: 17},
				},
				WorkItemStatusCounts: []status.NamedCount{
					{Name: "failed_terminal", Count: 9000},
				},
				RecentFailures: &status.CoordinatorRecentFailures{
					Window:              30 * time.Minute,
					FailedRuns:          2,
					BlockedCompleteness: 1,
					TerminalWorkItems:   3,
				},
			},
		},
		status.DefaultOptions(),
	)

	if got, want := report.Health.State, "degraded"; got != want {
		t.Fatalf("BuildReport().Health.State = %q, want %q", got, want)
	}
	reasons := strings.Join(report.Health.Reasons, " ")
	// Recent failures drive the degraded state.
	if !strings.Contains(reasons, "recent failed runs=2") ||
		!strings.Contains(reasons, "recent blocked completeness=1") ||
		!strings.Contains(reasons, "recent terminal work items=3") {
		t.Fatalf("BuildReport().Health.Reasons = %v, want recent failure detail", report.Health.Reasons)
	}
	// Cumulative totals stay available as informational detail.
	if !strings.Contains(reasons, "4200") || !strings.Contains(reasons, "9000") {
		t.Fatalf("BuildReport().Health.Reasons = %v, want cumulative totals retained", report.Health.Reasons)
	}
}

// TestBuildReportTreatsAgedOnlyCoordinatorFailuresAsHealthy proves a recovered
// stack with thousands of aged all-time failures but zero recent failures
// reports healthy, while retaining cumulative counts for operator detail.
func TestBuildReportTreatsAgedOnlyCoordinatorFailuresAsHealthy(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 5, 21, 14, 15, 0, 0, time.UTC),
			Queue: status.QueueSnapshot{
				Outstanding: 0,
				InFlight:    0,
				Pending:     0,
				Retrying:    0,
				Failed:      0,
				DeadLetter:  0,
			},
			Coordinator: &status.CoordinatorSnapshot{
				// Thousands of all-time failures (e.g. expired AWS SSO) that
				// have since aged out of the recent window.
				RunStatusCounts: []status.NamedCount{
					{Name: "failed", Count: 4200},
					{Name: "complete", Count: 12},
				},
				CompletenessCounts: []status.NamedCount{
					{Name: "blocked", Count: 17},
				},
				WorkItemStatusCounts: []status.NamedCount{
					{Name: "failed_terminal", Count: 9000},
					{Name: "expired", Count: 31},
				},
				RecentFailures: &status.CoordinatorRecentFailures{
					Window:              30 * time.Minute,
					FailedRuns:          0,
					BlockedCompleteness: 0,
					TerminalWorkItems:   0,
				},
			},
		},
		status.DefaultOptions(),
	)

	if got, want := report.Health.State, "healthy"; got != want {
		t.Fatalf("BuildReport().Health.State = %q, want %q", got, want)
	}
	if got, want := strings.Join(report.Health.Reasons, " "), "no outstanding queue backlog"; got != want {
		t.Fatalf("BuildReport().Health.Reasons = %q, want %q", got, want)
	}
	// Cumulative totals must remain on the snapshot for operator detail.
	if got := report.Coordinator; got == nil || got.RunStatusCounts == nil {
		t.Fatal("BuildReport().Coordinator must retain cumulative counts")
	}
}

// TestBuildReportFallsBackToCumulativeWhenRecentWindowUnknown proves that a
// reader which does not compute a recent-failure window keeps the cumulative
// behavior so genuinely-active failures are never masked.
func TestBuildReportFallsBackToCumulativeWhenRecentWindowUnknown(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 5, 21, 14, 15, 0, 0, time.UTC),
			Queue: status.QueueSnapshot{
				Outstanding: 0,
				InFlight:    0,
				Pending:     0,
			},
			Coordinator: &status.CoordinatorSnapshot{
				RunStatusCounts: []status.NamedCount{
					{Name: "failed", Count: 1},
				},
			},
		},
		status.DefaultOptions(),
	)

	if got, want := report.Health.State, "degraded"; got != want {
		t.Fatalf("BuildReport().Health.State = %q, want %q", got, want)
	}
}
