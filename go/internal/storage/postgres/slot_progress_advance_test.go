// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func slotProgressAdvanceFixture(items []workflow.WorkItem) workflow.OwnedTargetSlotAdvance {
	return workflow.OwnedTargetSlotAdvance{
		CollectorKind:       items[0].CollectorKind,
		CollectorInstanceID: items[0].CollectorInstanceID,
		DerivationKind:      workflow.SlotDerivationKindVulnerability,
		WindowStartUnix:     180000,
		PlannedAt:           time.Date(2026, time.May, 21, 12, 0, 0, 0, time.UTC),
		Slots:               []int{3, 9},
	}
}

// TestWorkflowControlStoreSlotProgressAdvancesOnEveryCommitPath is guard T12
// (#9776). The compare-and-set must run inside the admission transaction, after
// the per-instance advisory lock and before commit, on every path that commits:
// the terminal-run skip, the zero-eligible skip, the insert path, and the
// advance-only path an empty pick takes. Removing it from any one path leaves a
// slot that is oldest at every tick, so the rotation stalls. The planted
// removals recorded in the evidence note turn each subtest RED.
func TestWorkflowControlStoreSlotProgressAdvancesOnEveryCommitPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		rows     []queueFakeRows
		advance  bool // advance-only path: no items
		wantCAS  int
		wantLock bool
	}{
		{name: "terminal-run skip", rows: []queueFakeRows{{rows: [][]any{{true}}}}},
		{name: "zero-eligible skip", rows: []queueFakeRows{{rows: [][]any{{false}}}, {rows: nil}}},
		{name: "insert path", rows: []queueFakeRows{{rows: [][]any{{false}}}, {rows: [][]any{{0}, {1}}}}},
		{name: "advance-only path for an empty pick", advance: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := &orderedStatementRecorder{rows: tc.rows}
			store := NewWorkflowControlStore(recorder)
			run, items := workflowPlanningLockFixture(t)
			advance := slotProgressAdvanceFixture(items)

			var result workflow.SlotAdvanceResult
			var err error
			if tc.advance {
				result, err = store.AdvanceOwnedTargetSlotProgress(context.Background(), advance)
			} else {
				_, result, err = store.CreateRunWithWorkItemsAdvancingSlotProgress(context.Background(), run, items, advance)
			}
			if err != nil {
				t.Fatalf("admission error = %v, want nil", err)
			}

			lockAt := recorder.indexOfStatement("pg_advisory_xact_lock")
			casAt := recorder.indexOfStatement("INSERT INTO owned_target_slot_progress")
			if lockAt < 0 {
				t.Fatal("no planning lock was taken before the slot-progress write (#4586)")
			}
			if casAt < 0 {
				t.Fatal("the slot-progress compare-and-set never ran on this commit path (#9776): the picked slots stay oldest and the rotation stalls")
			}
			if casAt < lockAt {
				t.Fatalf("slot-progress write at statement %d precedes the planning lock at %d: the lock must come first", casAt, lockAt)
			}
			if !tc.advance {
				if readAt := recorder.indexOfStatement("planned_targets"); tc.name != "terminal-run skip" && readAt >= 0 && casAt < readAt {
					t.Fatalf("slot-progress write at %d precedes the planned-target read at %d", casAt, readAt)
				}
			}
			if insertAt := recorder.indexOfStatement("INSERT INTO workflow_work_items"); insertAt >= 0 && casAt < insertAt {
				t.Fatalf("slot-progress write at %d precedes the work-item insert at %d: progress must move with the admission", casAt, insertAt)
			}
			if !recorder.didCommit() || recorder.didRollBack() {
				t.Fatalf("committed=%v rolledBack=%v, want a clean commit so lock, admission and progress move together", recorder.didCommit(), recorder.didRollBack())
			}
			if result.Requested != len(advance.Slots) {
				t.Fatalf("result.Requested = %d, want %d", result.Requested, len(advance.Slots))
			}
			args := recorder.argsOfStatements("INSERT INTO owned_target_slot_progress")
			if len(args) != 5 {
				t.Fatalf("slot-progress write bound %d args, want one statement of 5 (once per transaction)", len(args))
			}
			if got, ok := args[4].([]int64); !ok || !slices.Equal(got, []int64{3, 9}) {
				t.Fatalf("slot-progress write bound slots %v, want [3 9]", args[4])
			}
		})
	}
}

