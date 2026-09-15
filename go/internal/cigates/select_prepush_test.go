// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// FilterPrePush's own selection rules, split from select_test.go: it is the
// `--pre-push` counterpart of FilterByCategory, applied by `ci-gates run
// --pre-push` (scripts/dev/pre-push.sh, the fast local floor before every
// push) to skip gates the registry marks `local.pre_push: deferred` --
// without ever silently dropping a gate that was genuinely triggered.

package cigates_test

import (
	"strings"
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

// TestFilterPrePush_RunsFloorGate proves a triggered gate registered
// `local.pre_push: floor` stays selected under --pre-push.
func TestFilterPrePush_RunsFloorGate(t *testing.T) {
	t.Parallel()
	floor := &cigates.Local{Command: "bash fast.sh", PrePushFloor: true}
	sels := []cigates.Selection{
		{Gate: gate("floor-gate", cigates.TierPrePR, cigates.CategoryExactness, []string{"go/**"}, floor, ""), Selected: true, Reason: "triggered"},
	}
	out := cigates.FilterPrePush(sels, true)
	if !out[0].Selected {
		t.Error("Selected = false, want true: floor gates run in the pre-push lane")
	}
	if out[0].Deferred {
		t.Error("Deferred = true, want false for a floor gate")
	}
}

// TestFilterPrePush_DefersUnlistedGate proves the floor is an allowlist: a
// triggered gate with no pre_push field is deferred to CI under --pre-push,
// with a reason that names where it still runs. Measured on a one-line Go
// change, a denylist of slow gates still ran for more than 15 minutes because
// most registry gates trigger on go/**.
func TestFilterPrePush_DefersUnlistedGate(t *testing.T) {
	t.Parallel()
	sels := []cigates.Selection{
		{Gate: gate("normal-gate", cigates.TierPrePR, cigates.CategoryExactness, []string{"go/**"}, localCmd("bash other.sh"), ""), Selected: true, Reason: "triggered"},
	}
	out := cigates.FilterPrePush(sels, true)
	if out[0].Selected {
		t.Error("Selected = true, want false: only floor gates run under --pre-push")
	}
	if !out[0].Deferred {
		t.Error("Deferred = false, want true so the caller prints DEFER-CI")
	}
	if !strings.Contains(out[0].Reason, "make pre-pr") || !strings.Contains(out[0].Reason, "CI") {
		t.Errorf("Reason = %q, want it to say the gate still runs in make pre-pr and CI", out[0].Reason)
	}
}

// TestFilterPrePush_DefaultReasonForBlockingGate proves the default reason
// for a triggered blocking gate deferred with no gate-specific
// pre_push_reason truthfully says it still blocks merge in CI -- true for
// every blocking gate, since the registry loader (parsePrePush) and
// ValidateRequiredStatusChecks both refuse a blocking gate with no
// ci.workflow/ci.job destination.
func TestFilterPrePush_DefaultReasonForBlockingGate(t *testing.T) {
	t.Parallel()
	sels := []cigates.Selection{
		{Gate: gate("blocking-gate", cigates.TierPrePR, cigates.CategoryExactness, []string{"go/**"}, localCmd("bash other.sh"), ""), Selected: true, Reason: "triggered"},
	}
	out := cigates.FilterPrePush(sels, true)
	if !strings.Contains(out[0].Reason, "blocks merge in CI") {
		t.Errorf("Reason = %q, want it to say the gate still blocks merge in CI", out[0].Reason)
	}
}

// TestFilterPrePush_DefaultReasonForAdvisoryGateWithCI proves the default
// reason for a triggered advisory (blocking:false) gate that still has a CI
// destination does not claim it blocks merge -- an advisory gate failing in
// CI never blocks merge, only make pre-pr and CI enforce it at all.
func TestFilterPrePush_DefaultReasonForAdvisoryGateWithCI(t *testing.T) {
	t.Parallel()
	g := cigates.Gate{
		ID: "advisory-gate", Name: "advisory-gate",
		Category: cigates.CategoryExactness, Tier: cigates.TierPrePR,
		Blocking: false, Triggers: []string{"go/**"},
		Local: localCmd("bash advisory.sh"),
		CI:    cigates.CI{Workflow: "advisory.yml", Job: "advisory"},
	}
	sels := []cigates.Selection{{Gate: g, Selected: true, Reason: "triggered"}}
	out := cigates.FilterPrePush(sels, true)
	if strings.Contains(out[0].Reason, "blocks merge in CI") {
		t.Errorf("Reason = %q, want it not to claim CI-merge-blocking for an advisory gate", out[0].Reason)
	}
	if !strings.Contains(out[0].Reason, "advisory") {
		t.Errorf("Reason = %q, want it to say the gate is advisory", out[0].Reason)
	}
}

// TestFilterPrePush_DefaultReasonForGateWithNoCIWorkflow proves the default
// reason for a triggered gate with no CI workflow at all (e.g.
// docs-contradiction) says it is local-only, run via make pre-pr -- it must
// not claim CI coverage that does not exist.
func TestFilterPrePush_DefaultReasonForGateWithNoCIWorkflow(t *testing.T) {
	t.Parallel()
	g := cigates.Gate{
		ID: "local-only-gate", Name: "local-only-gate",
		Category: cigates.CategoryExactness, Tier: cigates.TierPrePR,
		Blocking: false, Triggers: []string{"go/**"},
		Local: localCmd("bash local-only.sh"),
		CI:    cigates.CI{},
	}
	sels := []cigates.Selection{{Gate: g, Selected: true, Reason: "triggered"}}
	out := cigates.FilterPrePush(sels, true)
	if strings.Contains(out[0].Reason, "blocks merge in CI") || strings.Contains(out[0].Reason, "runs in make pre-pr and") {
		t.Errorf("Reason = %q, want it not to claim CI enforcement: this gate has no CI workflow", out[0].Reason)
	}
	if !strings.Contains(out[0].Reason, "no CI workflow") {
		t.Errorf("Reason = %q, want it to say there is no CI workflow", out[0].Reason)
	}
	if !strings.Contains(out[0].Reason, "make pre-pr") {
		t.Errorf("Reason = %q, want it to say the gate still runs in make pre-pr", out[0].Reason)
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
