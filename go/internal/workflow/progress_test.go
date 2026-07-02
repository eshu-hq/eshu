// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestPhaseRequirementValidate lives in phase_requirement_test.go, split out
// to keep this file under the 500-line cap (#4459).

func TestReconcileRunProgressPendingCollection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 18, 0, 0, 0, time.UTC)
	run, completeness, err := ReconcileRunProgress(RunProgressSnapshot{
		Run: Run{
			RunID:       "run-pending",
			TriggerKind: TriggerKindBootstrap,
			Status:      RunStatusCollectionPending,
			CreatedAt:   now.Add(-time.Minute),
			UpdatedAt:   now.Add(-time.Minute),
		},
		Collectors: []CollectorRunProgress{{
			CollectorKind:        scope.CollectorGit,
			TotalWorkItems:       2,
			PendingWorkItems:     2,
			PublishedPhaseCounts: map[PhasePublicationKey]int{},
		}},
	}, now)
	if err != nil {
		t.Fatalf("ReconcileRunProgress() error = %v, want nil", err)
	}
	if got, want := run.Status, RunStatusCollectionPending; got != want {
		t.Fatalf("run.Status = %q, want %q", got, want)
	}
	if got, want := len(completeness), 6; got != want {
		t.Fatalf("len(completeness) = %d, want %d", got, want)
	}
	for _, state := range completeness {
		if state.Keyspace == "" {
			t.Fatalf("phase %q keyspace = blank, want non-blank", state.PhaseName)
		}
		if got, want := state.Status, CompletenessStatusPending; got != want {
			t.Fatalf("phase %q status = %q, want %q", state.PhaseName, got, want)
		}
	}
}

func TestReconcileRunProgressCollectionActive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 18, 5, 0, 0, time.UTC)
	run, _, err := ReconcileRunProgress(RunProgressSnapshot{
		Run: Run{
			RunID:       "run-active",
			TriggerKind: TriggerKindBootstrap,
			Status:      RunStatusCollectionPending,
			CreatedAt:   now.Add(-time.Minute),
			UpdatedAt:   now.Add(-time.Minute),
		},
		Collectors: []CollectorRunProgress{{
			CollectorKind:        scope.CollectorGit,
			TotalWorkItems:       3,
			PendingWorkItems:     1,
			ClaimedWorkItems:     1,
			CompletedWorkItems:   1,
			PublishedPhaseCounts: map[PhasePublicationKey]int{},
		}},
	}, now)
	if err != nil {
		t.Fatalf("ReconcileRunProgress() error = %v, want nil", err)
	}
	if got, want := run.Status, RunStatusCollectionActive; got != want {
		t.Fatalf("run.Status = %q, want %q", got, want)
	}
}

func TestReconcileRunProgressReducerConverging(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 18, 10, 0, 0, time.UTC)
	run, completeness, err := ReconcileRunProgress(RunProgressSnapshot{
		Run: Run{
			RunID:       "run-converging",
			TriggerKind: TriggerKindBootstrap,
			Status:      RunStatusCollectionActive,
			CreatedAt:   now.Add(-time.Minute),
			UpdatedAt:   now.Add(-time.Minute),
		},
		Collectors: []CollectorRunProgress{{
			CollectorKind:      scope.CollectorGit,
			TotalWorkItems:     2,
			CompletedWorkItems: 2,
			PublishedPhaseCounts: map[PhasePublicationKey]int{
				{
					Keyspace:  reducer.GraphProjectionKeyspaceCodeEntitiesUID,
					PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
				}: 2,
			},
		}},
	}, now)
	if err != nil {
		t.Fatalf("ReconcileRunProgress() error = %v, want nil", err)
	}
	if got, want := run.Status, RunStatusReducerConverging; got != want {
		t.Fatalf("run.Status = %q, want %q", got, want)
	}
	if got, want := completeness[0].Status, CompletenessStatusReady; got != want {
		t.Fatalf("first phase status = %q, want %q", got, want)
	}
	if got, want := completeness[0].Keyspace, reducer.GraphProjectionKeyspaceCodeEntitiesUID; got != want {
		t.Fatalf("first phase keyspace = %q, want %q", got, want)
	}
	if got, want := completeness[1].Status, CompletenessStatusPending; got != want {
		t.Fatalf("second phase status = %q, want %q", got, want)
	}
}

