// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestBuildReportClassifiesProgressingQueue(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			ScopeCounts: []status.NamedCount{
				{Name: "active", Count: 3},
			},
			GenerationCounts: []status.NamedCount{
				{Name: "active", Count: 1},
				{Name: "completed", Count: 4},
			},
			Queue: status.QueueSnapshot{
				Total:                8,
				Outstanding:          4,
				Pending:              1,
				InFlight:             2,
				Retrying:             1,
				Succeeded:            4,
				OldestOutstandingAge: 2 * time.Minute,
			},
			StageCounts: []status.StageStatusCount{
				{Stage: "projector", Status: "running", Count: 1},
				{Stage: "projector", Status: "pending", Count: 1},
				{Stage: "reducer", Status: "claimed", Count: 1},
				{Stage: "reducer", Status: "retrying", Count: 1},
			},
			DomainBacklogs: []status.DomainBacklog{
				{
					Domain:      "repository",
					Outstanding: 3,
					Retrying:    1,
					OldestAge:   2 * time.Minute,
				},
			},
			QueueBlockages: []status.QueueBlockage{
				{
					Stage:          "reducer",
					Domain:         "semantic_entity_materialization",
					ConflictDomain: "code_graph",
					ConflictKey:    "scope-1:gen-1:code",
					Blocked:        2,
					OldestAge:      75 * time.Second,
				},
			},
		},
		status.DefaultOptions(),
	)

	if report.Health.State != "progressing" {
		t.Fatalf("BuildReport().Health.State = %q, want %q", report.Health.State, "progressing")
	}
	if len(report.StageSummaries) != 2 {
		t.Fatalf("BuildReport().StageSummaries len = %d, want 2", len(report.StageSummaries))
	}
	if got := report.StageSummaries[0].Stage; got != "projector" {
		t.Fatalf("BuildReport().StageSummaries[0].Stage = %q, want %q", got, "projector")
	}
	if got := report.StageSummaries[0].Running; got != 1 {
		t.Fatalf("BuildReport().StageSummaries[0].Running = %d, want 1", got)
	}
	if got := report.StageSummaries[1].Claimed; got != 1 {
		t.Fatalf("BuildReport().StageSummaries[1].Claimed = %d, want 1", got)
	}
	if got, want := len(report.QueueBlockages), 1; got != want {
		t.Fatalf("BuildReport().QueueBlockages len = %d, want %d", got, want)
	}
	if got := report.QueueBlockages[0].ConflictKey; got != "scope-1:gen-1:code" {
		t.Fatalf("BuildReport().QueueBlockages[0].ConflictKey = %q, want conflict key", got)
	}
}

func TestBuildReportTreatsActiveAuthoritativeGenerationAsHealthyWhenQueueDrained(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			ScopeActivity: status.ScopeActivitySnapshot{
				Active:  3,
				Changed: 0,
			},
			GenerationCounts: []status.NamedCount{
				{Name: "active", Count: 3},
				{Name: "completed", Count: 5},
			},
			Queue: status.QueueSnapshot{
				Outstanding: 0,
				InFlight:    0,
				Pending:     0,
				Retrying:    0,
				Failed:      0,
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
}

func TestBuildReportTreatsSharedProjectionBacklogAsProgressing(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			ScopeActivity: status.ScopeActivitySnapshot{
				Active:  1,
				Changed: 0,
			},
			GenerationCounts: []status.NamedCount{
				{Name: "active", Count: 1},
				{Name: "completed", Count: 1},
			},
			Queue: status.QueueSnapshot{
				Outstanding: 0,
				InFlight:    0,
				Pending:     0,
				Retrying:    0,
				Failed:      0,
			},
			DomainBacklogs: []status.DomainBacklog{
				{
					Domain:      "code_calls",
					Outstanding: 12,
					OldestAge:   45 * time.Second,
				},
			},
		},
		status.DefaultOptions(),
	)

	if got, want := report.Health.State, "progressing"; got != want {
		t.Fatalf("BuildReport().Health.State = %q, want %q", got, want)
	}
	reasons := strings.Join(report.Health.Reasons, " ")
	if !strings.Contains(reasons, "shared projection") || !strings.Contains(reasons, "code_calls") {
		t.Fatalf("BuildReport().Health.Reasons = %v, want shared projection code_calls reason", report.Health.Reasons)
	}
}