// TestWorkflowControlStoreSlotProgressCompareAndSetIsMonotonic is T8: the
// statement only ever raises last_planned_window_start, so a coordinator whose clock
// is one bucket behind moves zero rows and never regresses a slot, and zero
// moved rows is reported as a lost race rather than an error.
func TestWorkflowControlStoreSlotProgressCompareAndSetIsMonotonic(t *testing.T) {
	t.Parallel()

	if !strings.Contains(workflowSlotProgressAdvanceQuery,
		"WHERE owned_target_slot_progress.last_planned_window_start < EXCLUDED.last_planned_window_start") {
		t.Fatalf("compare-and-set is not monotonic (< on last_planned_window_start):\n%s", workflowSlotProgressAdvanceQuery)
	}
	// Exec 1 is the advisory lock, exec 2 the compare-and-set; it moves 0 rows.
	recorder := &orderedStatementRecorder{execRows: []int64{1, 0}}
	store := NewWorkflowControlStore(recorder)
	_, items := workflowPlanningLockFixture(t)
	result, err := store.AdvanceOwnedTargetSlotProgress(context.Background(), slotProgressAdvanceFixture(items))
	if err != nil {
		t.Fatalf("AdvanceOwnedTargetSlotProgress() error = %v, want nil: a lost race is not an error", err)
	}
	if result.Moved != 0 || !result.LostRace() {
		t.Fatalf("result = %#v, want 0 moved and LostRace", result)
	}
}

func TestWorkflowControlStoreAdvanceWithoutSlotsTakesNoLockAndWritesNothing(t *testing.T) {
	t.Parallel()

	recorder := &orderedStatementRecorder{}
	store := NewWorkflowControlStore(recorder)
	_, items := workflowPlanningLockFixture(t)
	advance := slotProgressAdvanceFixture(items)
	advance.Slots = nil
	result, err := store.AdvanceOwnedTargetSlotProgress(context.Background(), advance)
	if err != nil || result != (workflow.SlotAdvanceResult{}) {
		t.Fatalf("result = %#v, err = %v, want zero result and nil error", result, err)
	}
	if recorder.indexOfStatement("owned_target_slot_progress") >= 0 || recorder.indexOfStatement("pg_advisory_xact_lock") >= 0 {
		t.Fatal("an advance with no slots must not open a transaction or write")
	}
}

// TestWorkflowControlStoreAdvanceOnlyLocksTheItemPathKey pins that the
// advance-only path contends on the same advisory key the admission path takes
// for the same collector instance, so an empty pick and a work-bearing pick of
// two coordinators serialize.
func TestWorkflowControlStoreAdvanceOnlyLocksTheItemPathKey(t *testing.T) {
	t.Parallel()

	_, items := workflowPlanningLockFixture(t)
	advance := slotProgressAdvanceFixture(items)

	viaItems := &orderedStatementRecorder{rows: []queueFakeRows{{rows: [][]any{{false}}}, {rows: [][]any{{0}}}}}
	run, _ := workflowPlanningLockFixture(t)
	if _, _, err := NewWorkflowControlStore(viaItems).CreateRunWithWorkItemsAdvancingSlotProgress(context.Background(), run, items[:1], advance); err != nil {
		t.Fatalf("admission error = %v", err)
	}
	advanceOnly := &orderedStatementRecorder{}
	if _, err := NewWorkflowControlStore(advanceOnly).AdvanceOwnedTargetSlotProgress(context.Background(), advance); err != nil {
		t.Fatalf("advance-only error = %v", err)
	}
	a := viaItems.argsOfStatements("pg_advisory_xact_lock")
	b := advanceOnly.argsOfStatements("pg_advisory_xact_lock")
	if len(a) != 1 || len(b) != 1 || a[0] != b[0] {
		t.Fatalf("lock keys differ: item path %v, advance-only path %v", a, b)
	}
}

// TestWorkflowControlStoreSlotProgressRejectsUnsafeAdvances covers the P3-2 and
// P3-3 review findings. A duplicate slot makes the upsert fail with "cannot
// affect row a second time", and an advance whose collector kind or instance
// differs from the admitted items would write progress under a lock the
// transaction does not hold. Both are refused before any statement runs.
func TestWorkflowControlStoreSlotProgressRejectsUnsafeAdvances(t *testing.T) {
	t.Parallel()

	run, items := workflowPlanningLockFixture(t)
	cases := []struct {
		name    string
		mutate  func(*workflow.OwnedTargetSlotAdvance)
		wantErr string
	}{
		{"duplicate slot", func(a *workflow.OwnedTargetSlotAdvance) { a.Slots = []int{3, 3} }, "duplicate"},
		{"other collector instance", func(a *workflow.OwnedTargetSlotAdvance) { a.CollectorInstanceID = "other-instance" }, "does not match"},
		{"other collector kind", func(a *workflow.OwnedTargetSlotAdvance) { a.CollectorKind = "some_other_kind" }, "does not match"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := &orderedStatementRecorder{}
			store := NewWorkflowControlStore(recorder)
			advance := slotProgressAdvanceFixture(items)
			tc.mutate(&advance)

			_, _, err := store.CreateRunWithWorkItemsAdvancingSlotProgress(context.Background(), run, items, advance)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want one containing %q", err, tc.wantErr)
			}
			if len(recorder.statements) != 0 {
				t.Fatalf("ran %d statements before refusing an unsafe advance, want none", len(recorder.statements))
			}
		})
	}
}
