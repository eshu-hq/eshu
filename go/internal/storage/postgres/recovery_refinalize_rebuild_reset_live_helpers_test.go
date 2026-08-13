// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Real-Postgres proofs for the #4594 disaster-recovery rebuild.
//
// A rebuild-from-facts throws the graph away, keeps Postgres, and replays every
// active generation through the projector. Before this change it rebuilt only
// the source-local part of the graph, because three pieces of Postgres state
// that survive a graph wipe told the pipeline the work was already done:
//
//   - succeeded reducer work items, which a re-projection cannot re-open because
//     the enqueue uses ON CONFLICT (work_item_id) DO NOTHING;
//   - shared projection intents with completed_at set, which the partition
//     workers skip and the upsert deliberately never reopens;
//   - graph projection phase rows, which claim canonical nodes are committed for
//     a graph that no longer has any.
//
// These tests pin the reset to exactly the generations being refinalized. The
// two guard proofs at the bottom pin the opposite: that ordinary enqueue and
// ordinary shared-intent upsert still refuse to reset anything.
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run RefinalizeRebuildReset -count=1

// refinalizeRebuildResetLiveDB opens the DSN-gated database and applies the
// bootstrap schema, skipping when no DSN is configured. Schema setup and the
// proof get separate deadlines so one-time DDL cannot spend the proof's budget.
func refinalizeRebuildResetLiveDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()

	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #4594 refinalize rebuild-reset proofs")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	schemaCtx, cancelSchema := context.WithTimeout(context.Background(), 5*time.Minute)
	err = ApplyBootstrap(schemaCtx, SQLDB{DB: db})
	cancelSchema()
	if err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return db, ctx
}

// refinalizeResetScope seeds one scope plus two generations: the active one a
// refinalize covers, and a retired one that must survive untouched. Returning
// both lets a test assert the affected-set predicate by generation, not only by
// scope, which is the difference between "rebuilds the current graph" and
// "re-drives retired history nobody asked for".
func refinalizeResetScope(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	suffix string,
) (scopeID, activeGeneration, retiredGeneration string) {
	t.Helper()

	scopeID = "refinalize-reset-scope-" + suffix
	activeGeneration = "refinalize-reset-active-" + suffix
	retiredGeneration = "refinalize-reset-retired-" + suffix
	now := time.Now().UTC()

	// The scope row lands first: scope_generations carries an FK to it.
	seedRefinalizeResetScopeRow(t, ctx, db, scopeID, activeGeneration, now)

	// Only one generation per scope may be 'active' (scope_generations_active_scope_idx),
	// which is also what makes the retired one a genuine out-of-set control: the
	// refinalize predicate selects ingestion_scopes.active_generation_id, so a
	// superseded generation is exactly the row a rebuild must not re-drive.
	for generationID, status := range map[string]string{
		activeGeneration:  "active",
		retiredGeneration: "superseded",
	} {
		if _, err := db.ExecContext(
			ctx, `
			INSERT INTO scope_generations
			  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
			VALUES ($1, $2, 'manual', $4, $4, $3, $4)
			ON CONFLICT (generation_id) DO NOTHING`,
			generationID, scopeID, status, now,
		); err != nil {
			t.Fatalf("seed scope_generations %s: %v", generationID, err)
		}
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, statement := range []string{
			`DELETE FROM graph_projection_phase_state WHERE scope_id = $1`,
			`DELETE FROM shared_projection_intents WHERE scope_id = $1`,
			`DELETE FROM fact_work_items WHERE scope_id = $1`,
			`DELETE FROM scope_generations WHERE scope_id = $1`,
			`DELETE FROM ingestion_scopes WHERE scope_id = $1`,
		} {
			_, _ = db.ExecContext(cleanupCtx, statement, scopeID)
		}
	})

	return scopeID, activeGeneration, retiredGeneration
}

// seedRefinalizeResetScopeRow inserts or re-points the scope row so
// active_generation_id names the generation the refinalize is expected to cover.
func seedRefinalizeResetScopeRow(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, activeGeneration string,
	now time.Time,
) {
	t.Helper()

	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind,
		   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1::text, 'repository', 'git', $1::text, 'git', $1::text, $2, $2, 'active', $3::text, '{}'::jsonb)
		ON CONFLICT (scope_id) DO UPDATE SET active_generation_id = EXCLUDED.active_generation_id`,
		scopeID, now, activeGeneration,
	); err != nil {
		t.Fatalf("seed ingestion_scopes %s: %v", scopeID, err)
	}
}

// seedRefinalizeResetReducerWork inserts one reducer work item in the requested status,
// through the production enqueue so the row carries exactly the columns the
// projector writes, then moves it to its terminal status.
func seedRefinalizeResetReducerWork(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, generationID, entityKey, status string,
) string {
	t.Helper()

	queue := NewReducerQueue(SQLDB{DB: db}, "refinalize-reset-test", time.Minute)
	intent := projector.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainCodeCallMaterialization,
		EntityKey:    entityKey,
		Reason:       "refinalize rebuild reset proof",
		FactID:       "fact-" + entityKey,
		SourceSystem: "git",
	}
	if _, err := queue.Enqueue(ctx, []projector.ReducerIntent{intent}); err != nil {
		t.Fatalf("seed reducer work item %s: %v", entityKey, err)
	}

	workItemID := reducerWorkItemID(intent)
	if status != "pending" {
		if _, err := db.ExecContext(
			ctx,
			`UPDATE fact_work_items SET status = $2 WHERE work_item_id = $1`,
			workItemID, status,
		); err != nil {
			t.Fatalf("set reducer work item %s to %s: %v", entityKey, status, err)
		}
	}
	return workItemID
}

// refinalizeResetWorkItemStatus reads one work item's status, reporting absence as the empty
// string so a test can tell "deleted" apart from "still here in some state".
func refinalizeResetWorkItemStatus(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) string {
	t.Helper()

	var status string
	err := db.QueryRowContext(
		ctx,
		`SELECT status FROM fact_work_items WHERE work_item_id = $1`, workItemID,
	).Scan(&status)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatalf("read work item %s: %v", workItemID, err)
	}
	return status
}
