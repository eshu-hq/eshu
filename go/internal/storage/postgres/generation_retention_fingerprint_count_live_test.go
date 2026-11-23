// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
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

// seedRetentionSupports seeds a scope with a prunable superseded generation
// and a recent superseded one, a few content facts on the old generation so
// the cycle selects it, and the per-support rows (#10880 PR-B1) the supports
// arm must count: oldRows on the old generation, keptRows on the kept one.
func seedRetentionSupports(t *testing.T, ctx context.Context, database *sql.DB, scope, oldGen, keptGen string, now time.Time, oldRows, keptRows int) {
	t.Helper()
	testfixtures.SeedScope(t, ctx, database, scope)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scope, oldGen, now.Add(-10*24*time.Hour))
	testfixtures.SeedSupersededGeneration(t, ctx, database, scope, keptGen, now.Add(-time.Hour))
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
SELECT md5($2 || '-' || n), $1, $2, 'content_entity', 'k-' || n, 'git', 'k-' || n, now(), now(), '{}'::jsonb
FROM generate_series(1, 50) AS n`, scope, oldGen); err != nil {
		t.Fatalf("seed facts: %v", err)
	}
	for generation, rows := range map[string]int{oldGen: oldRows, keptGen: keptRows} {
		if _, err := database.ExecContext(ctx, `
INSERT INTO supply_chain_impact_finding_supports (fact_id, canonical_key, scope_id, generation_id, finding_id,
    has_payload_finding_id, priority_score, source_confidence, suppression_state, payload)
SELECT $2 || '-support-' || n, 'key-' || n, $1, $2, 'finding-' || $2 || '-' || n, 1, 0, 'high', 'none', '{}'::jsonb
FROM generate_series(1, $3::int) AS n`, scope, generation, rows); err != nil {
			t.Fatalf("seed supports for %s: %v", generation, err)
		}
	}
}

// TestGenerationRetentionSupportsArmCountsExactlyTheCascadeEnvelopeLive proves
// the supports arm (#10880 PR-B1) charges exactly the rows the generation
// delete cascades away: a candidate with seven supports reports seven, the
// kept generation's rows are not charged, and the prune removes the old rows
// and leaves the kept ones.
func TestGenerationRetentionSupportsArmCountsExactlyTheCascadeEnvelopeLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scope, oldGen, keptGen = "supports-count", "supports-count-old", "supports-count-kept"
	seedRetentionSupports(t, ctx, database, scope, oldGen, keptGen, now, 7, 3)

	tx, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	totals, _, _, err := GenerationRetentionStore{}.countRows(ctx, tx, []string{scope}, []string{oldGen})
	_ = tx.Rollback()
	if err != nil {
		t.Fatalf("countRows() error = %v", err)
	}
	if got := totals["supply_chain_impact_finding_supports"]; got != 7 {
		t.Fatalf("supports row count = %d, want 7 (the old generation's rows only)", got)
	}

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(ctx, chunkDrainPolicy())
	if err != nil || result.GenerationsPruned != 1 {
		t.Fatalf("PruneSupersededGenerations() = %d pruned, %v; want 1, nil", result.GenerationsPruned, err)
	}
	for generation, want := range map[string]int{oldGen: 0, keptGen: 3} {
		var left int
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM supply_chain_impact_finding_supports WHERE generation_id = $1`, generation).Scan(&left); err != nil {
			t.Fatalf("count supports for %s: %v", generation, err)
		}
		if left != want {
			t.Errorf("supports left for %s = %d, want %d", generation, left, want)
		}
	}
}

// TestGenerationRetentionRefusesWithoutValidSupportsGenerationIndexLive proves
// a missing or partial-only supports generation index refuses the cycle, names
// the index and prunes nothing, and that restoring it lets the next cycle run.
func TestGenerationRetentionRefusesWithoutValidSupportsGenerationIndexLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scope, oldGen, keptGen = "supports-refuse", "supports-refuse-old", "supports-refuse-kept"
	seedRetentionSupports(t, ctx, database, scope, oldGen, keptGen, now, 2, 1)
	const index = "supply_chain_impact_finding_supports_generation_idx"
	restore := `CREATE INDEX ` + index + ` ON supply_chain_impact_finding_supports (generation_id)`
	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	for name, setup := range map[string][]string{
		"supports-dropped":      {"DROP INDEX " + index},
		"supports-partial-only": {"DROP INDEX " + index, restore + ` WHERE suppression_state = 'none'`},
	} {
		t.Run(name, func(t *testing.T) {
			for _, statement := range setup {
				if _, err := database.ExecContext(ctx, statement); err != nil {
					t.Fatalf("setup %q: %v", statement, err)
				}
			}
			_, err := store.PruneSupersededGenerations(ctx, chunkDrainPolicy())
			if !errors.Is(err, ErrGenerationRetentionKeyIndexUnavailable) || !strings.Contains(err.Error(), index) {
				t.Fatalf("PruneSupersededGenerations() error = %v, want ErrGenerationRetentionKeyIndexUnavailable naming %s", err, index)
			}
			if remaining := remainingScopeGenerations(t, ctx, database, []string{oldGen}); !remaining[oldGen] {
				t.Fatal("refused cycle pruned the superseded generation")
			}
			for _, statement := range []string{"DROP INDEX IF EXISTS " + index, restore} {
				if _, err := database.ExecContext(ctx, statement); err != nil {
					t.Fatalf("restore %s: %v", index, err)
				}
			}
		})
	}
	result, err := store.PruneSupersededGenerations(ctx, chunkDrainPolicy())
	if err != nil || result.GenerationsPruned != 1 {
		t.Fatalf("PruneSupersededGenerations() after restoring the index = %d pruned, %v; want 1, nil", result.GenerationsPruned, err)
	}
}
