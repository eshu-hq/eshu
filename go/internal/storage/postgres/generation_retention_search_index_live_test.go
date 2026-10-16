// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	indexretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index/retention"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// seedRetentionSearchIndex gives one generation docs documents and
// docs*termsPerDoc terms; document ids repeat across generations, so the
// active generation re-mints a superseded generation's ids on its hash leaf.
func seedRetentionSearchIndex(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string, docs, termsPerDoc int) {
	t.Helper()
	execRetentionSeed(t, ctx, database, `INSERT INTO eshu_search_index_documents (scope_id, generation_id, document_id, fact_id, repo_id,
    source_kind, document, document_length, updated_at)
SELECT $1, $2, 'doc-' || d, 'fact-' || d, $1, 'file', '{"t":"x"}'::jsonb, 10, now()
FROM generate_series(0, $3::int - 1) AS d`, scopeID, generationID, docs)
	execRetentionSeed(t, ctx, database, `INSERT INTO eshu_search_index_terms (scope_id, generation_id, document_id, term_key, term, term_frequency)
SELECT $1, $2, 'doc-' || d, 'tk-' || k, 'term-' || k, 1
FROM generate_series(0, $3::int - 1) AS d, generate_series(0, $4::int - 1) AS k`, scopeID, generationID, docs, termsPerDoc)
}

// countRetentionSearchIndex returns one generation's terms and documents rows.
func countRetentionSearchIndex(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string) (terms, documents int64) {
	t.Helper()
	if err := database.QueryRowContext(ctx, `SELECT
    (SELECT count(*) FROM eshu_search_index_terms WHERE scope_id = $1 AND generation_id = $2),
    (SELECT count(*) FROM eshu_search_index_documents WHERE scope_id = $1 AND generation_id = $2)`,
		scopeID, generationID).Scan(&terms, &documents); err != nil {
		t.Fatalf("count search index rows: %v", err)
	}
	return terms, documents
}

// retentionEventSearchIndexRows sums one table's rows over every retention
// event in the schema.
func retentionEventSearchIndexRows(t *testing.T, ctx context.Context, database *sql.DB, table string) int64 {
	t.Helper()
	var rows int64
	if err := database.QueryRowContext(ctx, `SELECT COALESCE(sum((row_counts->>$1)::bigint), 0) FROM generation_retention_events`, table).Scan(&rows); err != nil {
		t.Fatalf("read event row_counts: %v", err)
	}
	return rows
}