func TestReconcileRunProgressComplete(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 18, 15, 0, 0, time.UTC)
	run, completeness, err := ReconcileRunProgress(RunProgressSnapshot{
		Run: Run{
			RunID:       "run-complete",
			TriggerKind: TriggerKindBootstrap,
			Status:      RunStatusReducerConverging,
			CreatedAt:   now.Add(-time.Minute),
			UpdatedAt:   now.Add(-time.Minute),
		},
		Collectors: []CollectorRunProgress{{
			CollectorKind:      scope.CollectorGit,
			TotalWorkItems:     1,
			CompletedWorkItems: 1,
			PublishedPhaseCounts: map[PhasePublicationKey]int{
				{
					Keyspace:  reducer.GraphProjectionKeyspaceCodeEntitiesUID,
					PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
				}: 1,
				{
					Keyspace:  reducer.GraphProjectionKeyspaceCodeEntitiesUID,
					PhaseName: reducer.GraphProjectionPhaseSemanticNodesCommitted,
				}: 1,
				{
					Keyspace:  reducer.GraphProjectionKeyspaceDeployableUnitUID,
					PhaseName: reducer.GraphProjectionPhaseDeployableUnitCorrelation,
				}: 1,
				{
					Keyspace:  reducer.GraphProjectionKeyspaceServiceUID,
					PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
				}: 1,
				{
					Keyspace:  reducer.GraphProjectionKeyspaceServiceUID,
					PhaseName: reducer.GraphProjectionPhaseDeploymentMapping,
				}: 1,
				{
					Keyspace:  reducer.GraphProjectionKeyspaceServiceUID,
					PhaseName: reducer.GraphProjectionPhaseWorkloadMaterialization,
				}: 1,
			},
		}},
	}, now)
	if err != nil {
		t.Fatalf("ReconcileRunProgress() error = %v, want nil", err)
	}
	if got, want := run.Status, RunStatusComplete; got != want {
		t.Fatalf("run.Status = %q, want %q", got, want)
	}
	if run.FinishedAt.IsZero() {
		t.Fatal("run.FinishedAt = zero, want non-zero")
	}
	for _, state := range completeness {
		if state.Keyspace == "" {
			t.Fatalf("phase %q keyspace = blank, want non-blank", state.PhaseName)
		}
		if got, want := state.Status, CompletenessStatusReady; got != want {
			t.Fatalf("phase %q status = %q, want %q", state.PhaseName, got, want)
		}
	}
}

func TestReconcileRunProgressCompletesOCIRegistryWithoutReducerPhases(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 12, 19, 0, 0, 0, time.UTC)
	run, completeness, err := ReconcileRunProgress(RunProgressSnapshot{
		Run: Run{
			RunID:       "run-oci-registry",
			TriggerKind: TriggerKindBootstrap,
			Status:      RunStatusCollectionActive,
			CreatedAt:   now.Add(-time.Minute),
			UpdatedAt:   now.Add(-time.Minute),
		},
		Collectors: []CollectorRunProgress{{
			CollectorKind:        scope.CollectorOCIRegistry,
			TotalWorkItems:       1,
			CompletedWorkItems:   1,
			PublishedPhaseCounts: map[PhasePublicationKey]int{},
		}},
	}, now)
	if err != nil {
		t.Fatalf("ReconcileRunProgress() error = %v, want nil", err)
	}
	if got, want := run.Status, RunStatusComplete; got != want {
		t.Fatalf("run.Status = %q, want %q", got, want)
	}
	if got := len(completeness); got != 0 {
		t.Fatalf("len(completeness) = %d, want 0 because OCI registry has no graph projection readiness yet", got)
	}
}

func TestReconcileRunProgressCompletesAWSWithoutImplementedGraphPhases(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 21, 13, 30, 0, 0, time.UTC)
	run, completeness, err := ReconcileRunProgress(RunProgressSnapshot{
		Run: Run{
			RunID:       "run-aws",
			TriggerKind: TriggerKindSchedule,
			Status:      RunStatusCollectionActive,
			CreatedAt:   now.Add(-time.Minute),
			UpdatedAt:   now.Add(-time.Minute),
		},
		Collectors: []CollectorRunProgress{{
			CollectorKind:        scope.CollectorAWS,
			TotalWorkItems:       19,
			CompletedWorkItems:   19,
			PublishedPhaseCounts: map[PhasePublicationKey]int{},
		}},
	}, now)
	if err != nil {
		t.Fatalf("ReconcileRunProgress() error = %v, want nil", err)
	}
	if got, want := run.Status, RunStatusComplete; got != want {
		t.Fatalf("run.Status = %q, want %q for completed AWS work without live graph phase publisher", got, want)
	}
	if got := len(completeness); got != 0 {
		t.Fatalf("len(completeness) = %d, want 0 because AWS has no live cloud-resource graph projection yet", got)
	}
}

