// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7427 PR3 disposable PostgreSQL 18 proof on the bootstrapped schema with the terms generation foreign key dropped by migration 191: the retention pass prunes a generation whose terms an earlier pre-delete removed and one whose 10000 terms were all present, leaves no terms row and a zero orphan census; a late writer's terms for a pruned generation (rejected with 23503 before 191) are stored, listed by the census as one terms orphan, invisible to EshuSearchIndexStore.Search, and deleted by the orphan sweep; enrolled in the live-postgres-readiness runner; bridges the family ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN. local run passed both tests in 6.8s of test time on PostgreSQL 18.6 on 2026-10-16
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	indexstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index"
	indexretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index/retention"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// requireTermsGenerationForeignKeyDropped fails unless the migrated schema
// has no foreign key from the terms parent or any leaf to scope_generations
// (#7427 PR3), so the proof below runs without the cascade.
func requireTermsGenerationForeignKeyDropped(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM pg_constraint AS constraint_row
WHERE constraint_row.contype = 'f'
  AND constraint_row.confrelid = to_regclass('scope_generations')
  AND (constraint_row.conrelid = to_regclass('eshu_search_index_terms')
       OR constraint_row.conrelid IN (
           SELECT inhrelid FROM pg_inherits WHERE inhparent = to_regclass('eshu_search_index_terms')))`).Scan(&count); err != nil {
		t.Fatalf("count terms generation foreign keys: %v", err)
	}
	if count != 0 {
		t.Fatalf("terms generation foreign keys on the migrated schema = %d, want 0", count)
	}
}

// requireNoSearchIndexOrphans fails unless the shipped census finds no
// search index pair whose generation row is gone.
func requireNoSearchIndexOrphans(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	census, err := indexretention.TakeCensus(ctx, SQLDB{DB: database})
	if err != nil {
		t.Fatalf("TakeCensus() error = %v", err)
	}
	for _, table := range []string{indexretention.TermsTable, indexretention.DocumentsTable} {
		if census.OrphanGenerations[table] != 0 {
			t.Fatalf("census %s orphan generations = %d, want 0 (orphans %v)", table, census.OrphanGenerations[table], census.Orphans)
		}
	}
}

// TestGenerationRetentionPrunesWithoutTheTermsForeignKeyLive: with the terms
// generation foreign key dropped, deleting a scope_generations row no longer
// removes its terms. Retention still leaves no terms row behind, for a
// generation whose terms an earlier pre-delete already removed and for one
// whose terms are all still there when the pass starts: the pass drains every
// candidate before its prune transaction and admits only drained ones.
func TestGenerationRetentionPrunesWithoutTheTermsForeignKeyLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	requireTermsGenerationForeignKeyDropped(t, ctx, database)
	now := time.Now().UTC()
	scopeID := "scope-7427pr3-" + now.Format("150405.000000000")
	activeID, drainedID, intactID := scopeID+"-active", scopeID+"-drained", scopeID+"-intact"
	testfixtures.SeedScope(t, ctx, database, scopeID)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, drainedID, now.Add(-12*24*time.Hour))
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, intactID, now.Add(-10*24*time.Hour))
	seedRetentionSearchIndex(t, ctx, database, scopeID, activeID, 1000, 2)
	seedRetentionSearchIndex(t, ctx, database, scopeID, drainedID, 2000, 5)
	seedRetentionSearchIndex(t, ctx, database, scopeID, intactID, 2000, 5)

	report, err := indexretention.Drain(ctx, SQLDB{DB: database},
		indexretention.Generation{ScopeID: scopeID, GenerationID: drainedID}, &indexretention.Budget{RowCap: 1 << 40})
	if err != nil || !report.Drained() || report.TermsDeleted != 10000 {
		t.Fatalf("pre-delete of %s = %+v, %v; want drained with 10000 terms", drainedID, report, err)
	}

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(ctx, retentionSelectionPolicy(0, 7*24*time.Hour, 10))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 2 {
		t.Fatalf("GenerationsPruned = %d, want 2 (skipped %v)", result.GenerationsPruned, result.Skipped)
	}
	if got := result.RowsPruned["eshu_search_index_terms"]; got != 10000 {
		t.Fatalf("RowsPruned[eshu_search_index_terms] = %d, want the intact generation's 10000", got)
	}
	for _, generationID := range []string{drainedID, intactID} {
		var exists bool
		if err := database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM scope_generations WHERE generation_id = $1)`, generationID).Scan(&exists); err != nil {
			t.Fatalf("read generation %s: %v", generationID, err)
		}
		if exists {
			t.Fatalf("generation %s survived the pass, want it pruned", generationID)
		}
		if terms, documents := countRetentionSearchIndex(t, ctx, database, scopeID, generationID); terms != 0 || documents != 0 {
			t.Fatalf("pruned generation %s keeps %d terms, %d documents, want none", generationID, terms, documents)
		}
	}
	requireNoSearchIndexOrphans(t, ctx, database)
	if terms, documents := countRetentionSearchIndex(t, ctx, database, scopeID, activeID); terms != 2000 || documents != 1000 {
		t.Fatalf("active generation has %d terms, %d documents, want 2000 and 1000", terms, documents)
	}
}

