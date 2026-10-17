// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7981 disposable PostgreSQL 18 proof that an over-limit generation drains one bounded fact chunk per cycle and prunes whole once the remainder fits with no orphan rows, that the chunk statements roll back atomically, and that a lock-held chunk target drops silently (the concurrent-insert proof moved to generation_retention_chunk_insert_wait_live_test.go in #8206), enrolled in the live-postgres-readiness runner; bridges the family ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN as the hard-ceiling proof does. local run passed all 4 tests (3 remain here after #8206) in 9.38s wall with the migrated-schema setup on postgres:16 on 2026-10-14
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// The #7981 over-limit chunk proofs drain one 2500-fact generation three
// cycles: two bounded 1000-fact chunks, then a whole-generation prune once
// the remainder fits BatchRowLimit. Fact ids are zero-padded so the keyset
// page drains oldest-first deterministically; content keys shared across the
// whole generation survive early chunks and die only when their last fact
// goes, while chunk-1-exclusive keys die with the first chunk. Tombstone
// facts drain as facts but never contribute content keys (the batch prune's
// key shape), so the fixture seeds no content rows for tombstone-only keys:
// like the batch path, the chunk path would orphan those, a pre-existing gap
// outside this change.

const (
	chunkDrainScope = "chunk-drain"
	chunkDrainGen   = "chunk-drain-huge"
	chunkNeighbor   = "chunk-neighbor"
	chunkRepo       = "repo-chunk"
)

func chunkDrainPolicy() GenerationRetentionPolicy {
	return GenerationRetentionPolicy{
		MinSupersededGenerations: 0,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     100,
		BatchRowLimit:            1000,
		PolicyScope:              "global",
		PolicyRevision:           "7981-chunk-live",
	}
}

// seedChunkDrainFixture seeds the over-limit generation (2500 facts: file
// facts on shared and chunk-1-exclusive keys, content_entity facts, and
// tombstones on shared keys), the content and mirror rows for live keys, and
// a neighbor scope whose active generation shares one file key that must
// survive the full drain.
func seedChunkDrainFixture(t *testing.T, ctx context.Context, database *sql.DB, now time.Time) {
	t.Helper()
	testfixtures.SeedScope(t, ctx, database, chunkDrainScope)
	testfixtures.SeedSupersededGeneration(t, ctx, database, chunkDrainScope, chunkDrainGen, now.Add(-10*24*time.Hour))
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload, is_tombstone)
SELECT 'chunk-' || lpad(n::text, 5, '0'), $1, $2,
    CASE WHEN n % 25 = 0 THEN 'content_entity' ELSE 'file' END,
    'ck-' || n, 'git', 'ck-' || n, now(), now(),
    CASE WHEN n % 25 = 0
         THEN jsonb_build_object('repo_id', $3::text, 'entity_id', 'ent-' || ((n / 25) % 20))
         ELSE jsonb_build_object('repo_id', $3::text, 'relative_path',
              CASE WHEN n <= 100 AND n % 25 != 0 THEN 'excl-' || n ELSE 'shared-' || (n % 10) END)
    END,
    (n % 7 = 0 AND n % 25 != 0)