func TestReconcileRunProgressFailed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 18, 20, 0, 0, time.UTC)
	run, completeness, err := ReconcileRunProgress(RunProgressSnapshot{
		Run: Run{
			RunID:       "run-failed",
			TriggerKind: TriggerKindBootstrap,
			Status:      RunStatusCollectionActive,
			CreatedAt:   now.Add(-time.Minute),
			UpdatedAt:   now.Add(-time.Minute),
		},
		Collectors: []CollectorRunProgress{{
			CollectorKind:       scope.CollectorGit,
			TotalWorkItems:      2,
			CompletedWorkItems:  1,
			FailedTerminalItems: 1,
			PublishedPhaseCounts: map[PhasePublicationKey]int{
				{
					Keyspace:  reducer.GraphProjectionKeyspaceCodeEntitiesUID,
					PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
				}: 1,
			},
		}},
	}, now)
	if err != nil {
		t.Fatalf("ReconcileRunProgress() error = %v, want nil", err)
	}
	if got, want := run.Status, RunStatusFailed; got != want {
		t.Fatalf("run.Status = %q, want %q", got, want)
	}
	for _, state := range completeness {
		if state.Keyspace == "" {
			t.Fatalf("phase %q keyspace = blank, want non-blank", state.PhaseName)
		}
		if got, want := state.Status, CompletenessStatusBlocked; got != want {
			t.Fatalf("phase %q status = %q, want %q", state.PhaseName, got, want)
		}
	}
}

func TestReconcileRunProgressTerminalReducerDeadLetterBlocksConvergence(t *testing.T) {
	t.Parallel()

	// Regression for #4459: a permanently dead-lettered reducer intent on a
	// required phase (deployment_mapping / workload_materialization) never
	// publishes its graph_projection_phase_state row, so
	// allRequiredPhasesReady stays false forever and the run wedges in
	// reducer_converging with no terminal signal. TerminalDeadLetterCounts
	// bridges the reducer's own dead-letter queue into completeness so the
	// run instead terminates as blocked/failed.
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	run, completeness, err := ReconcileRunProgress(RunProgressSnapshot{
		Run: Run{
			RunID:       "run-dlq-wedge",
			TriggerKind: TriggerKindBootstrap,
			Status:      RunStatusCollectionActive,
			CreatedAt:   now.Add(-time.Hour),
			UpdatedAt:   now.Add(-time.Hour),
		},
		Collectors: []CollectorRunProgress{{
			CollectorKind:      scope.CollectorGit,
			TotalWorkItems:     1,
			CompletedWorkItems: 1,
			PublishedPhaseCounts: map[PhasePublicationKey]int{
				{
					Keyspace:  reducer.GraphProjectionKeyspaceCodeEntitiesUID,
					PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
				}: 1,
				{
					Keyspace:  reducer.GraphProjectionKeyspaceCodeEntitiesUID,
					PhaseName: reducer.GraphProjectionPhaseSemanticNodesCommitted,
				}: 1,
				{
					Keyspace:  reducer.GraphProjectionKeyspaceDeployableUnitUID,
					PhaseName: reducer.GraphProjectionPhaseDeployableUnitCorrelation,
				}: 1,
				{
					Keyspace:  reducer.GraphProjectionKeyspaceServiceUID,
					PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
				}: 1,
				// deployment_mapping and workload_materialization are
				// intentionally absent: their reducer intents dead-lettered
				// terminally and never published.
			},
			TerminalDeadLetterCounts: map[PhasePublicationKey]int{
				{
					Keyspace:  reducer.GraphProjectionKeyspaceServiceUID,
					PhaseName: reducer.GraphProjectionPhaseDeploymentMapping,
				}: 1,
			},
		}},
	}, now)
	if err != nil {
		t.Fatalf("ReconcileRunProgress() error = %v, want nil", err)
	}
	if got, want := run.Status, RunStatusFailed; got != want {
		t.Fatalf("run.Status = %q, want %q (must not wedge in reducer_converging on a terminal dead-letter)", got, want)
	}
	if run.FinishedAt.IsZero() {
		t.Fatal("run.FinishedAt = zero, want non-zero when a run terminates on a terminal dead-letter")
	}

	var sawBlockedDeploymentMapping bool
	for _, state := range completeness {
		if state.Keyspace == reducer.GraphProjectionKeyspaceServiceUID &&
			state.PhaseName == string(reducer.GraphProjectionPhaseDeploymentMapping) {
			sawBlockedDeploymentMapping = true
			if got, want := state.Status, CompletenessStatusBlocked; got != want {
				t.Fatalf("deployment_mapping completeness status = %q, want %q", got, want)
			}
			if !strings.Contains(state.Detail, "dead_letter") && !strings.Contains(state.Detail, "dead-letter") {
				t.Fatalf("deployment_mapping completeness detail = %q, want it to name the dead-letter reason", state.Detail)
			}
		}
	}
	if !sawBlockedDeploymentMapping {
		t.Fatal("completeness rows missing the blocked deployment_mapping phase")
	}
}