// TestGenerationRetentionPredeletesSearchIndexBeforeTheScopeLockLive is the
// #7427 PR1 behavior proof. A prunable superseded generation owns 45,000
// terms and 4,500 documents. Before #7427 they were deleted only by the
// scope_generations FK cascade inside the prune transaction, under the
// re-locked scope row, so at the moment the pass took its targeted re-lock
// every one of them was still there (RED on main: 45000 at the hook). Now
// the committed pre-delete has removed them before the prune transaction
// opens, so the cascade under the lock finds nothing (GREEN: 0 at the hook),
// and the pass still reports them in RowsPruned. The active generation's
// re-minted rows survive.
func TestGenerationRetentionPredeletesSearchIndexBeforeTheScopeLockLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	scopeID := "scope-7427-" + now.Format("150405.000000000")
	activeID, oldID := scopeID+"-active", scopeID+"-old"
	testfixtures.SeedScope(t, ctx, database, scopeID)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, oldID, now.Add(-10*24*time.Hour))
	seedRetentionSearchIndex(t, ctx, database, scopeID, oldID, 4500, 10)
	seedRetentionSearchIndex(t, ctx, database, scopeID, activeID, 4500, 2)

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	termsAtRelock := int64(-1)
	store.beforeTargetedLock = func() {
		termsAtRelock, _ = countRetentionSearchIndex(t, ctx, database, scopeID, oldID)
	}
	result, err := store.PruneSupersededGenerations(ctx, retentionSelectionPolicy(0, 7*24*time.Hour, 10))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if termsAtRelock != 0 {
		t.Fatalf("terms rows of the pruned generation at the targeted re-lock = %d, want 0: the scope lock must not cover the search index delete", termsAtRelock)
	}
	if result.GenerationsPruned != 1 {
		t.Fatalf("GenerationsPruned = %d, want 1", result.GenerationsPruned)
	}
	if got := result.RowsPruned["eshu_search_index_terms"]; got != 45000 {
		t.Fatalf("RowsPruned[eshu_search_index_terms] = %d, want 45000", got)
	}
	if got := result.RowsPruned["eshu_search_index_documents"]; got != 4500 {
		t.Fatalf("RowsPruned[eshu_search_index_documents] = %d, want 4500", got)
	}
	if _, ok := result.PhaseDurations["predelete_search_index"]; !ok {
		t.Fatalf("PhaseDurations = %v, want predelete_search_index", result.PhaseDurations)
	}
	// The pruned generation's retention event must carry the rows the
	// counter adds for the search index tables (metrics-reducer-storage.md:
	// a batch's event counts sum to what the counter adds per table).
	for _, table := range []string{"eshu_search_index_terms", "eshu_search_index_documents"} {
		if eventRows := retentionEventSearchIndexRows(t, ctx, database, table); eventRows != result.RowsPruned[table] {
			t.Fatalf("event row_counts[%s] = %d, RowsPruned[%s] = %d: the event must record the pre-deleted rows", table, eventRows, table, result.RowsPruned[table])
		}
	}
	if terms, documents := countRetentionSearchIndex(t, ctx, database, scopeID, activeID); terms != 9000 || documents != 4500 {
		t.Fatalf("active generation has %d terms, %d documents, want 9000 and 4500", terms, documents)
	}
}

// TestGenerationRetentionAdmitsOnlyDrainedGenerationsLive: a row cap that
// stops the pre-delete after its first batch leaves the generation undrained,
// so the pass skips it as search_index_undrained and prunes nothing; the next
// pass at the default cap drains and prunes it. No progress table is needed.
func TestGenerationRetentionAdmitsOnlyDrainedGenerationsLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	scopeID := "scope-7427u-" + now.Format("150405.000000000")
	oldID := scopeID + "-old"
	testfixtures.SeedScope(t, ctx, database, scopeID)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, oldID, now.Add(-10*24*time.Hour))
	seedRetentionSearchIndex(t, ctx, database, scopeID, oldID, 3000, 10)

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	policy := retentionSelectionPolicy(0, 7*24*time.Hour, 10)
	policy.SearchIndexPredeleteRowCap = 1
	first, err := store.PruneSupersededGenerations(ctx, policy)
	if err != nil {
		t.Fatalf("first pass error = %v", err)
	}
	if first.GenerationsPruned != 0 || first.Skipped[searchIndexUndrainedSkippedReason] != 1 {
		t.Fatalf("first pass pruned %d, skipped %v, want 0 pruned and one search_index_undrained", first.GenerationsPruned, first.Skipped)
	}
	if first.SearchIndexBatches != 1 || first.RowsPruned["eshu_search_index_terms"] != indexretention.BatchRows {
		t.Fatalf("first pass batches = %d, terms = %d, want one batch of %d", first.SearchIndexBatches, first.RowsPruned["eshu_search_index_terms"], indexretention.BatchRows)
	}
	if remaining := remainingScopeGenerations(t, ctx, database, []string{oldID}); !remaining[oldID] {
		t.Fatal("undrained generation was pruned")
	}
	if events := retentionEventSearchIndexRows(t, ctx, database, "eshu_search_index_terms"); events != 0 {
		t.Fatalf("undrained pass wrote events with %d terms rows, want none: its pre-deleted rows are counter-only", events)
	}

	policy.SearchIndexPredeleteRowCap = 0
	second, err := store.PruneSupersededGenerations(ctx, policy)
	if err != nil {
		t.Fatalf("second pass error = %v", err)
	}
	if second.GenerationsPruned != 1 || second.Skipped[searchIndexUndrainedSkippedReason] != 0 {
		t.Fatalf("second pass pruned %d, skipped %v, want the drained generation pruned", second.GenerationsPruned, second.Skipped)
	}
	if terms, documents := countRetentionSearchIndex(t, ctx, database, scopeID, oldID); terms != 0 || documents != 0 {
		t.Fatalf("%d terms, %d documents remain, want none", terms, documents)
	}
	if events := retentionEventSearchIndexRows(t, ctx, database, "eshu_search_index_terms"); events != second.RowsPruned["eshu_search_index_terms"] {
		t.Fatalf("event terms rows = %d, second pass RowsPruned = %d, want equal for the pruned generation", events, second.RowsPruned["eshu_search_index_terms"])
	}
	if _, ok := second.PhaseDurations[GenerationRetentionPhaseSweepSearchIndex]; !ok {
		t.Fatalf("PhaseDurations = %v, want %s", second.PhaseDurations, GenerationRetentionPhaseSweepSearchIndex)
	}
}

