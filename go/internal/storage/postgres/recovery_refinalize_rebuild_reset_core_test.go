// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestRefinalizeRebuildResetDeletesOnlySucceededReducerWorkForAffectedGenerations
// is the #4594 failing-first proof for the first guard. A rebuild must clear the
// succeeded reducer rows for the generations it re-projects, because the enqueue
// cannot reopen them, and must leave everything else alone: in-flight work whose
// lease it would otherwise yank, terminal failures the replay endpoint owns, and
// any row belonging to a generation this refinalize does not cover.
func TestRefinalizeRebuildResetDeletesOnlySucceededReducerWorkForAffectedGenerations(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, retiredGeneration := refinalizeResetScope(t, ctx, db, suffix)

	// fact_work_items_reducer_live_lease_uniq allows one live lease per
	// (conflict_domain, conflict_key), and both in-flight statuses would land on
	// the same key within one scope, so `running` gets its own in-set scope. Both
	// scopes are refinalized, so both in-flight rows are inside the affected set
	// and their survival is a real assertion rather than an out-of-set accident.
	otherScopeID, otherGeneration, _ := refinalizeResetScope(t, ctx, db, suffix+"-second")

	succeeded := seedRefinalizeResetReducerWork(t, ctx, db, scopeID, activeGeneration, "succeeded-entity", "succeeded")
	claimed := seedRefinalizeResetReducerWork(t, ctx, db, scopeID, activeGeneration, "claimed-entity", "claimed")
	running := seedRefinalizeResetReducerWork(t, ctx, db, otherScopeID, otherGeneration, "running-entity", "running")
	deadLetter := seedRefinalizeResetReducerWork(t, ctx, db, scopeID, activeGeneration, "dead-entity", "dead_letter")
	retired := seedRefinalizeResetReducerWork(t, ctx, db, scopeID, retiredGeneration, "retired-entity", "succeeded")

	store := NewRecoveryStore(SQLDB{DB: db})
	result, err := store.RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{
		ScopeIDs: []string{scopeID, otherScopeID},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want nil", err)
	}

	if got := refinalizeResetWorkItemStatus(t, ctx, db, succeeded); got != "" {
		t.Fatalf("succeeded reducer work for the refinalized generation survived as %q; "+
			"the re-projection cannot reopen it (ON CONFLICT DO NOTHING), so the domain never re-runs", got)
	}
	for name, workItemID := range map[string]string{
		"claimed":            claimed,
		"running":            running,
		"dead_letter":        deadLetter,
		"retired generation": retired,
	} {
		if got := refinalizeResetWorkItemStatus(t, ctx, db, workItemID); got == "" {
			t.Fatalf("%s reducer work item was deleted; the reset must touch only succeeded rows "+
				"in the generations being refinalized", name)
		}
	}
	if got, want := refinalizeResetWorkItemStatus(t, ctx, db, claimed), "claimed"; got != want {
		t.Fatalf("claimed work item status = %q, want %q: a rebuild must not disturb a live lease", got, want)
	}
	if got, want := result.ReducerWorkDeleted, 1; got != want {
		t.Fatalf("result.ReducerWorkDeleted = %d, want %d", got, want)
	}
}

// TestRefinalizeRebuildResetReopensSharedIntentsForAffectedGenerationsOnly is the
// #4594 failing-first proof for the second guard. The partition workers drain
// only completed_at IS NULL, and the upsert's COALESCE deliberately refuses to
// reopen a completed row, so a rebuild has to clear completed_at here or the
// shared edge families (CALLS, INHERITS, the SQL table edges) stay missing.
func TestRefinalizeRebuildResetReopensSharedIntentsForAffectedGenerationsOnly(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, retiredGeneration := refinalizeResetScope(t, ctx, db, suffix)

	completed := time.Now().UTC()
	store := NewSharedIntentStore(SQLDB{DB: db})
	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:         "refinalize-reset-active-intent-" + suffix,
			ProjectionDomain: reducer.DomainCodeCalls,
			PartitionKey:     "caller->callee",
			ScopeID:          scopeID,
			AcceptanceUnitID: "unit-" + suffix,
			RepositoryID:     "repo-" + suffix,
			SourceRunID:      activeGeneration,
			GenerationID:     activeGeneration,
			Payload:          map[string]any{"action": "write"},
			CreatedAt:        completed,
			CompletedAt:      &completed,
		},
		{
			IntentID:         "refinalize-reset-retired-intent-" + suffix,
			ProjectionDomain: reducer.DomainCodeCalls,
			PartitionKey:     "caller->callee",
			ScopeID:          scopeID,
			AcceptanceUnitID: "unit-" + suffix,
			RepositoryID:     "repo-" + suffix,
			SourceRunID:      retiredGeneration,
			GenerationID:     retiredGeneration,
			Payload:          map[string]any{"action": "write"},
			CreatedAt:        completed,
			CompletedAt:      &completed,
		},
	}
	if err := store.UpsertIntents(ctx, rows); err != nil {
		t.Fatalf("seed shared intents: %v", err)
	}

	recoveryStore := NewRecoveryStore(SQLDB{DB: db})
	result, err := recoveryStore.RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{
		ScopeIDs: []string{scopeID},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want nil", err)
	}

	if completedAt := refinalizeResetIntentCompletedAt(t, ctx, db, rows[0].IntentID); completedAt != nil {
		t.Fatalf("shared intent for the refinalized generation is still completed at %v; "+
			"the partition worker only drains completed_at IS NULL, so its edges never rebuild", *completedAt)
	}
	if completedAt := refinalizeResetIntentCompletedAt(t, ctx, db, rows[1].IntentID); completedAt == nil {
		t.Fatal("shared intent for a generation outside the refinalize was reopened; " +
			"the reset must be scoped to the generations being rebuilt")
	}
	if got, want := result.SharedIntentsReopened, 1; got != want {
		t.Fatalf("result.SharedIntentsReopened = %d, want %d", got, want)
	}
}

