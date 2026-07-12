// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"testing"
	"time"
)

// TestOperationsComposesSnapshotSectionsAndLiveActivity verifies the
// operations-board read model carries health, collector heartbeat, stage
// summaries, domain backlogs, and queue depth straight from an already-loaded
// Report, and joins in the separately-fetched live-activity rows with their
// truncation/limit metadata.
func TestOperationsComposesSnapshotSectionsAndLiveActivity(t *testing.T) {
	asOf := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	report := Report{
		AsOf:   asOf,
		Health: HealthSummary{State: "degraded", Reasons: []string{"queue_backlog"}},
		Queue: QueueSnapshot{
			Total:       40,
			Outstanding: 12,
			InFlight:    4,
		},
		StageSummaries: []StageSummary{
			{Stage: "reducer", Pending: 3, Claimed: 2},
		},
		DomainBacklogs: []DomainBacklog{
			{Domain: "workload_materialization", Outstanding: 5, OldestAge: 6 * time.Minute},
		},
		Coordinator: &CoordinatorSnapshot{
			CollectorInstances: []CollectorInstanceSummary{
				{
					InstanceID:     "github-1",
					CollectorKind:  "github",
					Enabled:        true,
					ClaimsEnabled:  true,
					LastObservedAt: asOf.Add(-30 * time.Second),
				},
			},
		},
	}
	activity := []LiveActivityRow{
		{WorkItemID: "wi-1", Stage: "reducer", Status: "claimed", UpdatedAt: asOf.Add(-5 * time.Second)},
	}

	got := Operations(report, activity, true, 100)

	if got.AsOf != asOf {
		t.Fatalf("AsOf = %v, want %v", got.AsOf, asOf)
	}
	if got.Health.State != "degraded" {
		t.Fatalf("Health.State = %q, want degraded", got.Health.State)
	}
	if len(got.Collectors) != 1 || got.Collectors[0].InstanceID != "github-1" {
		t.Fatalf("Collectors not projected: %+v", got.Collectors)
	}
	if got.Collectors[0].LastObservedAt != asOf.Add(-30*time.Second) {
		t.Fatalf("Collectors[0].LastObservedAt = %v, want heartbeat carried through", got.Collectors[0].LastObservedAt)
	}
	if len(got.StageSummaries) != 1 || got.StageSummaries[0].Stage != "reducer" {
		t.Fatalf("StageSummaries not projected: %+v", got.StageSummaries)
	}
	if len(got.DomainBacklogs) != 1 || got.DomainBacklogs[0].Domain != "workload_materialization" {
		t.Fatalf("DomainBacklogs not projected: %+v", got.DomainBacklogs)
	}
	if got.Queue.Outstanding != 12 {
		t.Fatalf("Queue.Outstanding = %d, want 12", got.Queue.Outstanding)
	}
	if len(got.LiveActivity) != 1 || got.LiveActivity[0].WorkItemID != "wi-1" {
		t.Fatalf("LiveActivity not projected: %+v", got.LiveActivity)
	}
	if !got.Truncated {
		t.Fatal("Truncated = false, want true")
	}
	if got.Limit != 100 {
		t.Fatalf("Limit = %d, want 100", got.Limit)
	}
}

// TestOperationsClonesSlicesSoCallerNeverAliasesReport verifies the returned
// slices are copies, not aliases of the caller's Report or activity slices --
// mutating the projection must never corrupt the source Report the way a
// shared-slice aggregate would.
func TestOperationsClonesSlicesSoCallerNeverAliasesReport(t *testing.T) {
	report := Report{
		StageSummaries: []StageSummary{{Stage: "reducer", Pending: 1}},
		DomainBacklogs: []DomainBacklog{{Domain: "workload_materialization", Outstanding: 1}},
	}
	activity := []LiveActivityRow{{WorkItemID: "wi-1"}}

	got := Operations(report, activity, false, 50)

	got.StageSummaries[0].Pending = 999
	got.DomainBacklogs[0].Outstanding = 999
	got.LiveActivity[0].WorkItemID = "mutated"

	if report.StageSummaries[0].Pending != 1 {
		t.Fatalf("mutating projection aliased report.StageSummaries: got %d", report.StageSummaries[0].Pending)
	}
	if report.DomainBacklogs[0].Outstanding != 1 {
		t.Fatalf("mutating projection aliased report.DomainBacklogs: got %d", report.DomainBacklogs[0].Outstanding)
	}
	if activity[0].WorkItemID != "wi-1" {
		t.Fatalf("mutating projection aliased the input activity slice: got %q", activity[0].WorkItemID)
	}
}

// TestOperationsHandlesEmptyLiveActivity verifies an empty in-flight set
// projects to a nil (not panicking, not falsely truncated) LiveActivity.
func TestOperationsHandlesEmptyLiveActivity(t *testing.T) {
	got := Operations(Report{}, nil, false, 100)

	if got.LiveActivity != nil {
		t.Fatalf("LiveActivity = %+v, want nil for empty activity", got.LiveActivity)
	}
	if got.Truncated {
		t.Fatal("Truncated = true, want false")
	}
}

// TestLiveActivityRowAge verifies Age computes a non-negative duration and
// never goes negative from clock skew between asOf and UpdatedAt.
func TestLiveActivityRowAge(t *testing.T) {
	asOf := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		row  LiveActivityRow
		want time.Duration
	}{
		{
			name: "normal age",
			row:  LiveActivityRow{UpdatedAt: asOf.Add(-90 * time.Second)},
			want: 90 * time.Second,
		},
		{
			name: "zero UpdatedAt",
			row:  LiveActivityRow{},
			want: 0,
		},
		{
			name: "UpdatedAt after asOf (clock skew)",
			row:  LiveActivityRow{UpdatedAt: asOf.Add(5 * time.Second)},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.row.Age(asOf); got != tt.want {
				t.Fatalf("Age() = %v, want %v", got, tt.want)
			}
		})
	}
}
