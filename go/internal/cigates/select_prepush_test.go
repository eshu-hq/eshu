// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// FilterPrePush's own selection rules, split from select_test.go: it is the
// `--pre-push` counterpart of FilterByCategory, applied by `ci-gates run
// --pre-push` (scripts/dev/pre-push.sh, the fast local floor before every
// push) to skip gates the registry marks `local.pre_push: deferred` --
// without ever silently dropping a gate that was genuinely triggered.

package cigates_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func localDeferred(cmd, reason string) *cigates.Local {
	return &cigates.Local{Command: cmd, PrePushDeferred: true, PrePushReason: reason}
}

// TestFilterPrePush_Disabled proves --pre-push absent (prePush=false) is a
// no-op: every Selection, deferred or not, passes through unchanged.
func TestFilterPrePush_Disabled(t *testing.T) {
	t.Parallel()
	sels := []cigates.Selection{
		{Gate: gate("deferred-gate", cigates.TierPrePR, cigates.CategoryExactness, []string{"go/**"}, localDeferred("bash slow.sh", "median 70s"), ""), Selected: true, Reason: "triggered"},
	}
	out := cigates.FilterPrePush(sels, false)
	if !out[0].Selected {
		t.Error("Selected = false with prePush=false, want true (deferral must not apply)")
	}
	if out[0].Deferred {
		t.Error("Deferred = true with prePush=false, want false")
	}
}

// TestFilterPrePush_DefersATriggeredGate proves the primary case: a Selected
// gate marked pre_push: deferred is unselected, its Reason names the
// registry's pre_push_reason, and Deferred is set so the caller can print
// DEFER-CI instead of a generic SKIP.
func TestFilterPrePush_DefersATriggeredGate(t *testing.T) {
	t.Parallel()
	sels := []cigates.Selection{
		{Gate: gate("deferred-gate", cigates.TierPrePR, cigates.CategoryExactness, []string{"go/**"}, localDeferred("bash slow.sh", "median 70s of the pre-push run"), ""), Selected: true, Reason: "triggered"},
	}
	out := cigates.FilterPrePush(sels, true)
	if out[0].Selected {
		t.Error("Selected = true, want false for a deferred gate under --pre-push")
	}
	if !out[0].Deferred {
		t.Error("Deferred = false, want true so the caller prints DEFER-CI, not a silent SKIP")
	}
	if out[0].Reason == "" || out[0].Reason == "triggered" {
		t.Errorf("Reason = %q, want it to carry the registry's pre_push_reason", out[0].Reason)
	}
}

// TestFilterPrePush_LeavesNonDeferredGateSelected proves a triggered gate
// with no pre_push field is unaffected by --pre-push.
func TestFilterPrePush_LeavesNonDeferredGateSelected(t *testing.T) {
	t.Parallel()
	sels := []cigates.Selection{
		{Gate: gate("normal-gate", cigates.TierPrePR, cigates.CategoryExactness, []string{"go/**"}, localCmd("bash fast.sh"), ""), Selected: true, Reason: "triggered"},
	}
	out := cigates.FilterPrePush(sels, true)
	if !out[0].Selected {
		t.Error("Selected = false, want true: this gate carries no pre_push deferral")
	}
	if out[0].Deferred {
		t.Error("Deferred = true, want false: this gate was never deferred")
	}
}

// TestFilterPrePush_NeverMarksAnUntriggeredGateAsDeferred proves the "never
// skip silently" contract from the other direction: a gate that was already
// unselected (trigger mismatch) must not be relabeled Deferred=true just
// because it happens to carry a pre_push_reason -- DEFER-CI must mean
// "this gate WOULD have run here, but was deferred", never "this existed".
func TestFilterPrePush_NeverMarksAnUntriggeredGateAsDeferred(t *testing.T) {
	t.Parallel()
	sels := []cigates.Selection{
		{Gate: gate("deferred-gate", cigates.TierPrePR, cigates.CategoryExactness, []string{"go/internal/other/**"}, localDeferred("bash slow.sh", "median 70s"), ""), Selected: false, Reason: "no trigger matched changed paths"},
	}
	out := cigates.FilterPrePush(sels, true)
	if out[0].Deferred {
		t.Error("Deferred = true for a gate that was never triggered, want false")
	}
	if out[0].Selected {
		t.Error("Selected = true, want false (unchanged from the input)")
	}
}
