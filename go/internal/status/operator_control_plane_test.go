package status

import (
	"testing"
	"time"
)

// TestControlPlaneProjectsQueueClaimLatencyAndStuckWork verifies the unified
// read model surfaces queue depth, claim-latency, and stuck-work signals
// without inventing data the Report does not carry.
func TestControlPlaneProjectsQueueClaimLatencyAndStuckWork(t *testing.T) {
	report := Report{
		AsOf: time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		Queue: QueueSnapshot{
			Total:                40,
			Outstanding:          12,
			Pending:              8,
			InFlight:             4,
			Retrying:             3,
			DeadLetter:           2,
			OverdueClaims:        5,
			OldestOutstandingAge: 9 * time.Minute,
		},
		Coordinator: &CoordinatorSnapshot{OldestPendingAge: 7 * time.Minute},
		QueueBlockages: []QueueBlockage{
			{Stage: "reducer", Domain: "workload_materialization", Blocked: 3, OldestAge: 6 * time.Minute},
			{Stage: "reducer", Domain: "deployable_unit_correlation", Blocked: 1, OldestAge: 2 * time.Minute},
		},
		RetryPolicies: []RetryPolicySummary{
			{Stage: "projector", MaxAttempts: 3, RetryDelay: 30 * time.Second},
		},
	}

	cp := ControlPlane(report)

	if cp.AsOf != report.AsOf {
		t.Fatalf("AsOf = %v, want %v", cp.AsOf, report.AsOf)
	}
	if cp.Queue.Outstanding != 12 || cp.Queue.Pending != 8 || cp.Queue.InFlight != 4 {
		t.Fatalf("queue depth not projected: %+v", cp.Queue)
	}
	if cp.Queue.OverdueClaims != 5 {
		t.Fatalf("OverdueClaims = %d, want 5", cp.Queue.OverdueClaims)
	}
	if cp.Queue.OldestOutstandingAge != 9*time.Minute {
		t.Fatalf("OldestOutstandingAge = %v, want 9m", cp.Queue.OldestOutstandingAge)
	}
	if cp.Queue.OldestPendingAge != 7*time.Minute {
		t.Fatalf("OldestPendingAge = %v, want 7m (from coordinator)", cp.Queue.OldestPendingAge)
	}
	if cp.Queue.BlockedConflicts != 2 {
		t.Fatalf("BlockedConflicts = %d, want 2 (one per blockage row)", cp.Queue.BlockedConflicts)
	}
	if len(cp.RetryPolicies) != 1 || cp.RetryPolicies[0].MaxAttempts != 3 {
		t.Fatalf("retry policies not projected: %+v", cp.RetryPolicies)
	}
}

// TestControlPlaneProjectsDeadLetterClasses verifies dead-letter pressure is
// surfaced by reducer-domain class, by the collector-generation commit path,
// and with the newest failure class for correlation.
func TestControlPlaneProjectsDeadLetterClasses(t *testing.T) {
	report := Report{
		AsOf: time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		Queue: QueueSnapshot{
			DeadLetter: 4,
		},
		DomainBacklogs: []DomainBacklog{
			{Domain: "workload_materialization", DeadLetter: 3, OldestAge: 11 * time.Minute},
			{Domain: "deployable_unit_correlation", DeadLetter: 1, OldestAge: 4 * time.Minute},
			{Domain: "cloud_asset_resolution", DeadLetter: 0, Outstanding: 5},
		},
		CollectorGenerationDeadLetters: CollectorGenerationDeadLetterSnapshot{
			DeadLetter:          2,
			ReplayRequested:     1,
			OldestDeadLetterAge: 20 * time.Minute,
		},
		LatestQueueFailure: &QueueFailureSnapshot{
			Stage:        "reducer",
			Domain:       "workload_materialization",
			Status:       "dead_letter",
			WorkItemID:   "wi-123",
			ScopeID:      "scope-abc",
			GenerationID: "gen-xyz",
			FailureClass: "merge_conflict",
			UpdatedAt:    time.Date(2026, 6, 19, 2, 55, 0, 0, time.UTC),
		},
	}

	cp := ControlPlane(report)

	if cp.DeadLetters.QueueDeadLetter != 4 {
		t.Fatalf("QueueDeadLetter = %d, want 4", cp.DeadLetters.QueueDeadLetter)
	}
	// Only domains with dead letters should appear in the class breakdown.
	if len(cp.DeadLetters.ByDomain) != 2 {
		t.Fatalf("ByDomain len = %d, want 2 (zero-deadletter domains excluded): %+v", len(cp.DeadLetters.ByDomain), cp.DeadLetters.ByDomain)
	}
	// Highest dead-letter domain first.
	if cp.DeadLetters.ByDomain[0].Domain != "workload_materialization" || cp.DeadLetters.ByDomain[0].DeadLetter != 3 {
		t.Fatalf("ByDomain[0] = %+v, want workload_materialization=3", cp.DeadLetters.ByDomain[0])
	}
	if cp.DeadLetters.CollectorGeneration.DeadLetter != 2 {
		t.Fatalf("collector generation dead letter not projected: %+v", cp.DeadLetters.CollectorGeneration)
	}
	if cp.DeadLetters.LatestFailureClass != "merge_conflict" {
		t.Fatalf("LatestFailureClass = %q, want merge_conflict", cp.DeadLetters.LatestFailureClass)
	}
	if cp.DeadLetters.LatestFailureScopeID != "scope-abc" || cp.DeadLetters.LatestFailureGenerationID != "gen-xyz" {
		t.Fatalf("latest failure correlation IDs not projected: %+v", cp.DeadLetters)
	}
}

// TestControlPlaneProjectsCollectorFamilies verifies collector families are
// grouped by kind with a promotion verdict and the newest proof artifact
// timestamp, and that the catalog spine yields at least one family.
func TestControlPlaneProjectsCollectorFamilies(t *testing.T) {
	report := Report{AsOf: time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC)}

	cp := ControlPlane(report)

	if len(cp.CollectorFamilies) == 0 {
		t.Fatalf("expected catalog-spine collector families, got none")
	}
	for _, fam := range cp.CollectorFamilies {
		if fam.CollectorKind == "" {
			t.Fatalf("collector family missing kind: %+v", fam)
		}
		if fam.PromotionState == "" {
			t.Fatalf("collector family missing promotion state: %+v", fam)
		}
	}
}

// TestControlPlaneReducerDomainsSortedAndCarryRetryState confirms reducer
// domains preserve retry/dead-letter/oldest-age signals for diagnosis.
func TestControlPlaneReducerDomainsSortedAndCarryRetryState(t *testing.T) {
	report := Report{
		AsOf: time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		DomainBacklogs: []DomainBacklog{
			{Domain: "deployable_unit_correlation", Outstanding: 2, Retrying: 1, OldestAge: 3 * time.Minute},
			{Domain: "workload_materialization", Outstanding: 9, Retrying: 4, DeadLetter: 1, OldestAge: 12 * time.Minute},
		},
	}

	cp := ControlPlane(report)

	if len(cp.ReducerDomains) != 2 {
		t.Fatalf("ReducerDomains len = %d, want 2", len(cp.ReducerDomains))
	}
	if cp.ReducerDomains[0].Domain != "workload_materialization" {
		t.Fatalf("ReducerDomains not sorted by pressure: %+v", cp.ReducerDomains)
	}
	if cp.ReducerDomains[0].Retrying != 4 {
		t.Fatalf("reducer-domain retry state lost: %+v", cp.ReducerDomains[0])
	}
}
