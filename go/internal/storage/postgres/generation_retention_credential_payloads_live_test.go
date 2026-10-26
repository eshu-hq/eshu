// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8833 retention check for the #8813 superseded-payload decision: under DefaultGenerationRetentionPolicy the production prune deletes a superseded generation's content fact, content_entity fact, reducer_eshu_search_document fact, and eshu_search_index_documents and eshu_search_index_terms rows once the generation passes the 90-day ceiling or falls outside the newest-24 / 7-day window, and keeps them inside it; none of the five kinds is exempt; migrated schema per test
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

const retentionCredentialSeed = "RetentionCredSeed0123456789"

// seedCredentialCopies gives one generation one row of each kind that carries
// a raw credential copy: a content fact (content_body), a content_entity fact
// (source_cache), a reducer_eshu_search_document fact (ContextText), and the
// search index document and term.
func seedCredentialCopies(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string) {
	t.Helper()
	for _, row := range []struct{ kind, payload string }{
		{"content", `{"content_path":".yarnrc.yml","content_body":"npmAuthToken: ` + retentionCredentialSeed + `"}`},
		{"content_entity", `{"relative_path":".yarnrc.yml","source_cache":"npmAuthToken: ` + retentionCredentialSeed + `"}`},
		{"reducer_eshu_search_document", `{"repo_id":"` + scopeID + `","document":{"Path":".yarnrc.yml","ContextText":"npmAuthToken: ` + retentionCredentialSeed + `"}}`},
	} {
		if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, $4, $1, 'git', $1, now(), now(), $5::jsonb)`,
			generationID+"-"+row.kind, scopeID, generationID, row.kind, row.payload); err != nil {
			t.Fatalf("seed %s fact for %s: %v", row.kind, generationID, err)
		}
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO eshu_search_index_documents (scope_id, generation_id, document_id, fact_id, repo_id,
    source_kind, document, document_length, updated_at)
VALUES ($1, $2, 'doc-yarnrc', $2 || '-reducer_eshu_search_document', $1, 'file',
    jsonb_build_object('Path', '.yarnrc.yml', 'ContextText', 'npmAuthToken: ' || $3::text), 4, now())`,
		scopeID, generationID, retentionCredentialSeed); err != nil {
		t.Fatalf("seed index document for %s: %v", generationID, err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO eshu_search_index_terms (scope_id, generation_id, document_id, term_key, term, term_frequency)
VALUES ($1, $2, 'doc-yarnrc', 'tk-seed', lower($3::text), 1)`, scopeID, generationID, retentionCredentialSeed); err != nil {
		t.Fatalf("seed index term for %s: %v", generationID, err)
	}
}

// credentialCopies counts one generation's rows of the five kinds.
func credentialCopies(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string) map[string]int64 {
	t.Helper()
	counts := map[string]int64{}
	for _, kind := range []string{"content", "content_entity", "reducer_eshu_search_document"} {
		var n int64
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM fact_records
WHERE scope_id = $1 AND generation_id = $2 AND fact_kind = $3`, scopeID, generationID, kind).Scan(&n); err != nil {
			t.Fatalf("count %s facts: %v", kind, err)
		}
		counts["fact:"+kind] = n
	}
	terms, documents := countRetentionSearchIndex(t, ctx, database, scopeID, generationID)
	counts["eshu_search_index_documents"], counts["eshu_search_index_terms"] = documents, terms
	return counts
}

// TestGenerationRetentionPrunesCredentialCopiesByDefaultLive is the #8833
// retention check. The #8813 ruling accepted the superseded-payload window
// (no scrub) on the condition that none of the five kinds is exempt from the
// default retention policy. A generation past the 90-day ceiling, and one
// outside the newest-24 / 7-day window, lose every copy; a recent superseded
// generation inside the window keeps them; the active generation is never
// touched.
func TestGenerationRetentionPrunesCredentialCopiesByDefaultLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	scopeID := "scope-8833-" + now.Format("150405.000000000")
	testfixtures.SeedScope(t, ctx, database, scopeID)
	activeID := scopeID + "-active"
	seedCredentialCopies(t, ctx, database, scopeID, activeID)

	// 25 superseded generations, 8 to 32 days old: the oldest is outside the
	// newest-24 window and older than 7 days. One more is past the 90-day
	// ceiling. One recent generation sits inside the window.
	ceilingID, overflowID, recentID := scopeID+"-ceiling", scopeID+"-overflow-0", scopeID+"-overflow-24"
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, ceilingID, now.Add(-100*24*time.Hour))
	for i := 0; i < 25; i++ {
		testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, fmt.Sprintf("%s-overflow-%d", scopeID, i),
			now.Add(-time.Duration(32-i)*24*time.Hour))
	}
	for _, generation := range []string{ceilingID, overflowID, recentID} {
		seedCredentialCopies(t, ctx, database, scopeID, generation)
	}

	for _, generation := range []string{ceilingID, overflowID} {
		for kind, n := range credentialCopies(t, ctx, database, scopeID, generation) {
			if n != 1 {
				t.Fatalf("%s: seeded %s rows = %d, want 1 (the check would pass vacuously)", generation, kind, n)
			}
		}
	}

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(ctx, DefaultGenerationRetentionPolicy())
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned < 2 {
		t.Fatalf("GenerationsPruned = %d, want at least the ceiling and the overflow generation", result.GenerationsPruned)
	}
	for _, generation := range []string{ceilingID, overflowID} {
		for kind, n := range credentialCopies(t, ctx, database, scopeID, generation) {
			if n != 0 {
				t.Errorf("%s: %d %s rows survived the default retention pass; the kind is exempt and the #8813 payload decision reopens", generation, n, kind)
			}
		}
	}
	for _, generation := range []string{recentID, activeID} {
		for kind, n := range credentialCopies(t, ctx, database, scopeID, generation) {
			if n != 1 {
				t.Errorf("%s: %s rows = %d, want 1 (inside the retention window)", generation, kind, n)
			}
		}
	}
}