func TestBuildReportTreatsOldSharedProjectionBacklogAsStalled(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			Queue: status.QueueSnapshot{
				Outstanding: 0,
				InFlight:    0,
				Pending:     0,
			},
			DomainBacklogs: []status.DomainBacklog{
				{
					Domain:      "code_calls",
					Outstanding: 7,
					OldestAge:   11 * time.Minute,
				},
			},
		},
		status.Options{
			StallAfter:  10 * time.Minute,
			DomainLimit: 5,
		},
	)

	if got, want := report.Health.State, "stalled"; got != want {
		t.Fatalf("BuildReport().Health.State = %q, want %q", got, want)
	}
	reasons := strings.Join(report.Health.Reasons, " ")
	if !strings.Contains(reasons, "shared projection") || !strings.Contains(reasons, "11m0s") {
		t.Fatalf("BuildReport().Health.Reasons = %v, want stalled shared projection reason", report.Health.Reasons)
	}
}

func TestBuildReportTreatsActiveOldSharedProjectionBacklogAsProgressing(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			Queue: status.QueueSnapshot{
				Outstanding: 0,
				InFlight:    0,
				Pending:     0,
			},
			DomainBacklogs: []status.DomainBacklog{
				{
					Domain:      "code_calls",
					Outstanding: 622558,
					InFlight:    1,
					OldestAge:   12 * time.Minute,
				},
			},
		},
		status.Options{
			StallAfter:  10 * time.Minute,
			DomainLimit: 5,
		},
	)

	if got, want := report.Health.State, "progressing"; got != want {
		t.Fatalf("BuildReport().Health.State = %q, want %q", got, want)
	}
	reasons := strings.Join(report.Health.Reasons, " ")
	if !strings.Contains(reasons, "shared projection") ||
		!strings.Contains(reasons, "code_calls") ||
		!strings.Contains(reasons, "in flight") {
		t.Fatalf("BuildReport().Health.Reasons = %v, want active shared projection reason", report.Health.Reasons)
	}
}

func TestBuildReportTreatsInFlightOnlySharedProjectionAsHealthy(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			Queue: status.QueueSnapshot{
				Outstanding: 0,
				InFlight:    0,
				Pending:     0,
			},
			DomainBacklogs: []status.DomainBacklog{
				{
					Domain:   "code_calls",
					InFlight: 1,
				},
			},
		},
		status.Options{
			StallAfter:  10 * time.Minute,
			DomainLimit: 5,
		},
	)

	if got, want := report.Health.State, "healthy"; got != want {
		t.Fatalf("BuildReport().Health.State = %q, want %q", got, want)
	}
	reasons := strings.Join(report.Health.Reasons, " ")
	if reasons != "no outstanding queue backlog" {
		t.Fatalf("BuildReport().Health.Reasons = %v, want healthy queue-drained reason", report.Health.Reasons)
	}
}

func TestBuildReportTreatsCoordinatorReducerConvergenceAsProgressing(t *testing.T) {
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
					{Name: "complete", Count: 3},
					{Name: "reducer_converging", Count: 1},
				},
				CompletenessCounts: []status.NamedCount{
					{Name: "pending", Count: 2},
					{Name: "ready", Count: 4},
				},
			},
		},
		status.DefaultOptions(),
	)

	if got, want := report.Health.State, "progressing"; got != want {
		t.Fatalf("BuildReport().Health.State = %q, want %q", got, want)
	}
	reasons := strings.Join(report.Health.Reasons, " ")
	if !strings.Contains(reasons, "workflow coordinator") ||
		!strings.Contains(reasons, "reducer_converging=1") ||
		!strings.Contains(reasons, "pending completeness=2") {
		t.Fatalf("BuildReport().Health.Reasons = %v, want workflow convergence detail", report.Health.Reasons)
	}
}

