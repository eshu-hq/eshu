// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// casInterleaveCase carries the fixture coordinates for one forced-interleave
// check, so the helper below does not need eight positional parameters.
type casInterleaveCase struct {
	round    int
	scopeID  string
	genID    string
	identity EshuSearchVectorIdentity
	revision int64
	now      time.Time
}

// assertBlockedFinalizeRefusesToPublish is phase 3 of the #5045 contention
// proof, split out of the main test to keep both files under the 500-line cap.
//
// It pins the schedule with a row lock rather than racing for it, because
// releasing goroutines together only makes them runnable: a legal schedule
// still runs one worker's begin and finalize to completion before any other
// worker reaches Postgres, and the phase would pass having never overlapped.
//
// The sequence:
//
//  1. a builder takes a fence
//  2. a transaction locks the scope-state row FOR UPDATE
//  3. that builder's FinalizeReady is launched and BLOCKS on the lock
//  4. while it is blocked, the transaction bumps the fence past it
//  5. the transaction commits and the finalize wakes
//
// Step 5 is the point: the blocked UPDATE re-evaluates its predicate against
// the row as it exists AFTER the commit. That is the EvalPlanQual recheck path
// the #4233 review reasoned about rather than exercised. A finalize that wakes
// and publishes anyway would overwrite a newer build.
//
// The helper fails if the finalize returns while the row is still held, so a
// run in which the interleave did not actually happen cannot report success.
func assertBlockedFinalizeRefusesToPublish(
	t *testing.T,
	ctx context.Context,
	sqlDB *sql.DB,
	store EshuSearchVectorScopeStateStore,
	tc casInterleaveCase,
) {
	t.Helper()

	round := tc.round
	scopeID, genID := tc.scopeID, tc.genID
	identity := tc.identity
	overlapRevision := tc.revision
	now := tc.now

	if _, err := sqlDB.ExecContext(
		ctx, `
		UPDATE eshu_search_document_projection_state
		   SET projection_revision = $3, state = 'ready', updated_at = $4
		 WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, genID, overlapRevision, now,
	); err != nil {
		t.Fatalf("round %d: bump projection revision for overlap phase: %v", round, err)
	}

	staleFence, err := store.BeginBuilding(ctx, scopeID, genID, identity, overlapRevision)
	if err != nil {
		t.Fatalf("round %d: overlap begin building: %v", round, err)
	}

	lockTx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("round %d: begin lock transaction: %v", round, err)
	}
	if _, err := lockTx.ExecContext(
		ctx, `
		SELECT 1
		  FROM eshu_search_vector_scope_state
		 WHERE scope_id = $1 AND generation_id = $2
		   AND provider_profile_id = $3 AND source_class = $4
		   AND embedding_model_id = $5 AND vector_index_version = $6
		   FOR UPDATE`,
		scopeID, genID,
		identity.ProviderProfileID, identity.SourceClass,
		identity.EmbeddingModelID, identity.VectorIndexVersion,
	); err != nil {
		_ = lockTx.Rollback()
		t.Fatalf("round %d: lock scope state row: %v", round, err)
	}

	type finalizeResult struct {
		won bool
		err error
	}
	blocked := make(chan finalizeResult, 1)
	go func() {
		won, err := store.FinalizeReady(ctx, scopeID, genID, identity, overlapRevision, staleFence)
		blocked <- finalizeResult{won: won, err: err}
	}()

	// If it returns while the row is locked, the interleave never happened
	// and everything below would be proving nothing.
	select {
	case res := <-blocked:
		_ = lockTx.Rollback()
		t.Fatalf(
			"round %d: finalize returned (won=%v, err=%v) while the row was locked FOR UPDATE; "+
				"the forced interleave did not happen, so this phase would prove nothing",
			round, res.won, res.err,
		)
	case <-time.After(750 * time.Millisecond):
		// Blocked as intended.
	}

	// Land a newer fence while the finalize is parked behind the lock.
	if _, err := lockTx.ExecContext(
		ctx, `
		UPDATE eshu_search_vector_scope_state
		   SET build_fence = build_fence + 1, state = 'building'
		 WHERE scope_id = $1 AND generation_id = $2
		   AND provider_profile_id = $3 AND source_class = $4
		   AND embedding_model_id = $5 AND vector_index_version = $6`,
		scopeID, genID,
		identity.ProviderProfileID, identity.SourceClass,
		identity.EmbeddingModelID, identity.VectorIndexVersion,
	); err != nil {
		_ = lockTx.Rollback()
		t.Fatalf("round %d: bump fence under lock: %v", round, err)
	}
	if err := lockTx.Commit(); err != nil {
		t.Fatalf("round %d: commit fence bump: %v", round, err)
	}

	select {
	case res := <-blocked:
		if res.err != nil {
			t.Fatalf("round %d: finalize after lock release: %v", round, res.err)
		}
		if res.won {
			t.Fatalf(
				"round %d: a finalize holding fence %d woke from the row lock and published ready "+
					"even though a newer fence had committed while it was blocked: "+
					"the CAS predicate is not re-evaluated after the wait, so a superseded build "+
					"can overwrite a newer one",
				round, staleFence,
			)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("round %d: finalize never returned after the row lock was released", round)
	}

	var overlapState string
	if err := sqlDB.QueryRowContext(
		ctx, `
		SELECT state
		  FROM eshu_search_vector_scope_state
		 WHERE scope_id = $1 AND generation_id = $2
		   AND provider_profile_id = $3 AND source_class = $4
		   AND embedding_model_id = $5 AND vector_index_version = $6`,
		scopeID, genID,
		identity.ProviderProfileID, identity.SourceClass,
		identity.EmbeddingModelID, identity.VectorIndexVersion,
	).Scan(&overlapState); err != nil {
		t.Fatalf("round %d: read persisted scope state after overlap: %v", round, err)
	}
	if overlapState != "building" {
		t.Fatalf(
			"round %d: persisted state = %q after the rejected finalize, want \"building\": "+
				"the newer build's state was overwritten by the stale finalize",
			round, overlapState,
		)
	}
}