// TestGenerationRetentionLateTermsWriterLeavesASweptInvisibleOrphanLive is the
// orphan-visibility invariant once nothing rejects a terms row for a missing
// generation. A late writer adds terms for a generation retention already
// pruned; before #7427 PR3 the foreign key refused them (SQLSTATE 23503).
// Now they are stored, and they must stay invisible to the search reader,
// which joins the scope's active generation, until the orphan census lists
// them and the sweep deletes them.
func TestGenerationRetentionLateTermsWriterLeavesASweptInvisibleOrphanLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	requireTermsGenerationForeignKeyDropped(t, ctx, database)
	now := time.Now().UTC()
	scopeID := "scope-7427pr3o-" + now.Format("150405.000000000")
	const repoID = "repo-7427pr3"
	activeID, prunedID := scopeID+"-active", scopeID+"-pruned"
	testfixtures.SeedScope(t, ctx, database, scopeID)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, prunedID, now.Add(-10*24*time.Hour))
	seedSearchableGeneration(t, ctx, database, scopeID, activeID, repoID, "doc-active", "zebra")
	seedSearchableGeneration(t, ctx, database, scopeID, prunedID, repoID, "doc-pruned", "zebra")

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	if result, err := store.PruneSupersededGenerations(ctx, retentionSelectionPolicy(0, 7*24*time.Hour, 10)); err != nil || result.GenerationsPruned != 1 {
		t.Fatalf("PruneSupersededGenerations() = %d pruned, %v; want 1", result.GenerationsPruned, err)
	}

	// The late writer: its generation row is gone, its terms land anyway.
	for _, term := range []string{"zebra", "quokka"} {
		execRetentionSeed(t, ctx, database, `INSERT INTO eshu_search_index_terms (scope_id, generation_id, document_id, term_key, term, term_frequency)
VALUES ($1, $2, 'doc-pruned', $3, $4, 7)`, scopeID, prunedID, searchhybrid.TermKey(term), term)
	}
	census, err := indexretention.TakeCensus(ctx, SQLDB{DB: database})
	if err != nil {
		t.Fatalf("TakeCensus() error = %v", err)
	}
	if census.OrphanGenerations[indexretention.TermsTable] != 1 || census.OrphanGenerations[indexretention.DocumentsTable] != 0 {
		t.Fatalf("census after the late write = %v, want one terms orphan and no documents orphan", census.OrphanGenerations)
	}

	reader := indexstore.NewEshuSearchIndexStore(SQLDB{DB: database})
	search := indexstore.EshuSearchIndexSearch{
		ScopeID: scopeID, RepoID: repoID, Query: "zebra quokka", Limit: 10,
		Anchor: searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: repoID},
	}
	found, err := reader.Search(ctx, search)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(found.Candidates) != 1 || found.Candidates[0].Document.ID != "doc-active" {
		t.Fatalf("Search() candidates = %+v, want only doc-active: an orphan posting must not be read", found.Candidates)
	}
	search.Query = "quokka"
	if found, err = reader.Search(ctx, search); err != nil || len(found.Candidates) != 0 {
		t.Fatalf("Search(quokka) = %d candidates, %v; want none: only the orphan carries that term", len(found.Candidates), err)
	}

	sweep, err := indexretention.SweepOrphans(ctx, SQLDB{DB: database}, time.Time{}, 0, nil)
	if err != nil {
		t.Fatalf("SweepOrphans() error = %v", err)
	}
	if got := sweep.RowsSwept[indexretention.TermsTable]; got != 2 {
		t.Fatalf("swept terms rows = %d, want the late writer's 2", got)
	}
	requireNoSearchIndexOrphans(t, ctx, database)
	if terms, _ := countRetentionSearchIndex(t, ctx, database, scopeID, activeID); terms != 1 {
		t.Fatalf("active generation terms after the sweep = %d, want 1", terms)
	}
}

// seedSearchableGeneration gives one generation one searchable document with
// one term, plus the stats row the reader scores with.
func seedSearchableGeneration(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID, repoID, documentID, term string) {
	t.Helper()
	payload, err := json.Marshal(searchIndexDocumentFixture(documentID, repoID, term+" runbook"))
	if err != nil {
		t.Fatalf("marshal search document: %v", err)
	}
	execRetentionSeed(t, ctx, database, `INSERT INTO eshu_search_index_documents (scope_id, generation_id, document_id, fact_id, repo_id,
    source_kind, document, document_length, updated_at)
VALUES ($1, $2, $3, $3, $4, $5, $6::jsonb, 10, now())`,
		scopeID, generationID, documentID, repoID, string(searchdocs.SourceKindRuntimeSummary), string(payload))
	execRetentionSeed(t, ctx, database, `INSERT INTO eshu_search_index_stats (scope_id, generation_id, document_count, average_document_length, updated_at)
VALUES ($1, $2, 1, 10, now())`, scopeID, generationID)
	execRetentionSeed(t, ctx, database, `INSERT INTO eshu_search_index_terms (scope_id, generation_id, document_id, term_key, term, term_frequency)
VALUES ($1, $2, $3, $4, $5, 3)`, scopeID, generationID, documentID, searchhybrid.TermKey(term), term)
}
