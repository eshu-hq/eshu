// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestReducerStaleSweepReachesPastSparedHeadLive pins the #10863 P2 fix:
// the bounded standalone sweep must advance past a head of permanently
// spared rows instead of re-locking the same first window forever. The
// lock step takes ORDER BY work_item_id LIMIT batch, and the UPDATE
// never terminalizes a sole stale Crossplane row (#10106 carrier spare),
// so a head of at least batchSize such rows returned updated=0 on every
// pass; SweepOnce stopped on updated < batchSize and plain stale rows
// behind that head were never reached. Progress must come from the
// locked window advancing, not from the updated count.
func TestReducerStaleSweepReachesPastSparedHeadLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the stale sweep starvation proof")
	}
	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)
	now := time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC)
	scopeID := "scope-stale-sweep-starve"
	seedStaleGateScope(t, ctx, db, scopeID, now)

	const (
		batchSize  = 3
		sparedRows = 5 // >= batchSize, so the spared head spans locked windows
		plainRows  = 4
	)
	// The spared head sorts first under work_item_id COLLATE "C": every
	// locked window starts there until the sweep learns to move past it.
	for i := 0; i < sparedRows; i++ {
		id := fmt.Sprintf("a-spared-crossplane-%02d", i)
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID: id, scopeID: scopeID, generationID: "gen-old-" + scopeID,
			domain: "crossplane_satisfied_by_materialization", conflictDomain: reducerConflictDomainScope,
			conflictKey: id + "-key", updatedAt: now,
		})
	}
	for i := 0; i < plainRows; i++ {
		id := fmt.Sprintf("z-plain-stale-%02d", i)
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID: id, scopeID: scopeID, generationID: "gen-old-" + scopeID,
			domain: "deployment_mapping", conflictDomain: reducerConflictDomainScope,
			conflictKey: id + "-key", updatedAt: now,
		})
	}

	sweeper := ReducerStaleGenerationSweeper{
		Queue:             staleGateQueue(db, "starve-owner", now.Add(2*time.Hour)),
		BatchSize:         batchSize,
		MaxPassesPerCycle: 10,
	}
	for cycle := 0; cycle < 3; cycle++ {
		sweeper.SweepOnce(ctx)
	}

	for i := 0; i < plainRows; i++ {
		id := fmt.Sprintf("z-plain-stale-%02d", i)
		if status := staleGateRowStatus(t, ctx, db, id); status != "superseded" {
			t.Fatalf("plain stale row %q status = %q after 3 sweep cycles, want superseded (spared head starved the sweep)", id, status)
		}
	}
	for i := 0; i < sparedRows; i++ {
		id := fmt.Sprintf("a-spared-crossplane-%02d", i)
		if status := staleGateRowStatus(t, ctx, db, id); status != "pending" {
			t.Fatalf("sole stale Crossplane row %q status = %q, want pending (#10106 carrier spare must hold)", id, status)
		}
	}
}
