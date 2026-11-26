// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
)

// TestWinnersSettleSkipPredicateIsSharedWithAppliedDrain pins the #9975
// drain-discriminator contract: the applied drain and both settle skip
// counts must read the same applied predicate over the admitted window.
// The drain deletes admitted rows the chunk applied (subject to the version
// recheck); the not-in-chunk count reads admitted survivors the chunk did
// not apply and the version-mismatch count reads admitted survivors the
// chunk applied. A predicate that drifts between the three statements
// misattributes the delete shortfall the discriminator must split.
func TestWinnersSettleSkipPredicateIsSharedWithAppliedDrain(t *testing.T) {
	t.Parallel()

	shared := winnersKeyedAppliedPredicate("c")
	if strings.TrimSpace(shared) == "" {
		t.Fatal("winnersKeyedAppliedPredicate(c) is empty")
	}
	for name, statement := range map[string]string{
		"applied drain":    deleteWinnersKeyedAppliedDirtySQL,
		"not-in-chunk":     countWinnersKeyedSettleSkippedNotInChunkSQL,
		"version-mismatch": countWinnersKeyedSettleSkippedVersionMismatchSQL,
	} {
		if !strings.Contains(statement, shared) {
			t.Errorf("%s statement does not carry the shared applied predicate", name)
		}
	}
}

// TestSettleWinnersKeyedDirtyLogReportsSkipReasons pins the per-tick
// admitted-vs-deleted split the arbiter authorized for eshu-hq/eshu#9975
// criterion 4: after the applied drain deletes, the settle reports how many
// admitted rows stayed queued outside the chunk and how many stayed queued
// inside the chunk with a version that moved after admission. The
// over-cap fallback reports zeros: it runs no chunk, so the chunk table
// the split reads does not exist there.
func TestSettleWinnersKeyedDirtyLogReportsSkipReasons(t *testing.T) {
	t.Parallel()

	applied := &settleFakeTx{
		drained:           [][2]any{{"fact_upsert", int64(3)}},
		skippedNotInChunk: 7,
		skippedMismatch:   3,
	}
	_, _, _, _, skips, err := settleWinnersKeyedDirtyLog(context.Background(), applied, false, nil)
	if err != nil {
		t.Fatalf("settle applied: %v", err)
	}
	if skips.NotInChunk != 7 {
		t.Errorf("skipped not-in-chunk = %d, want 7", skips.NotInChunk)
	}
	if skips.VersionMismatch != 3 {
		t.Errorf("skipped version-mismatch = %d, want 3", skips.VersionMismatch)
	}
	var (
		notInChunkIdx = -1
		mismatchIdx   = -1
		drainIdx      = -1
	)
	for i, q := range applied.queries {
		switch q {
		case countWinnersKeyedSettleSkippedNotInChunkSQL:
			notInChunkIdx = i
		case countWinnersKeyedSettleSkippedVersionMismatchSQL:
			mismatchIdx = i
		case deleteWinnersKeyedAppliedDirtySQL:
			drainIdx = i
		}
	}
	if drainIdx < 0 || notInChunkIdx < 0 || mismatchIdx < 0 {
		t.Fatalf("applied settle queries = %d, want drain plus both skip counts", len(applied.queries))
	}
	if drainIdx >= notInChunkIdx || drainIdx >= mismatchIdx {
		t.Errorf("skip counts read before the drain: drain %d, not-in-chunk %d, mismatch %d",
			drainIdx, notInChunkIdx, mismatchIdx)
	}

	fallback := &settleFakeTx{drained: [][2]any{{"fact_upsert", int64(3)}}}
	_, _, _, _, fallbackSkips, err := settleWinnersKeyedDirtyLog(context.Background(), fallback, true, nil)
	if err != nil {
		t.Fatalf("settle over-cap: %v", err)
	}
	if fallbackSkips != (winnersSettleSkips{}) {
		t.Errorf("over-cap skips = %+v, want zeros (no chunk to split against)", fallbackSkips)
	}
	for _, q := range fallback.queries {
		if q == countWinnersKeyedSettleSkippedNotInChunkSQL || q == countWinnersKeyedSettleSkippedVersionMismatchSQL {
			t.Errorf("over-cap settle runs a skip count, want none without the chunk table")
		}
	}
}
