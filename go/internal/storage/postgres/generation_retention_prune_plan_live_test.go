// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// generationRetentionPruneCase is one content key shape the retention prunes
// must classify. candidate facts always sit in a generation being pruned.
type generationRetentionPruneCase struct {
	name string
	// retained lists the facts outside the pruned generations for the same key
	// (or a different repository or scope); each is "live" or "tombstone" and
	// names the generation that holds it.
	retained []generationRetentionRetainedFact
	// deleted is whether the content row for the key must be pruned.
	deleted bool
}

type generationRetentionRetainedFact struct {
	generation string
	repo       string
	tombstone  bool
}

// generationRetentionPruneCases cover every protection rule of the prunes: a
// live retained fact in the active generation or another scope protects a key;
// a tombstone, a different repository, or no retained fact does not.
var generationRetentionPruneCases = []generationRetentionPruneCase{
	{name: "candidate-only", deleted: true},
	{name: "kept-by-active", retained: []generationRetentionRetainedFact{{generation: "gen-active", repo: "repo-1"}}},
	{name: "tombstone-does-not-protect", retained: []generationRetentionRetainedFact{{generation: "gen-active", repo: "repo-1", tombstone: true}}, deleted: true},
	{name: "kept-by-other-scope", retained: []generationRetentionRetainedFact{{generation: "gen-other-scope", repo: "repo-1"}}},
	{name: "other-repo-does-not-protect", retained: []generationRetentionRetainedFact{{generation: "gen-active", repo: "repo-2"}}, deleted: true},
	{name: "tombstone-then-live-elsewhere-protects", retained: []generationRetentionRetainedFact{
		{generation: "gen-active", repo: "repo-1", tombstone: true},
		{generation: "gen-other-scope", repo: "repo-1"},
	}},
}

// generationRetentionContentPrunes are the three content prunes, in the order
// PruneSupersededGenerations runs them.
var generationRetentionContentPrunes = []struct{ name, statement string }{
	{"refs", pruneContentFileReferencesForGenerationsQuery},
	{"entities", pruneContentEntitiesForGenerationsQuery},
	{"files", pruneContentFilesForGenerationsQuery},
}

