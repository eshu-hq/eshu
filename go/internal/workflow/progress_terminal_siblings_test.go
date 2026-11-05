// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// deploymentMappingKey names a bridged phase whose owning reducer domain can
// dead-letter terminally (#4459).
var deploymentMappingKey = PhasePublicationKey{
	Keyspace:  gpphase.KeyspaceServiceUID,
	PhaseName: gpphase.PhaseDeploymentMapping,
}

// TestReconcileRunProgressTerminalItemWaitsForCollectionToEnd is the #10091
// transition table: a terminal failure fails the run only once no item of that
// run is pending or claimed. Failing it earlier takes the run out of the
// open-target guard and out of reconciliation while its siblings stay claimable,
// so the next planner pass admits a duplicate for each of them.
func TestReconcileRunProgressTerminalItemWaitsForCollectionToEnd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		collectors []CollectorRunProgress
		wantStatus RunStatus
	}{
		{
			name: "pending siblings keep the run open",
			collectors: []CollectorRunProgress{{
				CollectorKind:       scope.CollectorGit,
				TotalWorkItems:      61,
				PendingWorkItems:    60,
				FailedTerminalItems: 1,
			}},
			wantStatus: RunStatusCollectionActive,
		},
		{
			name: "pending and claimed siblings keep the run open",
			collectors: []CollectorRunProgress{{
				CollectorKind:       scope.CollectorGit,
				TotalWorkItems:      61,
				PendingWorkItems:    59,
				ClaimedWorkItems:    1,
				FailedTerminalItems: 1,
			}},
			wantStatus: RunStatusCollectionActive,
		},
		{
			name: "a claimed sibling alone keeps the run open",
			collectors: []CollectorRunProgress{{
				CollectorKind:       scope.CollectorGit,
				TotalWorkItems:      3,
				ClaimedWorkItems:    1,
				CompletedWorkItems:  1,
				FailedTerminalItems: 1,
			}},
			wantStatus: RunStatusCollectionActive,
		},
		{
			name: "a pending item of another collector keeps the run open",
			collectors: []CollectorRunProgress{
				{
					CollectorKind:       scope.CollectorGit,
					TotalWorkItems:      1,
					FailedTerminalItems: 1,
				},
				{
					CollectorKind:    scope.CollectorAWS,
					TotalWorkItems:   1,
					PendingWorkItems: 1,
				},
			},
			wantStatus: RunStatusCollectionActive,
		},
		{
			name: "drained siblings fail the run",
			collectors: []CollectorRunProgress{{
				CollectorKind:       scope.CollectorGit,
				TotalWorkItems:      61,
				CompletedWorkItems:  60,
				FailedTerminalItems: 1,
			}},
			wantStatus: RunStatusFailed,
		},
		{
			name: "drained siblings of another collector fail the run",
			collectors: []CollectorRunProgress{
				{
					CollectorKind:       scope.CollectorGit,
					TotalWorkItems:      1,
					FailedTerminalItems: 1,
				},
				{
					CollectorKind:      scope.CollectorAWS,
					TotalWorkItems:     1,
					CompletedWorkItems: 1,
				},
			},
			wantStatus: RunStatusFailed,
		},
		{
			// The one uncounted row stands for an expired item: the progress
			// query buckets only pending, claimed, completed and
			// failed_terminal, so any other status is an uncounted ended row.
			name: "an expired sibling counts as ended",
			collectors: []CollectorRunProgress{{
				CollectorKind:       scope.CollectorGit,
				TotalWorkItems:      3,
				CompletedWorkItems:  1,
				FailedTerminalItems: 1,
			}},
			wantStatus: RunStatusFailed,
		},
		{
			name: "an only item that failed terminally fails the run",
			collectors: []CollectorRunProgress{{
				CollectorKind:       scope.CollectorGit,
				TotalWorkItems:      1,
				FailedTerminalItems: 1,
			}},
			wantStatus: RunStatusFailed,
		},
		{
			name: "a reducer dead-letter with pending collection stays open",
			collectors: []CollectorRunProgress{{
				CollectorKind:            scope.CollectorGit,
				TotalWorkItems:           2,
				PendingWorkItems:         1,
				CompletedWorkItems:       1,
				TerminalDeadLetterCounts: map[PhasePublicationKey]int{deploymentMappingKey: 1},
			}},
			wantStatus: RunStatusCollectionActive,
		},
		{
			name: "a reducer dead-letter with only pending collection stays open",
			collectors: []CollectorRunProgress{{
				CollectorKind:            scope.CollectorGit,
				TotalWorkItems:           1,
				PendingWorkItems:         1,
				TerminalDeadLetterCounts: map[PhasePublicationKey]int{deploymentMappingKey: 1},
			}},
			wantStatus: RunStatusCollectionActive,
		},
		{
			name: "a reducer dead-letter fails the run once collection ends",
			collectors: []CollectorRunProgress{{
				CollectorKind:            scope.CollectorGit,
				TotalWorkItems:           2,
				CompletedWorkItems:       2,
				TerminalDeadLetterCounts: map[PhasePublicationKey]int{deploymentMappingKey: 1},
			}},
			wantStatus: RunStatusFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
			run, _, err := ReconcileRunProgress(RunProgressSnapshot{
				Run: Run{
					RunID:       "run-terminal-siblings",
					TriggerKind: TriggerKindSchedule,
					Status:      RunStatusCollectionActive,
					CreatedAt:   now.Add(-time.Hour),
					UpdatedAt:   now.Add(-time.Hour),
				},
				Collectors: tt.collectors,
			}, now)
			if err != nil {
				t.Fatalf("ReconcileRunProgress() error = %v, want nil", err)
			}
			if got := run.Status; got != tt.wantStatus {
				t.Fatalf("run.Status = %q, want %q", got, tt.wantStatus)
			}
			if tt.wantStatus == RunStatusFailed {
				if !run.FinishedAt.Equal(now) {
					t.Fatalf("run.FinishedAt = %v, want %v on a failed run", run.FinishedAt, now)
				}
				return
			}
			if !run.FinishedAt.IsZero() {
				t.Fatalf("run.FinishedAt = %v, want zero while collection is open", run.FinishedAt)
			}
		})
	}
}

