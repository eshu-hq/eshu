// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

// describingDrain is a ReducerGraphDrain that also names its canonical-code
// blockers, with a switchable active answer so a test can move the lane from
// one blocked reason to another.
type describingDrain struct {
	describingQuiescence
	activeMu sync.Mutex
	active   bool
}

func (d *describingDrain) HasActiveReducerGraphWork(context.Context) (bool, error) {
	d.activeMu.Lock()
	defer d.activeMu.Unlock()
	return d.active, nil
}

func (d *describingDrain) setActive(active bool) {
	d.activeMu.Lock()
	defer d.activeMu.Unlock()
	d.active = active
}

func blockedScopeIDs(t *testing.T, entries []map[string]any) any {
	t.Helper()
	if len(entries) != 1 {
		t.Fatalf("blocked log lines = %d, want 1", len(entries))
	}
	return entries[0]["blocking_scope_ids"]
}

// TestQuiescenceDescriberMirrorsGatePrecedence pins the blocker sample to the
// dependency that holds the lane. projectionLaneBlocked consults
// ReducerGraphDrain before CanonicalQuiescence, so when the two fields hold
// different describing implementations the blocked log must name the drain's
// scopes, not the standalone checker's.
func TestQuiescenceDescriberMirrorsGatePrecedence(t *testing.T) {
	t.Parallel()

	drain := &describingDrain{describingQuiescence: describingQuiescence{
		uncommitted: true, total: 1, ids: []string{"drain-scope"},
	}}
	checker := &describingQuiescence{total: 1, ids: []string{"checker-scope"}}
	h := newBlockedTelemetryHarness(t, func(r *Runner) {
		r.ReducerGraphDrain = drain
		r.CanonicalQuiescence = checker
	})

	h.process(t)

	got := blockedScopeIDs(t, h.logEntries(t, "code call projection lane blocked"))
	if want := []any{"drain-scope"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("blocking_scope_ids = %v, want %v (the gate's dependency)", got, want)
	}
	if got := checker.describeCount(); got != 0 {
		t.Fatalf("CanonicalQuiescence describe calls = %d, want 0 (the gate never consulted it)", got)
	}
}

// When the gate's own dependency cannot describe, the runner must not borrow
// another dependency's blockers: that would name scopes that are not holding
// the lane.
func TestQuiescenceDescriberDoesNotBorrowFromANonGateDependency(t *testing.T) {
	t.Parallel()

	checker := &describingQuiescence{total: 1, ids: []string{"checker-scope"}}
	h := newBlockedTelemetryHarness(t, func(r *Runner) {
		r.ReducerGraphDrain = staticReducerGraphDrain{uncommittedCanonical: true}
		r.CanonicalQuiescence = checker
	})

	h.process(t)

	entries := h.logEntries(t, "code call projection lane blocked")
	if got := blockedScopeIDs(t, entries); got != nil {
		t.Fatalf("blocking_scope_ids = %v, want absent", got)
	}
	if got := checker.describeCount(); got != 0 {
		t.Fatalf("CanonicalQuiescence describe calls = %d, want 0", got)
	}
}

// TestCodeCallProjectionRunnerLogsClosedEpisodeOnReasonSwitch: a switch from
// one blocked reason to another used to zero the old gauge and restart the
// clock at zero with no close record. The replaced episode must log its close
// (reason and age) before the new episode starts.
func TestCodeCallProjectionRunnerLogsClosedEpisodeOnReasonSwitch(t *testing.T) {
	t.Parallel()

	drain := &describingDrain{describingQuiescence: describingQuiescence{
		uncommitted: true, total: 1, ids: []string{"drain-scope"},
	}}
	h := newBlockedTelemetryHarness(t, func(r *Runner) { r.ReducerGraphDrain = drain })

	h.process(t)
	time.Sleep(20 * time.Millisecond)
	drain.setActive(true)
	h.process(t)

	closed := h.logEntries(t, "code call projection lane released")
	if len(closed) != 1 {
		t.Fatalf("released log lines = %d, want 1 for the replaced episode", len(closed))
	}
	if got, want := closed[0]["blocked_reason"], BlockedReasonCanonicalCodeQuiescence; got != want {
		t.Fatalf("closed blocked_reason = %v, want %v", got, want)
	}
	if got, want := closed[0]["replaced_by"], BlockedReasonReducerGraphWork; got != want {
		t.Fatalf("closed replaced_by = %v, want %v", got, want)
	}
	if age, _ := closed[0]["blocked_seconds"].(float64); age <= 0 {
		t.Fatalf("closed blocked_seconds = %v, want > 0", closed[0]["blocked_seconds"])
	}
	blocked := h.logEntries(t, "code call projection lane blocked")
	if len(blocked) != 2 || blocked[1]["blocked_reason"] != BlockedReasonReducerGraphWork {
		t.Fatalf("blocked log = %v, want a second episode with reason %s", blocked, BlockedReasonReducerGraphWork)
	}
	if got := blockingScopesGauge(h.metrics(t), BlockedReasonCanonicalCodeQuiescence); got != 0 {
		t.Fatalf("replaced episode gauge = %d, want 0", got)
	}
}

// A canonical-quiescence gate wired without the describer port must still
// count the blocked cycle and log it once, but publish no blocker sample.
func TestCodeCallProjectionRunnerNonDescribingGateStillReportsBlock(t *testing.T) {
	t.Parallel()

	h := newBlockedTelemetryHarness(t, func(r *Runner) {
		r.CanonicalQuiescence = staticReducerGraphDrain{uncommittedCanonical: true}
	})

	h.process(t)
	h.process(t)

	rm := h.metrics(t)
	if got := blockedCounterValue(rm, BlockedReasonCanonicalCodeQuiescence); got != 2 {
		t.Fatalf("lane_blocked_total{canonical_code_quiescence} = %d, want 2", got)
	}
	if got := blockingScopesGauge(rm, BlockedReasonCanonicalCodeQuiescence); got != -1 {
		t.Fatalf("lane_blocking_scopes = %d, want no data point (-1)", got)
	}
	entries := h.logEntries(t, "code call projection lane blocked")
	if len(entries) != 1 {
		t.Fatalf("blocked log lines = %d, want exactly 1", len(entries))
	}
	for _, key := range []string{"blocking_scope_ids", "blocking_scope_count", "error"} {
		if _, present := entries[0][key]; present {
			t.Fatalf("blocked log carries %q with no describer: %v", key, entries[0])
		}
	}
}