// refinalizeResetIntentCompletedAt reads one intent's completed_at, returning nil for a
// reopened row.
func refinalizeResetIntentCompletedAt(t *testing.T, ctx context.Context, db *sql.DB, intentID string) *time.Time {
	t.Helper()

	var completedAt sql.NullTime
	if err := db.QueryRowContext(
		ctx,
		`SELECT completed_at FROM shared_projection_intents WHERE intent_id = $1`, intentID,
	).Scan(&completedAt); err != nil {
		t.Fatalf("read shared intent %s: %v", intentID, err)
	}
	if !completedAt.Valid {
		return nil
	}
	return &completedAt.Time
}

// TestRefinalizeRebuildResetClearsReadinessPhaseStateForAffectedGenerationsOnly
// proves the third statement. graph_projection_phase_state lives in Postgres and
// survives a graph wipe, so after a wipe every readiness gate reads
// "canonical nodes committed" for a graph that holds nothing. A reducer or
// shared-projection row admitted on that stale answer writes into an empty graph:
// the edge Cypher is MATCH-only, so it matches nothing, writes nothing, and still
// acks succeeded. Deleting the rows re-arms the gates to first-ingest behavior.
func TestRefinalizeRebuildResetClearsReadinessPhaseStateForAffectedGenerationsOnly(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, retiredGeneration := refinalizeResetScope(t, ctx, db, suffix)

	now := time.Now().UTC()
	for _, generationID := range []string{activeGeneration, retiredGeneration} {
		if _, err := db.ExecContext(
			ctx, `
			INSERT INTO graph_projection_phase_state
			  (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase, committed_at, updated_at)
			VALUES ($1, $2, $3, $3, 'code_entities_uid', 'canonical_nodes_committed', $4, $4)`,
			scopeID, "unit-"+suffix, generationID, now,
		); err != nil {
			t.Fatalf("seed graph_projection_phase_state %s: %v", generationID, err)
		}
	}

	store := NewRecoveryStore(SQLDB{DB: db})
	result, err := store.RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{
		ScopeIDs: []string{scopeID},
	}, now)
	if err != nil {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want nil", err)
	}

	if refinalizeResetPhaseRowCount(t, ctx, db, scopeID, activeGeneration) != 0 {
		t.Fatal("readiness phase row for the refinalized generation survived; " +
			"a stale 'canonical nodes committed' lets work commit into a wiped graph and ack succeeded")
	}
	if refinalizeResetPhaseRowCount(t, ctx, db, scopeID, retiredGeneration) != 1 {
		t.Fatal("readiness phase row outside the refinalize was cleared; the reset must be generation-scoped")
	}
	if got, want := result.ReadinessPhasesCleared, 1; got != want {
		t.Fatalf("result.ReadinessPhasesCleared = %d, want %d", got, want)
	}
}

// refinalizeResetPhaseRowCount counts readiness phase rows for one scope generation.
func refinalizeResetPhaseRowCount(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string) int {
	t.Helper()

	var count int
	if err := db.QueryRowContext(
		ctx,
		`SELECT count(*) FROM graph_projection_phase_state WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, generationID,
	).Scan(&count); err != nil {
		t.Fatalf("count phase rows: %v", err)
	}
	return count
}

// TestRefinalizeRebuildResetConvergesAcrossTwoCalls proves an interrupted rebuild
// is re-runnable. The runbook's answer to "the rebuild died partway" is to issue
// the same command again, so a second refinalize over the same generations has to
// leave the same state rather than double-delete, error, or reopen work it has
// already handed back to the pipeline.
func TestRefinalizeRebuildResetConvergesAcrossTwoCalls(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, _ := refinalizeResetScope(t, ctx, db, suffix)

	seedRefinalizeResetReducerWork(t, ctx, db, scopeID, activeGeneration, "converge-entity", "succeeded")

	store := NewRecoveryStore(SQLDB{DB: db})
	filter := recovery.RefinalizeFilter{ScopeIDs: []string{scopeID}}

	first, err := store.RefinalizeScopeProjections(ctx, filter, time.Now().UTC())
	if err != nil {
		t.Fatalf("first RefinalizeScopeProjections() error = %v, want nil", err)
	}
	second, err := store.RefinalizeScopeProjections(ctx, filter, time.Now().UTC())
	if err != nil {
		t.Fatalf("second RefinalizeScopeProjections() error = %v, want nil", err)
	}

	if got, want := first.ReducerWorkDeleted, 1; got != want {
		t.Fatalf("first call ReducerWorkDeleted = %d, want %d", got, want)
	}
	if got, want := second.ReducerWorkDeleted, 0; got != want {
		t.Fatalf("second call ReducerWorkDeleted = %d, want %d: "+
			"a re-run must converge, not repeat work the first call already did", got, want)
	}
	if got, want := second.Enqueued, 1; got != want {
		t.Fatalf("second call Enqueued = %d, want %d: the projector row must still be reset to pending", got, want)
	}
}