func TestReconcileRunProgressRetryableDeadLetterDoesNotBlockConvergence(t *testing.T) {
	t.Parallel()

	// Negative/false-fail guard: a still-retrying (non-terminal) reducer
	// item, or a dead-letter on a phase with no bridged reducer domain, must
	// NOT flip the run to blocked/failed. Only a genuinely terminal
	// dead-letter on a required, bridged phase may terminate convergence.
	// Here TerminalDeadLetterCounts is empty (as it must be for anything
	// short of a confirmed dead_letter row), so the run must still converge
	// normally while deployment_mapping remains merely pending.
	now := time.Date(2026, time.July, 1, 12, 5, 0, 0, time.UTC)
	run, completeness, err := ReconcileRunProgress(RunProgressSnapshot{
		Run: Run{
			RunID:       "run-retrying-not-blocked",
			TriggerKind: TriggerKindBootstrap,
			Status:      RunStatusCollectionActive,
			CreatedAt:   now.Add(-time.Hour),
			UpdatedAt:   now.Add(-time.Hour),
		},
		Collectors: []CollectorRunProgress{{
			CollectorKind:      scope.CollectorGit,
			TotalWorkItems:     1,
			CompletedWorkItems: 1,
			PublishedPhaseCounts: map[PhasePublicationKey]int{
				{
					Keyspace:  reducer.GraphProjectionKeyspaceCodeEntitiesUID,
					PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
				}: 1,
				{
					Keyspace:  reducer.GraphProjectionKeyspaceCodeEntitiesUID,
					PhaseName: reducer.GraphProjectionPhaseSemanticNodesCommitted,
				}: 1,
				{
					Keyspace:  reducer.GraphProjectionKeyspaceDeployableUnitUID,
					PhaseName: reducer.GraphProjectionPhaseDeployableUnitCorrelation,
				}: 1,
				{
					Keyspace:  reducer.GraphProjectionKeyspaceServiceUID,
					PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
				}: 1,
				// deployment_mapping/workload_materialization still
				// legitimately in flight (retrying, not dead-lettered).
			},
			TerminalDeadLetterCounts: map[PhasePublicationKey]int{},
		}},
	}, now)
	if err != nil {
		t.Fatalf("ReconcileRunProgress() error = %v, want nil", err)
	}
	if got, want := run.Status, RunStatusReducerConverging; got != want {
		t.Fatalf("run.Status = %q, want %q (a non-terminal gap must keep converging, not false-fail)", got, want)
	}
	if !run.FinishedAt.IsZero() {
		t.Fatal("run.FinishedAt = non-zero, want zero for a still-converging run")
	}
	for _, state := range completeness {
		if state.Keyspace == reducer.GraphProjectionKeyspaceServiceUID &&
			state.PhaseName == string(reducer.GraphProjectionPhaseDeploymentMapping) {
			if got, want := state.Status, CompletenessStatusPending; got != want {
				t.Fatalf("deployment_mapping completeness status = %q, want %q (must not false-block on non-terminal work)", got, want)
			}
		}
	}
}

func TestRequiredPhasesForCollectorIncludesGitSecondPassGates(t *testing.T) {
	t.Parallel()

	requirements := RequiredPhasesForCollector(scope.CollectorGit)
	if got, want := len(requirements), 6; got != want {
		t.Fatalf("collector %q requirements = %d, want %d", scope.CollectorGit, got, want)
	}
	if got, want := requirements[2].Keyspace, reducer.GraphProjectionKeyspaceDeployableUnitUID; got != want {
		t.Fatalf("deployable unit keyspace = %q, want %q", got, want)
	}
	if got, want := requirements[2].PhaseName, reducer.GraphProjectionPhaseDeployableUnitCorrelation; got != want {
		t.Fatalf("deployable unit phase = %q, want %q", got, want)
	}
	if got, want := requirements[3].Keyspace, reducer.GraphProjectionKeyspaceServiceUID; got != want {
		t.Fatalf("service canonical keyspace = %q, want %q", got, want)
	}
	if got, want := requirements[3].PhaseName, reducer.GraphProjectionPhaseCanonicalNodesCommitted; got != want {
		t.Fatalf("service canonical phase = %q, want %q", got, want)
	}
	if got, want := requirements[4].PhaseName, reducer.GraphProjectionPhaseDeploymentMapping; got != want {
		t.Fatalf("deployment mapping phase = %q, want %q", got, want)
	}
	if got, want := requirements[5].PhaseName, reducer.GraphProjectionPhaseWorkloadMaterialization; got != want {
		t.Fatalf("workload materialization phase = %q, want %q", got, want)
	}
}