// TestGenerationRetentionContentPrunesDeleteExactRowsLive proves the three
// content prunes delete exactly the rows whose facts live only in the pruned
// generations, on the real migrated schema. The expected set is written out per
// case, not derived from the statements under test.
func TestGenerationRetentionContentPrunesDeleteExactRowsLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	seedGenerationRetentionPruneScopes(t, ctx, database)

	var wantEntities, wantFiles, wantRefs []string
	for _, c := range generationRetentionPruneCases {
		seedGenerationRetentionPruneCase(t, ctx, database, c)
		key := "repo-1|" + c.name
		if !c.deleted {
			wantEntities = append(wantEntities, key)
			wantFiles = append(wantFiles, key)
			wantRefs = append(wantRefs, key+"|r1", key+"|r2")
		}
	}
	// Rows no candidate fact names stay, as do rows behind an empty key.
	for _, row := range []struct{ repo, key string }{{"repo-1", "retained-only"}, {"repo-1", "no-fact"}} {
		seedGenerationRetentionPruneRows(t, ctx, database, row.repo, row.key)
		wantEntities = append(wantEntities, row.repo+"|"+row.key)
		wantFiles = append(wantFiles, row.repo+"|"+row.key)
		wantRefs = append(wantRefs, row.repo+"|"+row.key+"|r1", row.repo+"|"+row.key+"|r2")
	}
	seedGenerationRetentionPruneRows(t, ctx, database, "repo-1", "")
	wantEntities = append(wantEntities, "repo-1|")
	wantFiles = append(wantFiles, "repo-1|")
	wantRefs = append(wantRefs, "repo-1||r1", "repo-1||r2")
	for _, kind := range []string{"content_entity", "file"} {
		execRetentionSeed(t, ctx, database, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
VALUES ('empty-'||$1, 'scope-a', 'gen-cand-1', $1, 'empty-'||$1, 'git', 'empty-'||$1, now(), now(),
    '{"repo_id":"repo-1","entity_id":"","relative_path":""}'::jsonb)`, kind)
	}
	// A retained-only key: its only fact is in the active generation.
	seedGenerationRetentionPruneFact(t, ctx, database, "gen-active", "retained-only", "repo-1", false)

	candidates := []string{"gen-cand-1", "gen-cand-2"}
	for _, prune := range generationRetentionContentPrunes {
		name, statement := prune.name, prune.statement
		if _, err := database.ExecContext(ctx, statement, candidates); err != nil {
			t.Fatalf("prune %s: %v", name, err)
		}
	}

	assertGenerationRetentionKeys(t, ctx, database, "content_entities",
		`SELECT repo_id||'|'||entity_id FROM content_entities`, wantEntities)
	assertGenerationRetentionKeys(t, ctx, database, "content_files",
		`SELECT repo_id||'|'||relative_path FROM content_files`, wantFiles)
	assertGenerationRetentionKeys(t, ctx, database, "content_file_references",
		`SELECT repo_id||'|'||relative_path||'|'||reference_value FROM content_file_references`, wantRefs)
}

// TestGenerationRetentionContentPrunesFinishWithoutPlannerStatisticsLive fails
// when a prune's plan depends on planner statistics. A freshly bulk-loaded
// database has none, and the previous NOT EXISTS shape then estimated one
// retained row, chose a nested loop, and rescanned the retained facts once per
// candidate key: minutes of work for a few thousand keys (#6809).
//
// The per-statement phase gets its RED power from the entities statement: on
// the old SQL only the entities prune reliably outlives the 10s timeout (the
// references prune took 9.0s and the files prune under 20s on a loaded laptop,
// so they do not fail this test on their own). The second phase runs the
// production row-count statement and a whole retention batch on the same cold
// seed; the row-count statement did not stall cold when it was measured, so that
// phase is a guard against a later regression, not a demonstrated RED.
func TestGenerationRetentionContentPrunesFinishWithoutPlannerStatisticsLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	for _, table := range []string{"fact_records", "content_entities", "content_files", "content_file_references"} {
		execRetentionSeed(t, ctx, database, "ALTER TABLE "+table+" SET (autovacuum_enabled = false)")
	}
	const keys = 6000
	seedGenerationRetentionColdShape(t, ctx, database, keys)

	candidates := make([]string, 0, 10)
	for g := 1; g <= 10; g++ {
		candidates = append(candidates, fmt.Sprintf("gen-cold-%d", g))
	}
	for _, prune := range generationRetentionContentPrunes {
		name, statement := prune.name, prune.statement
		start := time.Now()
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin %s: %v", name, err)
		}
		if _, err := tx.ExecContext(ctx, "SET LOCAL statement_timeout = '10s'"); err != nil {
			_ = tx.Rollback()
			t.Fatalf("set statement_timeout: %v", err)
		}
		result, err := tx.ExecContext(ctx, statement, candidates)
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("cold prune %s did not finish inside 10s without planner statistics: %v", name, err)
		}
		deleted, _ := result.RowsAffected()
		_ = tx.Rollback()
		// Keys with key%10 >= 6 exist only in pruned generations: 40% of keys.
		wantDeleted := int64(keys * 4 / 10)
		if name == "refs" {
			wantDeleted *= 3
		}
		if deleted != wantDeleted {
			t.Errorf("cold prune %s deleted %d rows, want %d", name, deleted, wantDeleted)
		}
		t.Logf("cold prune %s: %d rows in %s", name, deleted, time.Since(start))
	}
	assertGenerationRetentionColdBatch(t, ctx, database, keys)
}

// assertGenerationRetentionColdBatch runs the production row-count statement
// and then a whole PruneSupersededGenerations batch on the cold seed, each under
// a 20s deadline, and checks the per-generation counts and the rows the batch deletes.
func assertGenerationRetentionColdBatch(t *testing.T, ctx context.Context, database *sql.DB, keys int) {
	t.Helper()
	batchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	candidates := make([]string, 0, 10)
	for g := 1; g <= 10; g++ {
		candidates = append(candidates, fmt.Sprintf("gen-cold-%d", g))
	}
	store := NewGenerationRetentionStore(SQLDB{DB: database})
	tx, err := SQLDB{DB: database}.Begin(batchCtx)
	if err != nil {
		t.Fatalf("begin row count: %v", err)
	}
	start := time.Now()
	counted, perGeneration, _, err := store.countRows(batchCtx, tx, candidates)
	_ = tx.Rollback()
	if err != nil {
		t.Fatalf("cold row counts did not finish inside 20s without planner statistics: %v", err)
	}
	t.Logf("cold row counts: %d tables in %s", len(counted), time.Since(start))
	// Every pruned generation holds all keys, and keys with key%10 >= 6 exist only
	// in the pruned generations. The count attributes a content row to each
	// generation whose facts name it, so each generation reports the rows the
	// prunes will delete and the batch total is the per-generation sum.
	doomed := int64(keys * 4 / 10)
	wantPerGeneration := map[string]int64{
		"fact_records":            int64(keys * 2),
		"content_entities":        doomed,
		"content_files":           doomed,
		"content_file_references": doomed * 3,
	}
	for _, generation := range candidates {
		for table, wantCount := range wantPerGeneration {
			if got := perGeneration[generation][table]; got != wantCount {
				t.Errorf("cold row count for %s in %s = %d, want %d", table, generation, got, wantCount)
			}
		}
	}
	start = time.Now()
	result, err := store.PruneSupersededGenerations(batchCtx, GenerationRetentionPolicy{
		MinSupersededGenerations: 0,
		MaxSupersededAge:         time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            10_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "6809-cold-batch",
	})
	if err != nil {
		t.Fatalf("cold retention batch did not finish inside 20s without planner statistics: %v", err)
	}
	t.Logf("cold retention batch: %d generations in %s", result.GenerationsPruned, time.Since(start))
	if result.GenerationsPruned != 10 {
		t.Errorf("GenerationsPruned = %d, want 10", result.GenerationsPruned)
	}
	wantPruned := map[string]int64{"content_entities": doomed, "content_files": doomed, "content_file_references": doomed * 3}
	for table, wantCount := range wantPruned {
		if result.RowsPruned[table] != wantCount {
			t.Errorf("batch pruned %d %s rows, want %d", result.RowsPruned[table], table, wantCount)
		}
		// The batch total the row limit sees never undercounts what is deleted.
		if counted[table] < result.RowsPruned[table] {
			t.Errorf("row count for %s = %d is below the %d rows pruned", table, counted[table], result.RowsPruned[table])
		}
	}
}

func execRetentionSeed(t *testing.T, ctx context.Context, database *sql.DB, statement string, args ...any) {
	t.Helper()
	if _, err := database.ExecContext(ctx, statement, args...); err != nil {
		t.Fatalf("exec %q: %v", strings.Fields(statement)[0], err)
	}
}

// seedGenerationRetentionPruneScopes creates scope-a with an active generation
// and two candidate generations, and scope-b with one retained generation.
func seedGenerationRetentionPruneScopes(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	for _, scope := range []string{"scope-a", "scope-b"} {
		execRetentionSeed(t, ctx, database, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, now(), now(), 'active', '{}'::jsonb)`, scope)
	}
	for _, gen := range []struct{ id, scope, status string }{
		{"gen-active", "scope-a", "active"},
		{"gen-cand-1", "scope-a", "superseded"},
		{"gen-cand-2", "scope-a", "superseded"},
		{"gen-other-scope", "scope-b", "active"},
	} {
		execRetentionSeed(t, ctx, database, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', now(), now(), $3)`, gen.id, gen.scope, gen.status)
	}
}

