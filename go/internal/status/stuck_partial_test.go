// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// TestBuildReportEscalatesDayStuckPartialScopeToDegraded proves the #10389
// contract: a scope whose last recorded pass was partial for 24 hours or more
// carries a status degraded reason naming the scope and the cause.
func TestBuildReportEscalatesDayStuckPartialScopeToDegraded(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: asOf,
			Queue: status.QueueSnapshot{
				Outstanding: 0,
				InFlight:    0,
				Pending:     0,
				Retrying:    0,
				Failed:      0,
			},
			SupplyChainStuckPartials: []status.SupplyChainStuckPartialScope{
				{
					ScopeID:      "scope-stuck",
					Causes:       []string{"active_expansion_rounds"},
					PartialSince: asOf.Add(-25 * time.Hour),
				},
			},
		},
		status.DefaultOptions(),
	)

	if got, want := report.Health.State, "degraded"; got != want {
		t.Fatalf("BuildReport().Health.State = %q, want %q", got, want)
	}
	reasons := strings.Join(report.Health.Reasons, " ")
	if !strings.Contains(reasons, "scope-stuck") {
		t.Fatalf("BuildReport().Health.Reasons = %v, want the stuck scope named", report.Health.Reasons)
	}
	if !strings.Contains(reasons, "active_expansion_rounds") {
		t.Fatalf("BuildReport().Health.Reasons = %v, want the partial cause named", report.Health.Reasons)
	}
}

// TestBuildReportKeepsSubDayPartialScopeHealthy proves the threshold: a scope
// partial for less than 24 hours does not degrade health on its own.
func TestBuildReportKeepsSubDayPartialScopeHealthy(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: asOf,
			SupplyChainStuckPartials: []status.SupplyChainStuckPartialScope{
				{
					ScopeID:      "scope-recent",
					Causes:       []string{"evidence_budget"},
					PartialSince: asOf.Add(-23*time.Hour - 59*time.Minute),
				},
			},
		},
		status.DefaultOptions(),
	)

	if got, want := report.Health.State, "healthy"; got != want {
		t.Fatalf("BuildReport().Health.State = %q, want %q (partial for under 24h)", got, want)
	}
}

// TestBuildReportClearsStuckPartialOnCompletePass proves the clearing half: a
// later complete pass removes the scope from the durable stuck set, so health
// no longer names it.
func TestBuildReportClearsStuckPartialOnCompletePass(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	report := status.BuildReport(
		status.RawSnapshot{
			AsOf:                     asOf,
			SupplyChainStuckPartials: nil,
		},
		status.DefaultOptions(),
	)

	if got, want := report.Health.State, "healthy"; got != want {
		t.Fatalf("BuildReport().Health.State = %q, want %q (no stuck scopes)", got, want)
	}
	for _, reason := range report.Health.Reasons {
		if strings.Contains(reason, "partial") {
			t.Fatalf("BuildReport().Health.Reasons = %v, want no partial reason after the clearing pass", report.Health.Reasons)
		}
	}
}
