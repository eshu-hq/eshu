package query

import (
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func causeStatus(t *testing.T, fc FreshnessCausality, cause FreshnessCause) FreshnessCauseStatus {
	t.Helper()
	for _, c := range fc.Causes {
		if c.Cause == cause {
			return c
		}
	}
	t.Fatalf("cause %q not present in causality projection", cause)
	return FreshnessCauseStatus{}
}

func TestFreshnessCausalityFreshWhenNoSignals(t *testing.T) {
	fc := freshnessCausalityFromReport(statuspkg.Report{
		AsOf:              time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		GenerationHistory: statuspkg.GenerationHistorySnapshot{Active: 5},
	})
	if fc.State != "fresh" {
		t.Fatalf("state = %q, want fresh", fc.State)
	}
	// All seven closed causes must be enumerated for the dashboard.
	if len(fc.Causes) != 7 {
		t.Fatalf("causes len = %d, want 7", len(fc.Causes))
	}
	for _, c := range fc.Causes {
		if c.Observed {
			t.Fatalf("no cause should be observed on a fresh run: %+v", c)
		}
		if c.NextCheck.Reason == "" {
			t.Fatalf("every cause must carry drilldown guidance: %+v", c)
		}
	}
}

func TestFreshnessCausalityBuildingOnPendingGenerationAndBacklog(t *testing.T) {
	fc := freshnessCausalityFromReport(statuspkg.Report{
		AsOf:              time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		GenerationHistory: statuspkg.GenerationHistorySnapshot{Active: 3, Pending: 2},
		DomainBacklogs: []statuspkg.DomainBacklog{
			{Domain: "workload_materialization", Outstanding: 7},
		},
	})
	if fc.State != "building" {
		t.Fatalf("state = %q, want building (catch-up work)", fc.State)
	}
	if !causeStatus(t, fc, FreshnessCausePendingRepoGeneration).Observed {
		t.Fatalf("pending_repo_generation should be observed")
	}
	if !causeStatus(t, fc, FreshnessCauseReducerBacklog).Observed {
		t.Fatalf("reducer_backlog should be observed")
	}
	if fc.PendingProjection.Outstanding != 7 {
		t.Fatalf("pending projection outstanding = %d, want 7", fc.PendingProjection.Outstanding)
	}
}

func TestFreshnessCausalityAggregatesPendingProjectionBeforeDomainCap(t *testing.T) {
	raw := statuspkg.RawSnapshot{
		AsOf: time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		DomainBacklogs: []statuspkg.DomainBacklog{
			{Domain: "domain-1", Outstanding: 1, DeadLetter: 1},
			{Domain: "domain-2", Outstanding: 2, DeadLetter: 1},
			{Domain: "domain-3", Outstanding: 3, DeadLetter: 1},
			{Domain: "domain-4", Outstanding: 4, DeadLetter: 1},
			{Domain: "domain-5", Outstanding: 5, DeadLetter: 1},
			{Domain: "domain-6", Outstanding: 6, DeadLetter: 1},
		},
	}
	report := statuspkg.BuildReport(raw, statuspkg.DefaultOptions())
	if len(report.DomainBacklogs) != statuspkg.DefaultOptions().DomainLimit {
		t.Fatalf("test setup expected capped report domains = %d, got %d", statuspkg.DefaultOptions().DomainLimit, len(report.DomainBacklogs))
	}

	fc := freshnessCausalityFromRawAndReport(raw, report)
	if fc.PendingProjection.Outstanding != 21 {
		t.Fatalf("pending projection outstanding = %d, want all raw domains sum 21", fc.PendingProjection.Outstanding)
	}
	if fc.PendingProjection.DeadLetter != 6 {
		t.Fatalf("pending projection dead_letter = %d, want all raw domains sum 6", fc.PendingProjection.DeadLetter)
	}
	if fc.PendingProjection.Domains != 6 {
		t.Fatalf("pending projection domains = %d, want all raw domains count 6", fc.PendingProjection.Domains)
	}
}

func TestFreshnessCausalityStaleOnDeadLetter(t *testing.T) {
	fc := freshnessCausalityFromReport(statuspkg.Report{
		AsOf: time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		DomainBacklogs: []statuspkg.DomainBacklog{
			{Domain: "deployable_unit_correlation", DeadLetter: 2},
		},
	})
	if fc.State != "stale" {
		t.Fatalf("state = %q, want stale (stuck dead letters)", fc.State)
	}
	if !causeStatus(t, fc, FreshnessCauseDeadLetteredDomain).Observed {
		t.Fatalf("dead_lettered_domain should be observed")
	}
}

func TestFreshnessCausalityPerAnswerCausesNotClusterObserved(t *testing.T) {
	fc := freshnessCausalityFromReport(statuspkg.Report{AsOf: time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC)})
	for _, cause := range []FreshnessCause{
		FreshnessCauseContentCoverageUnavailable,
		FreshnessCauseUnsupportedProfile,
		FreshnessCauseRetentionExpired,
	} {
		c := causeStatus(t, fc, cause)
		if c.Observability != "per_answer" {
			t.Fatalf("cause %q observability = %q, want per_answer", cause, c.Observability)
		}
		if c.Observed {
			t.Fatalf("per-answer cause %q must not be marked cluster-observed", cause)
		}
	}
}

func TestFreshnessCausalityRetractionTransitions(t *testing.T) {
	fc := freshnessCausalityFromReport(statuspkg.Report{
		AsOf:              time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		GenerationHistory: statuspkg.GenerationHistorySnapshot{Active: 2, Superseded: 3},
		GenerationTransitions: []statuspkg.GenerationTransitionSnapshot{
			{ScopeID: "scope-1", GenerationID: "gen-old", Status: "superseded", SupersededAt: time.Date(2026, 6, 19, 2, 0, 0, 0, time.UTC)},
		},
	})
	if fc.Generations.Superseded != 3 {
		t.Fatalf("retired (superseded) generations = %d, want 3", fc.Generations.Superseded)
	}
	if len(fc.RecentTransitions) != 1 || fc.RecentTransitions[0].Status != "superseded" {
		t.Fatalf("recent retraction transitions not projected: %+v", fc.RecentTransitions)
	}
}