// seedGenerationRetentionPruneCase inserts the candidate fact for a case in
// gen-cand-1 (and gen-cand-2, so a key repeats across pruned generations), its
// retained facts, and its content rows.
func seedGenerationRetentionPruneCase(t *testing.T, ctx context.Context, database *sql.DB, c generationRetentionPruneCase) {
	t.Helper()
	for _, gen := range []string{"gen-cand-1", "gen-cand-2"} {
		seedGenerationRetentionPruneFact(t, ctx, database, gen, c.name, "repo-1", false)
	}
	for _, retained := range c.retained {
		seedGenerationRetentionPruneFact(t, ctx, database, retained.generation, c.name, retained.repo, retained.tombstone)
	}
	seedGenerationRetentionPruneRows(t, ctx, database, "repo-1", c.name)
}

// seedGenerationRetentionPruneFact writes one entity fact and one file fact for
// key in generation. The scope is looked up from the generation.
func seedGenerationRetentionPruneFact(t *testing.T, ctx context.Context, database *sql.DB, generation, key, repo string, tombstone bool) {
	t.Helper()
	for _, kind := range []string{"content_entity", "file"} {
		execRetentionSeed(t, ctx, database, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT $1 || '/' || $2 || '/' || $3 || '/' || $4, g.scope_id, g.generation_id, $2, $1 || '/' || $3, 'git',
    $1 || '/' || $3, now(), now(), $5,
    jsonb_build_object('repo_id', $4::text, 'entity_id', $3::text, 'relative_path', $3::text)
FROM scope_generations AS g WHERE g.generation_id = $1`, generation, kind, key, repo, tombstone)
	}
}

// seedGenerationRetentionPruneRows inserts the content_entities, content_files
// and two content_file_references rows for (repo, key). Keys are unique across
// the test because content_entities.entity_id is globally unique.
func seedGenerationRetentionPruneRows(t *testing.T, ctx context.Context, database *sql.DB, repo, key string) {
	t.Helper()
	execRetentionSeed(t, ctx, database, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, indexed_at)
VALUES ($1, $2, $1, 'Function', 'n', 1, 2, 'x', now())`, key, repo)
	execRetentionSeed(t, ctx, database, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
VALUES ($1, $2, 'x', 'h', 1, now())`, repo, key)
	for _, ref := range []string{"r1", "r2"} {
		execRetentionSeed(t, ctx, database, `
INSERT INTO content_file_references (repo_id, relative_path, reference_kind, reference_value, indexed_at)
VALUES ($1, $2, 'import', $3, now())`, repo, key, ref)
	}
}

// seedGenerationRetentionColdShape loads keys distinct keys across 10 pruned
// generations plus one active generation that still holds keys with key%10 < 6,
// without running ANALYZE, so the planner has no statistics.
func seedGenerationRetentionColdShape(t *testing.T, ctx context.Context, database *sql.DB, keys int) {
	t.Helper()
	execRetentionSeed(t, ctx, database, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ('scope-cold', 'repository', 'git', 'cold', 'git', 'cold', now(), now(), 'active', '{}'::jsonb)`)
	execRetentionSeed(t, ctx, database, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-cold-active', 'scope-cold', 'snapshot', now(), now(), 'active')`)
	execRetentionSeed(t, ctx, database, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
SELECT 'gen-cold-' || g, 'scope-cold', 'snapshot', now(), now(), 'superseded', now() - interval '30 days'
FROM generate_series(1, 10) g`)
	for _, kind := range []string{"content_entity", "file"} {
		execRetentionSeed(t, ctx, database, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
SELECT 'cold/' || $1 || '/' || g || '/' || k, 'scope-cold', 'gen-cold-' || g, $1,
    'cold/' || $1 || '/' || g || '/' || k, 'git', 'cold/' || $1 || '/' || g || '/' || k, now(), now(),
    jsonb_build_object('repo_id', 'repo-cold', 'entity_id', 'e-' || k, 'relative_path', 'p-' || k)
FROM generate_series(1, 10) g, generate_series(1, $2::int) k`, kind, keys)
		execRetentionSeed(t, ctx, database, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
SELECT 'cold/' || $1 || '/active/' || k, 'scope-cold', 'gen-cold-active', $1,
    'cold/' || $1 || '/active/' || k, 'git', 'cold/' || $1 || '/active/' || k, now(), now(),
    jsonb_build_object('repo_id', 'repo-cold', 'entity_id', 'e-' || k, 'relative_path', 'p-' || k)
FROM generate_series(1, $2::int) k WHERE k % 10 < 6`, kind, keys)
	}
	execRetentionSeed(t, ctx, database, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, indexed_at)
SELECT 'e-' || k, 'repo-cold', 'p-' || k, 'Function', 'n', 1, 2, 'x', now() FROM generate_series(1, $1::int) k`, keys)
	execRetentionSeed(t, ctx, database, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
SELECT 'repo-cold', 'p-' || k, 'x', 'h', 1, now() FROM generate_series(1, $1::int) k`, keys)
	execRetentionSeed(t, ctx, database, `
INSERT INTO content_file_references (repo_id, relative_path, reference_kind, reference_value, indexed_at)
SELECT 'repo-cold', 'p-' || k, 'import', 'v' || r, now() FROM generate_series(1, $1::int) k, generate_series(1, 3) r`, keys)
}

// assertGenerationRetentionKeys compares the rows a query returns with want,
// ignoring order.
func assertGenerationRetentionKeys(t *testing.T, ctx context.Context, database *sql.DB, table, query string, want []string) {
	t.Helper()
	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatalf("scan %s: %v", table, err)
		}
		got = append(got, key)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	slices.Sort(got)
	want = slices.Clone(want)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("%s rows after prune:\n got  %v\n want %v", table, got, want)
	}
}
