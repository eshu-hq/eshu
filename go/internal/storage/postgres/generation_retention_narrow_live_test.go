// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"database/sql"
	"testing"
	"time"
)

// seedNarrowFixture seeds scope-n with n0 (superseded two days ago), n1
// (superseded now, inside the window) and n2 (active), five facts on n0, and
// a (n1 -> n0) link of 100 delta rows, so n0 alone is over a limit of 60 only
// because of its ledger rows.
func seedNarrowFixture(t *testing.T, database *sql.DB) {
	t.Helper()
	ctx := t.Context()
	for i, step := range []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ('scope-n', 'repository', 'git', 'repo-n', 'git', 'repo-n', now(), now(), 'active', '{}'::jsonb)`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at) VALUES
('n0', 'scope-n', 'snapshot', now() - interval '3 days', now() - interval '3 days', 'superseded', now() - interval '2 days'),
('n1', 'scope-n', 'snapshot', now() - interval '1 day', now() - interval '1 day', 'superseded', now())`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('n2', 'scope-n', 'snapshot', now(), now(), 'active')`,
		`UPDATE ingestion_scopes SET active_generation_id = 'n2' WHERE scope_id = 'scope-n'`,
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
SELECT 'n0/' || i, 'scope-n', 'n0', 'repository', 'k' || i, 'git', 'k' || i, now(), now(), '{}'::jsonb
FROM generate_series(1, 5) AS i`,
		`INSERT INTO changed_since_links (scope_id, generation_id, prior_generation_id, link_kind, digest_version,
    delta_rows, files_keys, content_entities_keys, facts_keys, computed_at)
VALUES ('scope-n', 'n1', 'n0', 'incremental', 1, 100, 0, 100, 0, now())`,
		`INSERT INTO changed_since_link_deltas (scope_id, generation_id, prior_generation_id, fact_category,
    classification, stable_fact_key, current_tombstoned)
SELECT 'scope-n', 'n1', 'n0', 'content_entities', 'added', 'ent:' || i, FALSE FROM generate_series(1, 100) AS i`,
	} {
		if _, err := database.ExecContext(ctx, step); err != nil {
			t.Fatalf("seed step %d: %v", i, err)
		}
	}
}

func narrowPolicy() GenerationRetentionPolicy {
	return GenerationRetentionPolicy{
		MinSupersededGenerations: 0, MaxSupersededAge: time.Hour, BatchGenerationLimit: 10,
		BatchRowLimit: 60, PolicyScope: "global", PolicyRevision: "7127-pr3d-c",
	}
}

// TestGenerationRetentionNarrowedCandidateTakenElsewhereLive is PC1 (c) of
// arbiter ruling arb-7127-3d-c. n0 is over the limit only because of its
// ledger rows, so it is a batch of one and the store rolls back to the
// selection savepoint. Between that rollback and the targeted re-lock another
// session can take the candidate:
//
//   - "row locked": it holds n0 FOR UPDATE. The targeted query returns no
//     row, the pass commits, prunes nothing, writes no event and returns no
//     error. Once that session ends, the next pass prunes n0 alone.
//   - "work started": it starts running work on n0. The targeted query's
//     fact_work_items exclusion returns no row, so the pass prunes nothing
//     and the work item survives (a prune would cascade-delete it).
func TestGenerationRetentionNarrowedCandidateTakenElsewhereLive(t *testing.T) {
	t.Run("row locked", func(t *testing.T) {
		database, ctx := openGenerationRetentionMigratedSchema(t)
		seedNarrowFixture(t, database)

		other, err := database.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin other: %v", err)
		}
		defer func() { _ = other.Rollback() }()
		store := NewGenerationRetentionStore(SQLDB{DB: database})
		store.beforeTargetedLock = func() {
			if _, err := other.ExecContext(ctx, `SELECT 1 FROM scope_generations WHERE generation_id = 'n0' FOR UPDATE`); err != nil {
				t.Errorf("other session lock: %v", err)
			}
		}
		result, err := store.PruneSupersededGenerations(ctx, narrowPolicy())
		if err != nil {
			t.Fatalf("PruneSupersededGenerations: %v", err)
		}
		if result.GenerationsPruned != 0 || result.RowsOverLimit != 0 {
			t.Fatalf("pruned %d over by %d, want nothing while n0 is held", result.GenerationsPruned, result.RowsOverLimit)
		}
		var events, n0 int
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM generation_retention_events`).Scan(&events); err != nil {
			t.Fatalf("count events: %v", err)
		}
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM scope_generations WHERE generation_id = 'n0'`).Scan(&n0); err != nil {
			t.Fatalf("count n0: %v", err)
		}
		if events != 0 || n0 != 1 {
			t.Fatalf("%d events and n0 rows %d, want 0 and 1", events, n0)
		}
		_ = other.Rollback()

		store.beforeTargetedLock = nil
		result, err = store.PruneSupersededGenerations(ctx, narrowPolicy())
		if err != nil {
			t.Fatalf("second prune: %v", err)
		}
		if result.GenerationsPruned != 1 || result.RowsOverLimit <= 0 || result.LockedScopeRows != 1 {
			t.Fatalf("second prune = %d over by %d with %d scope rows, want n0 alone over the limit with 1",
				result.GenerationsPruned, result.RowsOverLimit, result.LockedScopeRows)
		}
	})
	t.Run("work started", func(t *testing.T) {
		database, ctx := openGenerationRetentionMigratedSchema(t)
		seedNarrowFixture(t, database)
		store := NewGenerationRetentionStore(SQLDB{DB: database})
		store.beforeTargetedLock = func() {
			if _, err := database.ExecContext(ctx, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id,
    stage, domain, status, created_at, updated_at)
VALUES ('work-n0', 'scope-n', 'n0', 'projector', 'source_local', 'running', now(), now())`); err != nil {
				t.Errorf("start work on n0: %v", err)
			}
		}
		result, err := store.PruneSupersededGenerations(ctx, narrowPolicy())
		if err != nil {
			t.Fatalf("PruneSupersededGenerations: %v", err)
		}
		if result.GenerationsPruned != 0 || result.RowsOverLimit != 0 {
			t.Fatalf("pruned %d over by %d, want nothing while n0 has running work", result.GenerationsPruned, result.RowsOverLimit)
		}
		var events, n0, work, deltas int
		for query, dest := range map[string]*int{
			`SELECT count(*) FROM generation_retention_events`:                                &events,
			`SELECT count(*) FROM scope_generations WHERE generation_id = 'n0'`:               &n0,
			`SELECT count(*) FROM fact_work_items WHERE work_item_id = 'work-n0'`:             &work,
			`SELECT count(*) FROM changed_since_link_deltas WHERE prior_generation_id = 'n0'`: &deltas,
		} {
			if err := database.QueryRowContext(ctx, query).Scan(dest); err != nil {
				t.Fatalf("%s: %v", query, err)
			}
		}
		if events != 0 || n0 != 1 || work != 1 || deltas != 100 {
			t.Fatalf("%d events, n0 rows %d, work rows %d, delta rows %d; want 0, 1, 1, 100", events, n0, work, deltas)
		}
	})
}
