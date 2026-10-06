// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
)

// TestTargetedMaintenanceErrorsAreMatchableWithStableReasons pins the typed
// error contract the activation-obligation consumer maps to its own port
// errors: every sentinel survives %w wrapping for errors.Is, carries a stable
// reason string for telemetry labels, and the reason set is closed.
func TestTargetedMaintenanceErrorsAreMatchableWithStableReasons(t *testing.T) {
	t.Parallel()

	want := map[error]string{
		ErrTargetedMaintenanceCatalogChanged: "catalog_changed",
		ErrTargetedMaintenanceNoMemoBaseline: "no_memo_baseline",
		ErrTargetedMaintenanceClosureTooDeep: "closure_too_deep",
		ErrTargetedMaintenanceInapplicable:   "inapplicable",
		ErrTargetedMaintenanceNotActive:      "not_active",
		ErrTargetedMaintenanceRetry:          "retry",
	}
	reasons := make([]string, 0, len(want))
	for sentinel, reason := range want {
		wrapped := fmt.Errorf("maintain owed partition: %w", sentinel)
		if !errors.Is(wrapped, sentinel) {
			t.Fatalf("errors.Is(wrapped, %v) = false", sentinel)
		}
		if got := TargetedMaintenanceReason(wrapped); got != reason {
			t.Fatalf("TargetedMaintenanceReason(%v) = %q, want %q", sentinel, got, reason)
		}
		for other := range want {
			if other != sentinel && errors.Is(wrapped, other) {
				t.Fatalf("%v also matches %v", sentinel, other)
			}
		}
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	if got := TargetedMaintenanceReasons(); !reflect.DeepEqual(got, reasons) {
		t.Fatalf("TargetedMaintenanceReasons() = %v, want %v", got, reasons)
	}
	if got := TargetedMaintenanceReason(nil); got != "" {
		t.Fatalf("TargetedMaintenanceReason(nil) = %q, want empty", got)
	}
	if got := TargetedMaintenanceReason(errors.New("db down")); got != "error" {
		t.Fatalf("TargetedMaintenanceReason(untyped) = %q, want error", got)
	}
}

// TestTargetedMaintenanceOutcomeErr pins the per-owed outcome to error
// mapping: only a published phase is success.
func TestTargetedMaintenanceOutcomeErr(t *testing.T) {
	t.Parallel()

	for kind, want := range map[TargetedMaintenanceOutcomeKind]error{
		TargetedMaintenancePublished:    nil,
		TargetedMaintenanceNotActive:    ErrTargetedMaintenanceNotActive,
		TargetedMaintenanceInapplicable: ErrTargetedMaintenanceInapplicable,
		TargetedMaintenanceRetry:        ErrTargetedMaintenanceRetry,
	} {
		outcome := TargetedMaintenanceOutcome{Partition: OwedPartition{ScopeID: "s", GenerationID: "g"}, Kind: kind}
		if got := outcome.Err(); !errors.Is(got, want) || (want == nil && got != nil) {
			t.Fatalf("outcome %s Err() = %v, want %v", kind, got, want)
		}
	}
}

// TestTargetedMaintenanceSnapshotDropsConflictingScopes pins the snapshot
// union invariant: the batch and fan-in guard compare each
// repository's generation under lock with ONE generation per scope. When the
// load set and the affected set disagree about a scope's generation (it
// advanced between the two reads), the scope is left out of the snapshot so
// both guards skip it, and the conflict is reported instead of silently
// letting one read win.
func TestTargetedMaintenanceSnapshotDropsConflictingScopes(t *testing.T) {
	t.Parallel()

	load := partitionSet("git:a", "a-1", "git:b", "b-1", "gcp:c", "c-1")
	affected := partitionSet("git:a", "a-1", "git:b", "b-2", "git:d", "d-1")
	snapshot, conflicts := buildTargetedMaintenanceSnapshot(load, affected)
	wantSnapshot := map[string]string{"git:a": "a-1", "gcp:c": "c-1", "git:d": "d-1"}
	if !reflect.DeepEqual(snapshot, wantSnapshot) {
		t.Fatalf("snapshot = %v, want %v", snapshot, wantSnapshot)
	}
	if want := []string{"git:b"}; !reflect.DeepEqual(conflicts, want) {
		t.Fatalf("conflicts = %v, want %v", conflicts, want)
	}
}

// TestActivationMaintainerMapsTheTargetedPassToTheConsumerContract pins every
// branch of the production maintainer's mapping (#7584): the owed
// partition's own outcome decides not_active and inapplicable even when the
// pass refused; typed refusals become holds with their stable reason; a
// published phase lets Finalize run; retry and untyped errors are failures.
func TestActivationMaintainerMapsTheTargetedPassToTheConsumerContract(t *testing.T) {
	t.Parallel()
	owed := OwedPartition{ScopeID: "git:owed", GenerationID: "gen-owed"}
	result := func(kind TargetedMaintenanceOutcomeKind) TargetedMaintenanceResult {
		return TargetedMaintenanceResult{Outcomes: []TargetedMaintenanceOutcome{
			{Partition: OwedPartition{ScopeID: "git:other", GenerationID: "gen-other"}, Kind: TargetedMaintenancePublished},
			{Partition: owed, Kind: kind},
		}}
	}
	refused := func(sentinel *TargetedMaintenanceError) error {
		return fmt.Errorf("partition-scoped maintenance: %w", sentinel)
	}
	for _, tc := range []struct {
		name       string
		result     TargetedMaintenanceResult
		err        error
		wantNil    bool
		wantIs     error
		wantHold   string
		wantNoHold bool
	}{
		{name: "published proceeds to finalize", result: result(TargetedMaintenancePublished), wantNil: true},
		{name: "not_active proceeds so finalize retires obsolete", result: result(TargetedMaintenanceNotActive), wantNil: true},
		{name: "not_active wins over a catalog refusal", result: result(TargetedMaintenanceNotActive), err: refused(ErrTargetedMaintenanceCatalogChanged), wantNil: true},
		{name: "inapplicable retires", result: result(TargetedMaintenanceInapplicable), wantIs: maintenance.ErrActivationInapplicable, wantNoHold: true},
		{name: "inapplicable wins over a catalog refusal", result: result(TargetedMaintenanceInapplicable), err: refused(ErrTargetedMaintenanceCatalogChanged), wantIs: maintenance.ErrActivationInapplicable, wantNoHold: true},
		{name: "catalog_changed holds", result: result(TargetedMaintenanceRetry), err: refused(ErrTargetedMaintenanceCatalogChanged), wantHold: "catalog_changed"},
		{name: "no_memo_baseline holds", result: result(TargetedMaintenanceRetry), err: refused(ErrTargetedMaintenanceNoMemoBaseline), wantHold: "no_memo_baseline"},
		{name: "closure_too_deep holds", result: result(TargetedMaintenanceRetry), err: refused(ErrTargetedMaintenanceClosureTooDeep), wantHold: "closure_too_deep"},
		{name: "retry is a failure", result: result(TargetedMaintenanceRetry), wantIs: ErrTargetedMaintenanceRetry, wantNoHold: true},
		{name: "untyped error is a failure", result: result(TargetedMaintenanceRetry), err: errors.New("connection reset"), wantNoHold: true},
		{name: "missing outcome is a failure", result: TargetedMaintenanceResult{}, wantNoHold: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotOwed []OwedPartition
			maintainer := ActivationMaintainer{pass: func(_ context.Context, partitions []OwedPartition) (TargetedMaintenanceResult, error) {
				gotOwed = partitions
				return tc.result, tc.err
			}}
			err := maintainer.MaintainActivation(context.Background(), maintenance.ActivationObligation{
				ScopeID: owed.ScopeID, GenerationID: owed.GenerationID, LeaseOwner: "o", LeaseToken: 3,
			})
			if len(gotOwed) != 1 || gotOwed[0] != owed {
				t.Fatalf("owed partitions = %+v, want exactly %+v", gotOwed, owed)
			}
			if tc.wantNil {
				if err != nil {
					t.Fatalf("MaintainActivation() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("MaintainActivation() = nil, want an error")
			}
			var hold *maintenance.ActivationHoldError
			isHold := errors.As(err, &hold)
			if tc.wantHold != "" && (!isHold || hold.Reason() != tc.wantHold) {
				t.Fatalf("MaintainActivation() = %v, want a hold with reason %q", err, tc.wantHold)
			}
			if tc.wantNoHold && isHold {
				t.Fatalf("MaintainActivation() = %v is a hold, want none", err)
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Fatalf("MaintainActivation() = %v, want errors.Is %v", err, tc.wantIs)
			}
		})
	}
}