func TestBuildReportTreatsRecentProducerActivityAsProgressing(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 5, 21, 15, 29, 21, 0, time.UTC),
			GenerationCounts: []status.NamedCount{
				{Name: "active", Count: 890},
				{Name: "pending", Count: 2},
			},
			Queue: status.QueueSnapshot{
				Total:                7530,
				Succeeded:            7508,
				Outstanding:          22,
				Pending:              22,
				InFlight:             0,
				OldestOutstandingAge: 12 * time.Minute,
			},
			Coordinator: &status.CoordinatorSnapshot{
				RunStatusCounts: []status.NamedCount{
					{Name: "complete", Count: 3},
					{Name: "reducer_converging", Count: 1},
				},
				WorkItemStatusCounts: []status.NamedCount{
					{Name: "completed", Count: 22},
				},
				CompletenessCounts: []status.NamedCount{
					{Name: "pending", Count: 2},
				},
			},
			ProducerActivity: status.ProducerActivitySnapshot{
				HasActiveOrPendingGeneration: true,
				LatestGenerationAge:          15 * time.Second,
			},
		},
		status.Options{
			StallAfter:  10 * time.Minute,
			DomainLimit: 5,
		},
	)

	if got, want := report.Health.State, "progressing"; got != want {
		t.Fatalf("BuildReport().Health.State = %q, want %q", got, want)
	}
	reasons := strings.Join(report.Health.Reasons, " ")
	if !strings.Contains(reasons, "recent producer activity") {
		t.Fatalf("BuildReport().Health.Reasons = %v, want recent producer activity reason", report.Health.Reasons)
	}
}

func TestBuildReportDoesNotHideFailuresBehindRecentProducerActivity(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 5, 21, 15, 29, 21, 0, time.UTC),
			Queue: status.QueueSnapshot{
				Total:                12,
				Succeeded:            6,
				Outstanding:          4,
				Pending:              4,
				DeadLetter:           2,
				OldestOutstandingAge: 12 * time.Minute,
			},
			ProducerActivity: status.ProducerActivitySnapshot{
				HasActiveOrPendingGeneration: true,
				LatestGenerationAge:          15 * time.Second,
			},
		},
		status.Options{
			StallAfter:  10 * time.Minute,
			DomainLimit: 5,
		},
	)

	if got, want := report.Health.State, "degraded"; got != want {
		t.Fatalf("BuildReport().Health.State = %q, want %q", got, want)
	}
	if !strings.Contains(strings.Join(report.Health.Reasons, " "), "dead-lettered") {
		t.Fatalf("BuildReport().Health.Reasons = %v, want dead-letter detail", report.Health.Reasons)
	}
}

func TestBuildReportTreatsCoordinatorFailureAsDegraded(t *testing.T) {
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
					{Name: "failed", Count: 1},
				},
				CompletenessCounts: []status.NamedCount{
					{Name: "blocked", Count: 3},
				},
			},
		},
		status.DefaultOptions(),
	)

	if got, want := report.Health.State, "degraded"; got != want {
		t.Fatalf("BuildReport().Health.State = %q, want %q", got, want)
	}
	reasons := strings.Join(report.Health.Reasons, " ")
	if !strings.Contains(reasons, "workflow coordinator") ||
		!strings.Contains(reasons, "failed runs=1") ||
		!strings.Contains(reasons, "blocked completeness=3") {
		t.Fatalf("BuildReport().Health.Reasons = %v, want workflow failure detail", report.Health.Reasons)
	}
}

func TestBuildReportClassifiesStalledBacklog(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			Queue: status.QueueSnapshot{
				Outstanding:          5,
				Pending:              5,
				OldestOutstandingAge: 12 * time.Minute,
			},
			StageCounts: []status.StageStatusCount{
				{Stage: "projector", Status: "pending", Count: 5},
			},
		},
		status.Options{
			StallAfter:  10 * time.Minute,
			DomainLimit: 5,
		},
	)

	if report.Health.State != "stalled" {
		t.Fatalf("BuildReport().Health.State = %q, want %q", report.Health.State, "stalled")
	}
	if len(report.Health.Reasons) == 0 {
		t.Fatal("BuildReport().Health.Reasons = empty, want non-empty")
	}
	if !strings.Contains(report.Health.Reasons[0], "no in-flight work") {
		t.Fatalf("BuildReport().Health.Reasons[0] = %q, want substring %q", report.Health.Reasons[0], "no in-flight work")
	}
}

func TestBuildReportClassifiesDegradedFailures(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			GenerationCounts: []status.NamedCount{
				{Name: "failed", Count: 1},
			},
			Queue: status.QueueSnapshot{
				DeadLetter: 2,
			},
		},
		status.DefaultOptions(),
	)

	if report.Health.State != "degraded" {
		t.Fatalf("BuildReport().Health.State = %q, want %q", report.Health.State, "degraded")
	}
	if !strings.Contains(strings.Join(report.Health.Reasons, " "), "dead-letter") {
		t.Fatalf("BuildReport().Health.Reasons = %v, want mention of dead-lettered work", report.Health.Reasons)
	}
}