// TestGenerationRetentionAdmitSkipsAGenerationWhoseLockIsHeldLive: the prune
// transaction takes each drained candidate's advisory lock before its
// re-lock; a candidate another session holds is skipped as
// advisory_lock_busy, never waited on, so a pre-delete batch and the FK
// cascade never delete one generation's rows at once (#7427 D4).
func TestGenerationRetentionAdmitSkipsAGenerationWhoseLockIsHeldLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	scopeID := "scope-7427b-" + now.Format("150405.000000000")
	freeID, heldID := scopeID+"-free", scopeID+"-held"
	testfixtures.SeedScope(t, ctx, database, scopeID)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, freeID, now.Add(-11*24*time.Hour))
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, heldID, now.Add(-10*24*time.Hour))

	pass := indexretention.NewPass(1<<40, time.Time{}, nil)
	if err := pass.Run(ctx, SQLDB{DB: database}, []indexretention.Generation{
		{ScopeID: scopeID, GenerationID: freeID}, {ScopeID: scopeID, GenerationID: heldID},
	}); err != nil {
		t.Fatalf("pre-delete pass error = %v", err)
	}
	holder, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if locked, err := indexretention.TryLockGenerations(ctx, holder, []string{heldID}); err != nil || !locked[heldID] {
		t.Fatalf("holder lock = %v, %v", locked, err)
	}

	tx, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin prune transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	result := newGenerationRetentionResult()
	searchIndex := &generationRetentionSearchIndex{pass: pass}
	candidates := []generationRetentionCandidate{
		{scopeID: scopeID, generationID: freeID}, {scopeID: scopeID, generationID: heldID},
	}
	admitted, chunkTarget, err := searchIndex.admit(ctx, tx, candidates, nil, &result)
	if err != nil {
		t.Fatalf("admit error = %v", err)
	}
	if len(admitted) != 1 || admitted[0].generationID != freeID || chunkTarget != nil {
		t.Fatalf("admitted = %+v, want only %s", admitted, freeID)
	}
	if result.Skipped[searchIndexLockBusySkippedReason] != 1 {
		t.Fatalf("Skipped = %v, want one advisory_lock_busy", result.Skipped)
	}
}

// TestGenerationRetentionCandidateReadQueryPreparesLive proves the marked
// candidate read and its work_mem setting run against the migrated schema.
func TestGenerationRetentionCandidateReadQueryPreparesLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	prepared, err := database.PrepareContext(ctx, generationRetentionSearchIndexReadQuery)
	if err != nil {
		t.Fatalf("prepare search index candidate read: %v", err)
	}
	_ = prepared.Close()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, generationRetentionSearchIndexReadWorkMemStatement); err != nil {
		t.Fatalf("search index candidate read work_mem statement: %v", err)
	}
}