// TestReconcileRunProgressTerminalItemBlocksCompletenessWhileRunIsOpen pins the
// operator signal that replaces the early failed status: a terminal failure
// blocks every required phase at once, even though the run stays open until its
// siblings drain.
func TestReconcileRunProgressTerminalItemBlocksCompletenessWhileRunIsOpen(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	_, completeness, err := ReconcileRunProgress(RunProgressSnapshot{
		Run: Run{
			RunID:       "run-terminal-siblings-completeness",
			TriggerKind: TriggerKindSchedule,
			Status:      RunStatusCollectionPending,
			CreatedAt:   now.Add(-time.Hour),
			UpdatedAt:   now.Add(-time.Hour),
		},
		Collectors: []CollectorRunProgress{{
			CollectorKind:       scope.CollectorGit,
			TotalWorkItems:      61,
			PendingWorkItems:    60,
			FailedTerminalItems: 1,
		}},
	}, now)
	if err != nil {
		t.Fatalf("ReconcileRunProgress() error = %v, want nil", err)
	}
	if len(completeness) == 0 {
		t.Fatal("completeness rows = 0, want the git collector's required phases")
	}
	for _, state := range completeness {
		if got, want := state.Status, CompletenessStatusBlocked; got != want {
			t.Fatalf("phase %q status = %q, want %q while the run is open", state.PhaseName, got, want)
		}
	}
}

// TestReconcileRunProgressDeadLetterRunFailsWhenPendingItemCompletes walks one
// run across two ticks: open while a sibling is pending, failed once it
// completes, so a dead-lettered phase still ends the run instead of wedging it
// in reducer_converging (#4459).
func TestReconcileRunProgressDeadLetterRunFailsWhenPendingItemCompletes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	snapshot := RunProgressSnapshot{
		Run: Run{
			RunID:       "run-dead-letter-then-drain",
			TriggerKind: TriggerKindSchedule,
			Status:      RunStatusCollectionActive,
			CreatedAt:   now.Add(-time.Hour),
			UpdatedAt:   now.Add(-time.Hour),
		},
		Collectors: []CollectorRunProgress{{
			CollectorKind:            scope.CollectorGit,
			TotalWorkItems:           2,
			PendingWorkItems:         1,
			CompletedWorkItems:       1,
			TerminalDeadLetterCounts: map[PhasePublicationKey]int{deploymentMappingKey: 1},
		}},
	}

	open, completeness, err := ReconcileRunProgress(snapshot, now)
	if err != nil {
		t.Fatalf("first ReconcileRunProgress() error = %v, want nil", err)
	}
	if got, want := open.Status, RunStatusCollectionActive; got != want {
		t.Fatalf("first run.Status = %q, want %q", got, want)
	}
	var sawBlocked bool
	for _, state := range completeness {
		if state.Keyspace == deploymentMappingKey.Keyspace && state.PhaseName == string(deploymentMappingKey.PhaseName) {
			sawBlocked = state.Status == CompletenessStatusBlocked
		}
	}
	if !sawBlocked {
		t.Fatal("deployment_mapping completeness is not blocked while the run is open")
	}

	snapshot.Run = open
	snapshot.Collectors[0].PendingWorkItems = 0
	snapshot.Collectors[0].CompletedWorkItems = 2
	failedAt := now.Add(time.Minute)
	failed, _, err := ReconcileRunProgress(snapshot, failedAt)
	if err != nil {
		t.Fatalf("second ReconcileRunProgress() error = %v, want nil", err)
	}
	if got, want := failed.Status, RunStatusFailed; got != want {
		t.Fatalf("second run.Status = %q, want %q", got, want)
	}
	if !failed.FinishedAt.Equal(failedAt) {
		t.Fatalf("second run.FinishedAt = %v, want %v", failed.FinishedAt, failedAt)
	}
}
