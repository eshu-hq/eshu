// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"
)

// seedFingerprintCountFixture seeds one prunable superseded generation
// ("fp-old") with two succeeded work items, one of which carries a
// deployable_unit_reopen_fingerprints row (#8283). The second work item owns
// no fingerprint row, so the count must discriminate 1 from 0.
func seedFingerprintCountFixture(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	steps := []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ('fp-scope', 'repository', 'git', 'fp-repo', 'git', 'fp-repo', now(), now(), 'active', '{}'::jsonb)`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('fp-active', 'fp-scope', 'snapshot', now(), now(), 'active')`,
		`UPDATE ingestion_scopes SET active_generation_id = 'fp-active' WHERE scope_id = 'fp-scope'`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, superseded_at)
VALUES ('fp-old', 'fp-scope', 'snapshot', now() - interval '2 days', now() - interval '2 days',
    'superseded', now() - interval '1 day')`,
		`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
VALUES ('fp-work-1', 'fp-scope', 'fp-old', 'reducer', 'deployable_unit_correlation', 'succeeded', now(), now()),
       ('fp-work-2', 'fp-scope', 'fp-old', 'reducer', 'deployable_unit_correlation', 'succeeded', now(), now())`,
		`INSERT INTO deployable_unit_reopen_fingerprints (work_item_id, repo_ids, input_fingerprint, logic_version, claimed_at, recorded_at)
VALUES ('fp-work-1', '{}', 'fp-fingerprint', 1, now(), now())`,
	}
	for i, step := range steps {
		if _, err := database.ExecContext(ctx, step); err != nil {
			t.Fatalf("fingerprint fixture step %d: %v", i, err)
		}
	}
}

// TestGenerationRetentionCountsReopenFingerprintsLive proves the row-count
// query counts the fingerprint rows the prune cascades away (#8300): the
// reported count for deployable_unit_reopen_fingerprints must equal the rows
// the prune actually deletes. Without the arm the prune still deletes the
// row through the fact_work_items cascade while the count reports nothing,
// so BatchRowLimit and the retention events under-count by one row per
// fingerprinted work item.
func TestGenerationRetentionCountsReopenFingerprintsLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	seedFingerprintCountFixture(t, ctx, database)

	tx, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	totals, _, _, err := GenerationRetentionStore{}.countRows(ctx, tx, []string{"fp-scope"}, []string{"fp-old"})
	_ = tx.Rollback()
	if err != nil {
		t.Fatalf("countRows() error = %v", err)
	}
	if got := totals["deployable_unit_reopen_fingerprints"]; got != 1 {
		t.Errorf("row count for deployable_unit_reopen_fingerprints = %d, want 1", got)
	}
	if got := totals["fact_work_items"]; got != 2 {
		t.Errorf("row count for fact_work_items = %d, want 2", got)
	}

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	result, err := store.PruneSupersededGenerations(ctx, GenerationRetentionPolicy{
		MinSupersededGenerations: 0,
		MaxSupersededAge:         time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            1_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "8300-fingerprint-count",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 1 {
		t.Fatalf("GenerationsPruned = %d, want 1", result.GenerationsPruned)
	}
	if got := result.RowsPruned["deployable_unit_reopen_fingerprints"]; got != 1 {
		t.Errorf("RowsPruned[deployable_unit_reopen_fingerprints] = %d, want 1", got)
	}
	var remaining int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM deployable_unit_reopen_fingerprints`).Scan(&remaining); err != nil {
		t.Fatalf("count fingerprints after prune: %v", err)
	}
	if remaining != 0 {
		t.Errorf("deployable_unit_reopen_fingerprints after prune = %d, want 0", remaining)
	}
}

// retentionCountArmTables lists every table_name literal the row-count query
// counts, derived from the shipped constant so the catalog test below fails
// when a new cascading table has no arm rather than when this list drifts.
var retentionCountArmTablePattern = regexp.MustCompile(`'([a-z0-9_]+)' AS table_name`)

// TestGenerationRetentionRowCountsCoverCascadeTablesLive lists every table
// the prune deletes through a cascading foreign key — transitively from
// fact_work_items and scope_generations — on the migrated schema, and
// requires the row-count query to count it (#8300). A new cascade child or
// grandchild that lands without an arm fails here instead of silently
// under-counting BatchRowLimit and the retention events.
func TestGenerationRetentionRowCountsCoverCascadeTablesLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	rows, err := database.QueryContext(ctx, `
WITH RECURSIVE cascade_tree AS (
    SELECT con.conrelid AS child_oid
    FROM pg_constraint AS con
    JOIN pg_class AS parent ON parent.oid = con.confrelid
    WHERE con.contype = 'f' AND con.confdeltype = 'c'
      AND parent.relname IN ('scope_generations', 'fact_work_items')
    UNION
    SELECT con.conrelid
    FROM pg_constraint AS con
    JOIN cascade_tree AS tree ON tree.child_oid = con.confrelid
    WHERE con.contype = 'f' AND con.confdeltype = 'c'
)
SELECT DISTINCT child.relname
FROM cascade_tree
JOIN pg_class AS child ON child.oid = cascade_tree.child_oid
JOIN pg_namespace AS ns ON ns.oid = child.relnamespace AND ns.nspname = current_schema()
ORDER BY 1`)
	if err != nil {
		t.Fatalf("list cascade children: %v", err)
	}
	var children []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			_ = rows.Close()
			t.Fatalf("scan cascade child: %v", err)
		}
		children = append(children, table)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		t.Fatalf("list cascade children: %v", err)
	}
	_ = rows.Close()
	if len(children) == 0 {
		t.Fatal("no cascade children found: the catalog query matches nothing")
	}

	arms := map[string]bool{}
	for _, match := range retentionCountArmTablePattern.FindAllStringSubmatch(generationRetentionRowCountsQuery, -1) {
		arms[match[1]] = true
	}
	if len(arms) == 0 {
		t.Fatal("no table_name arms found: the arm pattern matches nothing")
	}
	for _, child := range children {
		if !arms[child] {
			t.Errorf("cascade child %s has no arm in generationRetentionRowCountsQuery", child)
		}
	}
}
