// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossscope

import (
	"testing"
	"time"
)

// TestDecideWaitAnchorEpochFencesResetsAndClears pins the review P3-1 epoch
// rules: an ordinary wait carries the epoch it read, a reset or a clear moves
// to the next epoch, and a cleared tombstone restarts the bound.
func TestDecideWaitAnchorEpochFencesResetsAndClears(t *testing.T) {
	t.Parallel()
	row := committedRow("arn:a")
	row.AnchorEpoch = 4

	carried := DecideWait(waitInput(&row, "gen-1", waitTestCycle, waitTestT0.Add(time.Minute), "arn:a", "arn:b"))
	if carried.ResetAnchor || carried.Row.AnchorEpoch != 4 || !carried.Row.FirstDeferredAt.Equal(row.FirstDeferredAt) {
		t.Fatalf("unsettled wait: reset %v epoch %d anchor %v, want epoch 4 and the kept anchor",
			carried.ResetAnchor, carried.Row.AnchorEpoch, carried.Row.FirstDeferredAt)
	}

	settled := row
	settled.SettledAt = waitTestT0.Add(30 * time.Minute)
	later := waitTestT0.Add(time.Hour)
	reset := DecideWait(waitInput(&settled, "gen-2", waitTestCycle, later, "arn:c"))
	if !reset.ResetAnchor || !reset.Upsert || reset.Row.AnchorEpoch != 5 || !reset.Row.FirstDeferredAt.Equal(later) {
		t.Fatalf("settled wait, new set: %+v, want a reset at epoch 5 anchored now", reset)
	}

	cleared := DecideWait(waitInput(&row, "gen-2", waitTestCycle, later))
	if !cleared.Clear || cleared.Upsert || cleared.Row.AnchorEpoch != 4 || !cleared.Row.ClearedAt.Equal(later) ||
		cleared.Row.CommittedGenerationID != "gen-2" {
		t.Fatalf("emptied set: %+v, want a clear carrying read epoch 4 and the gen-2 marker", cleared)
	}

	tombstone := ReadinessWait{
		ScopeID: waitTestScope, Domain: waitTestDomain, AnchorEpoch: 5,
		CommittedGenerationID: "gen-2", ClearedAt: later, FirstDeferredAt: later, UpdatedAt: later,
	}
	if again := DecideWait(waitInput(&tombstone, "gen-2", waitTestCycle, later.Add(time.Minute))); again.Clear || again.Upsert || !again.Commit {
		t.Fatalf("tombstone, empty set: %+v, want commit with no ledger write", again)
	}
	restart := later.Add(2 * time.Hour)
	fresh := DecideWait(waitInput(&tombstone, "gen-3", waitTestCycle, restart, "arn:d"))
	if !fresh.ResetAnchor || fresh.Row.AnchorEpoch != 6 || !fresh.Row.FirstDeferredAt.Equal(restart) ||
		!fresh.Commit || !fresh.Defer {
		t.Fatalf("tombstone, new set: %+v, want a new bound at epoch 6 that commits then defers", fresh)
	}
	if PollEligible(tombstone, true, "gen-2", waitTestCycle) {
		t.Fatal("PollEligible(tombstone) = true, want false")
	}
}

// TestCommittedInGeneration pins the review P3-2 retract rule input.
func TestCommittedInGeneration(t *testing.T) {
	t.Parallel()
	row := committedRow("arn:a")
	if !CommittedInGeneration(row, true, "gen-1") {
		t.Fatal("CommittedInGeneration(gen-1 row, gen-1) = false, want true")
	}
	if CommittedInGeneration(row, true, "gen-2") || CommittedInGeneration(row, false, "gen-1") ||
		CommittedInGeneration(ReadinessWait{}, true, "") {
		t.Fatal("CommittedInGeneration() = true for another generation, no row, or no marker; want false")
	}
}

// TestDecideWaitSettleAdvancesEpoch is review P3-a: settling moves the row to
// the next epoch, so a lease-expired straggler that read the unsettled row
// cannot overwrite settled_at with NULL and make the next evaluation settle
// (and count abandoned) a second time.
func TestDecideWaitSettleAdvancesEpoch(t *testing.T) {
	t.Parallel()
	row := committedRow("arn:a")
	row.AnchorEpoch = 4
	settle := DecideWait(waitInput(&row, "gen-1", waitTestCycle, row.FirstDeferredAt.Add(ProducerReadinessMaxWait), "arn:a"))
	if settle.Outcome != ReadinessWaitAbandoned || !settle.Upsert || settle.Row.SettledAt.IsZero() {
		t.Fatalf("expired wait: %+v, want an abandoned settle upsert", settle)
	}
	if settle.Row.AnchorEpoch != 5 || !settle.Row.FirstDeferredAt.Equal(row.FirstDeferredAt) {
		t.Fatalf("settle epoch %d anchor %v, want epoch 5 and the kept anchor %v",
			settle.Row.AnchorEpoch, settle.Row.FirstDeferredAt, row.FirstDeferredAt)
	}
}