FROM generate_series(1, 2500) AS n`, chunkDrainScope, chunkDrainGen, chunkRepo); err != nil {
		t.Fatalf("seed chunk facts: %v", err)
	}
	// Content rows for live keys only: shared file keys, chunk-1-exclusive
	// file keys with a live (non-tombstone) fact, and every entity key.
	if _, err := database.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
SELECT $1, 'shared-' || n, 'c', 'h', 1, now() FROM generate_series(0, 9) AS n
UNION ALL
SELECT $1, 'excl-' || n, 'c', 'h', 1, now() FROM generate_series(1, 100) AS n
WHERE n % 25 != 0 AND n % 7 != 0`, chunkRepo); err != nil {
		t.Fatalf("seed chunk content files: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, indexed_at)
SELECT 'ent-' || n, $1, 'e.tf', 'aws_resource', 'ent-' || n, 1, 2, 's', now()
FROM generate_series(0, 19) AS n`, chunkRepo); err != nil {
		t.Fatalf("seed chunk content entities: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO infra_resource_entities (entity_id, repo_id, relative_path, label, entity_name, updated_at)
SELECT 'ent-' || n, $1, 'e.tf', 'l', 'ent-' || n, now() FROM generate_series(0, 19) AS n`, chunkRepo); err != nil {
		t.Fatalf("seed chunk mirror rows: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO content_file_references (repo_id, relative_path, reference_kind, reference_value, indexed_at)
VALUES ($1, 'excl-1', 'kindA', 'val1', now()), ($1, 'excl-1', 'kindA', 'val2', now()),
       ($1, 'shared-0', 'kindA', 'val9', now())`, chunkRepo); err != nil {
		t.Fatalf("seed chunk references: %v", err)
	}
	// The neighbor's active generation shares shared-0: that key, its
	// content row and its reference must survive the full drain.
	testfixtures.SeedScope(t, ctx, database, chunkNeighbor)
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
VALUES ('neighbor-1', $1, $1 || '-active', 'file', 'nkey', 'git', 'nkey', now(), now(),
    jsonb_build_object('repo_id', $2::text, 'relative_path', 'shared-0'))`, chunkNeighbor, chunkRepo); err != nil {
		t.Fatalf("seed neighbor fact: %v", err)
	}
}

// countContentRows reports the content, reference and mirror rows for one
// repo, so the drain can assert exact per-cycle progress and a final
// orphan-free state.
func countContentRows(t *testing.T, ctx context.Context, database *sql.DB, repo string) map[string]int64 {
	t.Helper()
	counts := make(map[string]int64, 4)
	for table, query := range map[string]string{
		"content_files":           `SELECT count(*) FROM content_files WHERE repo_id = $1`,
		"content_file_references": `SELECT count(*) FROM content_file_references WHERE repo_id = $1`,
		"content_entities":        `SELECT count(*) FROM content_entities WHERE repo_id = $1`,
		"infra_resource_entities": `SELECT count(*) FROM infra_resource_entities WHERE repo_id = $1`,
	} {
		var count int64
		if err := database.QueryRowContext(ctx, query, repo).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		counts[table] = count
	}
	return counts
}

// countLedgerRowsForScope reports the changed-since ledger rows for one
// scope across the four ledger tables.
func countLedgerRowsForScope(t *testing.T, ctx context.Context, database *sql.DB, scopeID string) map[string]int64 {
	t.Helper()
	counts := make(map[string]int64, 4)
	for _, table := range []string{
		linksfreshnessstore.TableLinks, linksfreshnessstore.TableLinkDeltas,
		linksfreshnessstore.TableLinkBucketCounts, linksfreshnessstore.TableActivations,
	} {
		var count int64
		if err := database.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT count(*) FROM %s WHERE scope_id = $1`, table), scopeID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		counts[table] = count
	}
	return counts
}

// TestGenerationRetentionChunkedDrainLive drains the 2500-fact generation in
// three cycles and proves the crash-resume shape: each chunk cycle deletes
// exactly one bounded page, records progress, keeps the generation row, and
// holds the scope lock for a measured (bounded) hold; the final cycle prunes
// the remainder whole with no orphan rows in facts, content, references,
// entities, mirror or ledger, while the neighbor's shared key survives.
func TestGenerationRetentionChunkedDrainLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	seedChunkDrainFixture(t, ctx, database, now)
	policy := chunkDrainPolicy()

	prune := func() GenerationRetentionResult {
		t.Helper()
		store := NewGenerationRetentionStore(SQLDB{DB: database})
		store.Now = func() time.Time { return now }
		result, err := store.PruneSupersededGenerations(ctx, policy)
		if err != nil {
			t.Fatalf("PruneSupersededGenerations() error = %v", err)
		}
		return result
	}

	// Cycle 1: the oldest 1000 facts go, with the 82 live exclusive keys'
	// file rows and excl-1's two references; shared and entity keys stay.
	first := prune()
	if first.GenerationsPruned != 0 {
		t.Fatalf("cycle 1 GenerationsPruned = %d, want 0 (chunk only)", first.GenerationsPruned)
	}
	if got := first.Skipped[generationRetentionChunkedProgressSkippedReason]; got != 1 {
		t.Fatalf("cycle 1 Skipped[row_limit_chunked] = %d, want 1", got)
	}
	if got := first.RowsPruned["fact_records"]; got != 1000 {
		t.Fatalf("cycle 1 RowsPruned[fact_records] = %d, want 1000 (one bounded page)", got)
	}
	if got := first.RowsPruned["content_files"]; got != 82 {
		t.Fatalf("cycle 1 RowsPruned[content_files] = %d, want 82 (the live exclusive keys)", got)
	}
	if got := first.RowsPruned["content_file_references"]; got != 2 {
		t.Fatalf("cycle 1 RowsPruned[content_file_references] = %d, want 2 (excl-1's references)", got)
	}
	if got := first.RowsPruned["content_entities"]; got != 0 {
		t.Fatalf("cycle 1 RowsPruned[content_entities] = %d, want 0 (every entity key keeps a live fact)", got)
	}
	if got := first.RowsPruned["infra_resource_entities"]; got != 0 {
		t.Fatalf("cycle 1 RowsPruned[infra_resource_entities] = %d, want 0 (no entity died)", got)
	}
	if first.ScopeLockHold <= 0 {
		t.Fatal("cycle 1 ScopeLockHold <= 0, want the chunk cycle's measured narrow hold")
	}
	t.Logf("cycle 1 ScopeLockHold = %v for a 1000-fact chunk", first.ScopeLockHold)
	// Timestamptz stores microseconds, so the round-tripped age carries a
	// sub-microsecond skew over the exact 10 days.
	if got := first.OldestEligibleAge; got < 10*24*time.Hour || got > 10*24*time.Hour+time.Second {
		t.Fatalf("cycle 1 OldestEligibleAge = %v, want the 10-day target age", got)
	}
	if facts := countScopeGenerationFacts(t, ctx, database, chunkDrainScope, chunkDrainGen); facts != 1500 {
		t.Fatalf("cycle 1 facts = %d, want 1500", facts)
	}
	remaining := remainingScopeGenerations(t, ctx, database, []string{chunkDrainGen})
	if !remaining[chunkDrainGen] {
		t.Fatal("generation row gone after cycle 1, want retained until the final prune")
	}

	// Cycle 2: the next 1000 facts go; no exclusive keys remain, so only
	// facts move while shared and entity keys stay.
	second := prune()
	if second.GenerationsPruned != 0 {
		t.Fatalf("cycle 2 GenerationsPruned = %d, want 0 (chunk only)", second.GenerationsPruned)
	}
	if got := second.Skipped[generationRetentionChunkedProgressSkippedReason]; got != 1 {
		t.Fatalf("cycle 2 Skipped[row_limit_chunked] = %d, want 1", got)
	}
	if got := second.RowsPruned["fact_records"]; got != 1000 {
		t.Fatalf("cycle 2 RowsPruned[fact_records] = %d, want 1000", got)
	}
	if got := second.RowsPruned["content_files"]; got != 0 {
		t.Fatalf("cycle 2 RowsPruned[content_files] = %d, want 0 (shared keys keep live facts)", got)
	}
	if facts := countScopeGenerationFacts(t, ctx, database, chunkDrainScope, chunkDrainGen); facts != 500 {
		t.Fatalf("cycle 2 facts = %d, want 500", facts)
	}
	t.Logf("cycle 2 ScopeLockHold = %v for a 1000-fact chunk", second.ScopeLockHold)

	// Cycle 3: 500 remaining facts fit the limit, so the generation prunes
	// whole with its ledger order intact and no orphans.
	third := prune()
	t.Logf("cycle 3 ScopeLockHold = %v for the 500-fact final prune", third.ScopeLockHold)
	if third.GenerationsPruned != 1 {
		t.Fatalf("cycle 3 GenerationsPruned = %d, want 1 (the drained remainder)", third.GenerationsPruned)
	}
	if got := third.Skipped[generationRetentionChunkedProgressSkippedReason]; got != 0 {
		t.Fatalf("cycle 3 Skipped[row_limit_chunked] = %d, want 0 (no chunk drained)", got)
	}
	remaining = remainingScopeGenerations(t, ctx, database, []string{chunkDrainGen})
	if remaining[chunkDrainGen] {
		t.Fatal("generation row survived cycle 3, want pruned with its remainder")
	}
	if facts := countScopeGenerationFacts(t, ctx, database, chunkDrainScope, chunkDrainGen); facts != 0 {
		t.Fatalf("final facts = %d, want 0", facts)
	}
	content := countContentRows(t, ctx, database, chunkRepo)
	if content["content_files"] != 1 {
		t.Fatalf("final content_files = %d, want 1 (only the neighbor's shared-0)", content["content_files"])
	}
	if content["content_file_references"] != 1 {
		t.Fatalf("final content_file_references = %d, want 1 (only shared-0's reference)", content["content_file_references"])
	}
	if content["content_entities"] != 0 {
		t.Fatalf("final content_entities = %d, want 0", content["content_entities"])
	}
	if content["infra_resource_entities"] != 0 {
		t.Fatalf("final infra_resource_entities = %d, want 0 (mirror orphans cleaned)", content["infra_resource_entities"])
	}
	for table, count := range countLedgerRowsForScope(t, ctx, database, chunkDrainScope) {
		if count != 0 {
			t.Fatalf("final ledger %s = %d, want 0 (the final prune keeps ledger-before-generations)", table, count)
		}
	}
	var events int
	if err := database.QueryRowContext(ctx,
		`SELECT count(*) FROM generation_retention_events WHERE generation_id_hash = $1`,
		retentionHashID("generation", chunkDrainGen)).Scan(&events); err != nil {
		t.Fatalf("count retention events: %v", err)
	}
	if events != 1 {
		t.Fatalf("retention events for the drained generation = %d, want 1 (the final whole prune)", events)
	}
}

// TestGenerationRetentionChunkStatementsRollBackLive proves the chunk page,
// fact delete and content prune are one atomic unit on a populated backend:
// run them in a transaction, roll it back, and every fact and content row is
// still there. A crashed chunk cycle therefore resumes with nothing
// half-deleted, and the next cycle re-pages from the same oldest-first
// order.
func TestGenerationRetentionChunkStatementsRollBackLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scopeID, generationID = "chunk-rollback", "chunk-rollback-g0"
	testfixtures.SeedScope(t, ctx, database, scopeID)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, generationID, now.Add(-10*24*time.Hour))
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
SELECT 'rb-' || n, $1, $2, 'file', 'rbk', 'git', 'rbk', now(), now(),
    jsonb_build_object('repo_id', 'repo-rollback', 'relative_path', 'rb.txt')
FROM generate_series(1, 50) AS n`, scopeID, generationID); err != nil {
		t.Fatalf("seed rollback facts: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
VALUES ('repo-rollback', 'rb.txt', 'c', 'h', 1, now())`); err != nil {
		t.Fatalf("seed rollback content: %v", err)
	}

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	// A failed chunk cycle rolls back: the effects below must vanish.
	rolledBack := false
	defer func() {
		if !rolledBack {
			_ = tx.Rollback()
		}
	}()
	rows, err := tx.QueryContext(ctx, generationRetentionChunkPageQuery, scopeID, generationID, 1000)
	if err != nil {
		t.Fatalf("chunk page: %v", err)
	}
	var ids []string
	for rows.Next() {
		var id, kind, repo, path, entity string
		var tombstone bool
		if err := rows.Scan(&id, &kind, &tombstone, &repo, &path, &entity); err != nil {
			_ = rows.Close()
			t.Fatalf("scan chunk page: %v", err)
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("chunk page rows: %v", err)
	}
	if len(ids) != 50 {
		t.Fatalf("chunk page ids = %d, want 50", len(ids))
	}
	if _, err := tx.ExecContext(ctx, generationRetentionChunkDeleteFactsQuery, ids); err != nil {
		t.Fatalf("chunk fact delete: %v", err)
	}
	pruneRows, err := tx.QueryContext(ctx, generationRetentionChunkPruneFilesQuery,
		[]string{"repo-rollback"}, []string{"rb.txt"})
	if err != nil {
		t.Fatalf("chunk file prune: %v", err)
	}
	var refs, files int64
	for pruneRows.Next() {
		if err := pruneRows.Scan(&refs, &files); err != nil {
			_ = pruneRows.Close()
			t.Fatalf("scan chunk prune counts: %v", err)
		}
	}
	_ = pruneRows.Close()
	if err := pruneRows.Err(); err != nil {
		t.Fatalf("chunk prune rows: %v", err)
	}
	if files != 1 {
		t.Fatalf("chunk pruned files = %d, want 1 (rb.txt died with its page)", files)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	rolledBack = true

	if facts := countScopeGenerationFacts(t, ctx, database, scopeID, generationID); facts != 50 {
		t.Fatalf("facts after rollback = %d, want 50 (the chunk delete rolled back)", facts)
	}
	content := countContentRows(t, ctx, database, "repo-rollback")
	if content["content_files"] != 1 {
		t.Fatalf("content files after rollback = %d, want 1 (the chunk prune rolled back)", content["content_files"])
	}
}

// TestGenerationRetentionChunkLockMissDropsSilentlyLive is the chunk path's
// contention proof: when another session holds the over-limit generation's
// row, the targeted re-lock skips it with SKIP LOCKED and the pass prunes
// nothing, counts no skip, and leaves every fact for the lock holder's own
// progress. No deadlock is possible: the pass never waits on a row it did
// not lock.
func TestGenerationRetentionChunkLockMissDropsSilentlyLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scopeID, generationID = "chunk-miss", "chunk-miss-g0"
	testfixtures.SeedScope(t, ctx, database, scopeID)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, generationID, now.Add(-10*24*time.Hour))
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
SELECT 'miss-' || n, $1, $2, 'metric', 'mk' || n, 'git', 'mk' || n, now(), now(), '{}'::jsonb
FROM generate_series(1, 1500) AS n`, scopeID, generationID); err != nil {
		t.Fatalf("seed miss facts: %v", err)
	}

	holder, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(ctx,
		`SELECT generation_id FROM scope_generations WHERE generation_id = $1 FOR UPDATE`, generationID); err != nil {
		t.Fatalf("holder lock: %v", err)
	}

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(ctx, GenerationRetentionPolicy{
		MinSupersededGenerations: 0,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            1000,
		PolicyScope:              "global",
		PolicyRevision:           "7981-chunk-miss-live",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 0 {
		t.Fatalf("GenerationsPruned = %d, want 0 (the chunk target was locked elsewhere)", result.GenerationsPruned)
	}
	for reason, count := range result.Skipped {
		t.Errorf("Skipped[%q] = %d, want no skips (a lock miss drops silently for the holder's progress)", reason, count)
	}
	if facts := countScopeGenerationFacts(t, ctx, database, scopeID, generationID); facts != 1500 {
		t.Fatalf("facts after lock miss = %d, want 1500 (nothing drained)", facts)
	}
}
