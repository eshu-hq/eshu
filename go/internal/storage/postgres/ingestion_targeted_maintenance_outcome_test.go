// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
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
