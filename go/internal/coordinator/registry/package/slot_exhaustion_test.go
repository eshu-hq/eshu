// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packages

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestPackageRegistryDerivationReportsATruncatedSlotAsExhausted is the #9422
// exhaustion rule on the package-registry planner, which shares the
// owned-package reader. With target_limit L = 2 the read takes L+1 = 3 rows of
// a 4-row slot and one of them is not a derivable package name, so the 2
// derivable rows fill the budget with no candidate left; the unread slot
// target must still report budget exhaustion.
func TestPackageRegistryDerivationReportsATruncatedSlotAsExhausted(t *testing.T) {
	t.Parallel()

	owned := makeOwnedNPMTargets(3)
	owned[1].PackageName = "" // not derivable: NormalizePackageIdentity rejects it
	skipped := planPackageSlotRun(t, owned, workflow.OwnedPackageDependencyTargetWindow{
		TotalTargets: 40, SlotCount: 32, Slot: 7, SlotSize: 4, RowsRead: 3,
	})
	if len(skipped) != 1 || skipped[0].Reason != schedule.DerivedTargetSkipReasonBudgetExhausted {
		t.Fatalf("skipped = %#v, want one budget-exhausted row", skipped)
	}
	if got := skipped[0]; got.SkippedCount != 1 || got.SelectedCount != 2 {
		t.Fatalf("budget row = %#v, want skipped 1 (unread), selected 2", got)
	}
}

// TestPackageRegistryDerivationWholeSlotAtTheLimitIsNotExhausted is the
// inverse: exactly L derivable rows, the whole slot read, no exhaustion.
func TestPackageRegistryDerivationWholeSlotAtTheLimitIsNotExhausted(t *testing.T) {
	t.Parallel()

	skipped := planPackageSlotRun(t, makeOwnedNPMTargets(2), workflow.OwnedPackageDependencyTargetWindow{
		TotalTargets: 40, SlotCount: 32, Slot: 7, SlotSize: 2, RowsRead: 2,
	})
	if len(skipped) != 0 {
		t.Fatalf("skipped = %#v, want none for a whole slot at the limit", skipped)
	}
}

func planPackageSlotRun(
	t *testing.T,
	owned []workflow.OwnedPackageDependencyTarget,
	window workflow.OwnedPackageDependencyTargetWindow,
) []schedule.DerivedTargetSkipEvidence {
	t.Helper()
	observedAt := time.Date(2026, time.October, 28, 17, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "collector-package-registry",
		CollectorKind:  scope.CollectorPackageRegistry,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm"],"target_limit":2,"version_limit":1}}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
	run, _, err := WorkPlanner{}.PlanPackageRegistryWork(context.Background(), PlanRequest{
		Instance:            instance,
		ObservedAt:          observedAt,
		PlanKey:             "continuous-20261028T170000Z",
		OwnedPackageTargets: owned,
		OwnedPackageWindow:  window,
	})
	if err != nil {
		t.Fatalf("PlanPackageRegistryWork() error = %v, want nil", err)
	}
	var requested struct {
		SkippedTargets []schedule.DerivedTargetSkipEvidence `json:"skipped_targets"`
	}
	if err := json.Unmarshal([]byte(run.RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", run.RequestedScopeSet, err)
	}
	return requested.SkippedTargets
}
