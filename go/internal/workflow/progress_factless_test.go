// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Factless-convergence tests live here, split out to keep progress_test.go
// under the 500-line cap (#9257).

func TestReconcileRunProgressCompletesFactlessClaim(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 27, 13, 0, 0, 0, time.UTC)
	run, completeness, err := ReconcileRunProgress(RunProgressSnapshot{
		Run: Run{
			RunID:       "run-factless",
			TriggerKind: TriggerKindSchedule,
			Status:      RunStatusReducerConverging,
			CreatedAt:   now.Add(-time.Hour),
			UpdatedAt:   now.Add(-time.Hour),
		},
		Collectors: []CollectorRunProgress{{
			CollectorKind:          scope.CollectorTerraformState,
			TotalWorkItems:         1,
			CompletedWorkItems:     1,
			FactlessCompletedItems: 1,
			PublishedPhaseCounts:   map[PhasePublicationKey]int{},
		}},
	}, now)
	if err != nil {
		t.Fatalf("ReconcileRunProgress() error = %v, want nil", err)
	}
	if got, want := run.Status, RunStatusComplete; got != want {
		t.Fatalf("run.Status = %q, want %q for a completed claim that committed no facts", got, want)
	}
	if run.FinishedAt.IsZero() {
		t.Fatal("run.FinishedAt = zero, want non-zero")
	}
	if got := len(completeness); got != 2 {
		t.Fatalf("len(completeness) = %d, want 2 required phases", got)
	}
	for _, state := range completeness {
		if got, want := state.Status, CompletenessStatusReady; got != want {
			t.Fatalf("phase %q status = %q, want %q", state.PhaseName, got, want)
		}
		if !strings.Contains(state.Detail, "factless") {
			t.Fatalf("phase %q detail = %q, want it to name the factless convergence", state.PhaseName, state.Detail)
		}
	}
}

func TestReconcileRunProgressFactlessCountAboveCompletedFails(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 27, 13, 0, 0, 0, time.UTC)
	_, _, err := ReconcileRunProgress(RunProgressSnapshot{
		Run: Run{
			RunID:       "run-factless-invalid",
			TriggerKind: TriggerKindSchedule,
			Status:      RunStatusCollectionActive,
			CreatedAt:   now.Add(-time.Minute),
			UpdatedAt:   now.Add(-time.Minute),
		},
		Collectors: []CollectorRunProgress{{
			CollectorKind:          scope.CollectorTerraformState,
			TotalWorkItems:         1,
			CompletedWorkItems:     0,
			FactlessCompletedItems: 1,
			PublishedPhaseCounts:   map[PhasePublicationKey]int{},
		}},
	}, now)
	if err == nil {
		t.Fatal("ReconcileRunProgress() error = nil, want factless count above completed to fail")
	}
}
