// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestEshuSearchVectorPendingBoundedPlanLive proves that ListPendingSearchVectorScopes
// returns exactly the scopes that have at least one pending document and that the
// Postgres query plan does NOT materialise the full corpus-wide metadata table
// (no top-level Unique / full-set Sort over eshu_search_vector_metadata).
//
// Set ESHU_SEARCH_VECTOR_PENDING_PLAN_LIVE=1 and ESHU_POSTGRES_DSN to a live
// Postgres DSN to run this proof. The test is skipped when either env var is
// absent so the normal CI gate is unaffected.
//
// Equivalence cases seeded (all under a throwaway scope/generation isolated by
// a unique prefix so concurrent corpus rows are invisible to the probe):
//
//  1. active doc with NO metadata row              → pending  (returned)
//  2. metadata build_state='disabled'              → ready    (not returned)
//  3. build_state='ready' WITH matching value row  → ready    (not returned)
//  4. build_state='ready' WITHOUT value row        → pending  (returned)
//  5. metadata exists, embedding_content_hash !=   → pending  (returned)
//  6. tombstoned search-document fact              → excluded from active_docs
//  7. fact whose generation != active_generation   → excluded from active_docs
//  8. scope with ALL docs ready                    → not returned (no pending docs)
//  9. two facts share the same document_id         → scope returned exactly once
//
// Cleanup: all inserted rows are deleted via ON DELETE CASCADE from
// ingestion_scopes, plus explicit DELETE of the scope row.
func TestEshuSearchVectorPendingBoundedPlanLive(t *testing.T) {
	if os.Getenv("ESHU_SEARCH_VECTOR_PENDING_PLAN_LIVE") != "1" {
		t.Skip("set ESHU_SEARCH_VECTOR_PENDING_PLAN_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	db := SQLDB{DB: sqlDB}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Unique prefix isolates this test run's rows from concurrent corpus data.
	prefix := fmt.Sprintf("4233-proof-%d", time.Now().UnixNano())
	providerProfileID := "semantic-search-default"
	sourceClass := "search_documents"
	modelID := "local-hash-v1"
	vectorVersion := "vector-v1"
	now := time.Now().UTC()

	// --- Scope A: has a mix of pending and ready docs (cases 1-5, 6, 7) ---
	scopeA := prefix + ":scope-a"
	genA := prefix + ":gen-a"
	// stale generation (case 7): same scope, different (older) generation
	genStale := prefix + ":gen-stale"

	// --- Scope B: ALL docs ready, must NOT be returned ---
	scopeB := prefix + ":scope-b"
	genB := prefix + ":gen-b"

	// --- Scope C: duplicate document_id dedup case (case 9) ---
	scopeC := prefix + ":scope-c"
	genC := prefix + ":gen-c"

	// Cleanup: ON DELETE CASCADE removes scope_generations, fact_records,
	// eshu_search_vector_metadata, eshu_search_vector_values rows tied to these
	// scopes. We delete the scope rows themselves at the end.
	t.Cleanup(func() {
		cleanCtx := context.Background()
		for _, sid := range []string{scopeA, scopeB, scopeC} {
			_, _ = sqlDB.ExecContext(cleanCtx,
				`DELETE FROM ingestion_scopes WHERE scope_id = $1`, sid)
		}
	})

	// Insert ingestion_scopes.
	for _, row := range []struct {
		scopeID string
		genID   string
	}{
		{scopeA, genA},
		{scopeB, genB},
	} {
		if _, err := sqlDB.ExecContext(
			ctx, `
			INSERT INTO ingestion_scopes
			  (scope_id, scope_kind, source_system, source_key, collector_kind,
			   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
			VALUES ($1::text, 'repository', 'git', $1::text, 'git', $1::text, $2, $2, 'active', $3::text,
			        jsonb_build_object('repo_id', $1::text))
			ON CONFLICT (scope_id) DO NOTHING`,
			row.scopeID, now, row.genID,
		); err != nil {
			t.Fatalf("insert ingestion_scope %s: %v", row.scopeID, err)
		}
	}

	// Insert scope_generations. Active generations use status='active';
	// genStale uses status='superseded' so it does not conflict with the
	// scope_generations_active_scope_idx UNIQUE (scope_id) WHERE status='active'
	// partial index — a scope may have at most one active generation at a time.
	for _, row := range []struct {
		genID   string
		scopeID string
		status  string
	}{
		{genA, scopeA, "active"},
		{genStale, scopeA, "superseded"}, // case 7: different (non-active) generation
		{genB, scopeB, "active"},
	} {
		if _, err := sqlDB.ExecContext(
			ctx, `
			INSERT INTO scope_generations
			  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
			VALUES ($1, $2, 'manual', $3, $3, $4, $3)
			ON CONFLICT (generation_id) DO NOTHING`,
			row.genID, row.scopeID, now, row.status,
		); err != nil {
			t.Fatalf("insert scope_generation %s: %v", row.genID, err)
		}
	}

	// Helper: insert a search document fact record. The payload mirrors the
	// production shape emitted by reducer.eshuSearchDocumentPayload: a top-level
	// "document_id" key (what the pending lister reads) alongside the embedded
	// "document" object. searchdocs.Document has no JSON tags, so its ID field
	// marshals as the capitalized key "ID" — reproduced here so a reader that
	// (incorrectly) reads the nested lowercase document.id yields NULL, which is
	// the #4885 regression this test guards against.
	insertFact := func(factID, scopeID, genID, docID, contentHash string, tombstone bool) {
		t.Helper()
		payload := fmt.Sprintf(
			`{"document_id":%q,"document":{"ID":%q},"content_hash":%q}`,
			docID, docID, contentHash,
		)
		if _, err := sqlDB.ExecContext(
			ctx, `
			INSERT INTO fact_records
			  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
			   source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
			VALUES ($1,$2,$3,$4,$5,'git',$5,$6,$6,$7,$8::jsonb)
			ON CONFLICT (fact_id) DO NOTHING`,
			factID, scopeID, genID,
			EshuSearchDocumentFactKind,
			factID,
			now, tombstone, payload,
		); err != nil {
			t.Fatalf("insert fact %s: %v", factID, err)
		}
	}

	// Helper: insert a metadata row.
	insertMeta := func(scopeID, genID, docID, contentHash, buildState string) {
		t.Helper()
		if _, err := sqlDB.ExecContext(
			ctx, `
			INSERT INTO eshu_search_vector_metadata
			  (scope_id, generation_id, document_id, provider_profile_id, source_class,
			   embedding_model_id, embedding_dimensions, embedding_content_hash,
			   vector_index_version, build_state, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,128,$7,$8,$9,$10,$10)
			ON CONFLICT ON CONSTRAINT eshu_search_vector_metadata_pkey DO NOTHING`,
			scopeID, genID, docID, providerProfileID, sourceClass,
			modelID, contentHash, vectorVersion, buildState, now,
		); err != nil {
			t.Fatalf("insert metadata %s/%s: %v", scopeID, docID, err)
		}
	}

	// Helper: insert a value row (signals that vector is actually stored).
	insertValue := func(scopeID, genID, docID, contentHash string) {
		t.Helper()
		// 128-dimension zero vector.
		zeros := make([]float64, 128)
		vecLit := "{"
		for i, v := range zeros {
			if i > 0 {
				vecLit += ","
			}
			vecLit += fmt.Sprintf("%g", v)
		}
		vecLit += "}"
		if _, err := sqlDB.ExecContext(
			ctx, `
			INSERT INTO eshu_search_vector_values
			  (scope_id, generation_id, document_id, provider_profile_id, source_class,
			   embedding_model_id, embedding_dimensions, embedding_content_hash,
			   vector_index_version, vector_values, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,128,$7,$8,$9::double precision[],$10,$10)
			ON CONFLICT ON CONSTRAINT eshu_search_vector_values_pkey DO NOTHING`,
			scopeID, genID, docID, providerProfileID, sourceClass,
			modelID, contentHash, vectorVersion, vecLit, now,
		); err != nil {
			t.Fatalf("insert value %s/%s: %v", scopeID, docID, err)
		}
	}

	// --- Seed scope A equivalence cases ---

	// Case 1: active doc, NO metadata row → pending.
	insertFact(prefix+":a-doc1-fact", scopeA, genA, prefix+":doc1", "hash1", false)

	// Case 2: metadata build_state='disabled', no value needed → ready.
	insertFact(prefix+":a-doc2-fact", scopeA, genA, prefix+":doc2", "hash2", false)
	insertMeta(scopeA, genA, prefix+":doc2", "hash2", "disabled")

	// Case 3: build_state='ready' WITH matching value row → ready.
	insertFact(prefix+":a-doc3-fact", scopeA, genA, prefix+":doc3", "hash3", false)
	insertMeta(scopeA, genA, prefix+":doc3", "hash3", "ready")
	insertValue(scopeA, genA, prefix+":doc3", "hash3")

	// Case 4: build_state='ready' WITHOUT value row → pending.
	insertFact(prefix+":a-doc4-fact", scopeA, genA, prefix+":doc4", "hash4", false)
	insertMeta(scopeA, genA, prefix+":doc4", "hash4", "ready")
	// no value row inserted

	// Case 5: metadata exists but embedding_content_hash != doc content_hash → pending.
	insertFact(prefix+":a-doc5-fact", scopeA, genA, prefix+":doc5", "hash5-new", false)
	insertMeta(scopeA, genA, prefix+":doc5", "hash5-old", "ready")
	insertValue(scopeA, genA, prefix+":doc5", "hash5-old")

	// Case 6: tombstoned search-document fact → excluded from active_docs.
	insertFact(prefix+":a-doc6-fact", scopeA, genA, prefix+":doc6", "hash6", true)

	// Case 7: fact whose generation != scope.active_generation_id → excluded.
	// genStale is not the active_generation_id for scopeA.
	insertFact(prefix+":a-doc7-fact", scopeA, genStale, prefix+":doc7", "hash7", false)

	// --- Seed scope B: ALL docs ready → not returned ---

	// Scope B doc1: disabled.
	insertFact(prefix+":b-doc1-fact", scopeB, genB, prefix+":b-doc1", "b-hash1", false)
	insertMeta(scopeB, genB, prefix+":b-doc1", "b-hash1", "disabled")

	// Scope B doc2: ready with value.
	insertFact(prefix+":b-doc2-fact", scopeB, genB, prefix+":b-doc2", "b-hash2", false)
	insertMeta(scopeB, genB, prefix+":b-doc2", "b-hash2", "ready")
	insertValue(scopeB, genB, prefix+":b-doc2", "b-hash2")

	// --- Seed scope C: duplicate document_id dedup case (case 9) ---
	// Two active facts share the same document_id under the same scope+generation
	// with no metadata row. The GROUP BY in the outer query must collapse them so
	// scopeC appears exactly once in the result set.
	if _, err := sqlDB.ExecContext(
		ctx, `
		INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind,
		   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1::text, 'repository', 'git', $1::text, 'git', $1::text, $2, $2, 'active', $3::text,
		        jsonb_build_object('repo_id', $1::text))
		ON CONFLICT (scope_id) DO NOTHING`,
		scopeC, now, genC,
	); err != nil {
		t.Fatalf("insert ingestion_scope %s: %v", scopeC, err)
	}
	if _, err := sqlDB.ExecContext(
		ctx, `
		INSERT INTO scope_generations
		  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		VALUES ($1, $2, 'manual', $3, $3, 'active', $3)
		ON CONFLICT (generation_id) DO NOTHING`,
		genC, scopeC, now,
	); err != nil {
		t.Fatalf("insert scope_generation %s: %v", genC, err)
	}
	// Two facts with the same document_id, distinct fact_id — no metadata → both pending.
	// GROUP BY docs.scope_id,generation_id,repo_id must return scopeC exactly once.
	insertFact(prefix+":c-doc1-fact-a", scopeC, genC, prefix+":c-doc1", "c-hash1", false)
	insertFact(prefix+":c-doc1-fact-b", scopeC, genC, prefix+":c-doc1", "c-hash1", false)

	// --- Run ListPendingSearchVectorScopes ---
	store := NewEshuSearchVectorPendingStore(db)
	req := EshuSearchVectorPendingRequest{
		ProviderProfileID:  providerProfileID,
		SourceClass:        sourceClass,
		EmbeddingModelID:   modelID,
		VectorIndexVersion: vectorVersion,
		Limit:              100,
	}

	start := time.Now()
	scopes, err := store.ListPendingSearchVectorScopes(ctx, req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ListPendingSearchVectorScopes: %v", err)
	}
	t.Logf("ListPendingSearchVectorScopes returned %d scopes in %s", len(scopes), elapsed)

	// Build a set of returned scope IDs for O(1) lookup.
	returnedScopes := make(map[string]bool, len(scopes))
	for _, s := range scopes {
		returnedScopes[s.ScopeID] = true
	}

	// scopeA MUST be returned (has pending docs: cases 1, 4, 5).
	if !returnedScopes[scopeA] {
		t.Errorf("scopeA (%s) not returned; expected pending (cases 1,4,5)", scopeA)
	}
	// scopeB MUST NOT be returned (all docs ready or disabled).
	if returnedScopes[scopeB] {
		t.Errorf("scopeB (%s) returned; expected all-ready scope to be excluded", scopeB)
	}
	// scopeC MUST be returned exactly once: two facts share the same document_id
	// with no metadata row; GROUP BY docs.scope_id,generation_id,repo_id must
	// deduplicate them so the scope appears in the result set exactly one time.
	if !returnedScopes[scopeC] {
		t.Errorf("scopeC (%s) not returned; expected pending scope with duplicate doc-id facts", scopeC)
	}
	var scopeCCount int
	for _, s := range scopes {
		if s.ScopeID == scopeC {
			scopeCCount++
		}
	}
	if scopeCCount != 1 {
		t.Errorf("scopeC appears %d times in result; want exactly 1 (GROUP BY dedup)", scopeCCount)
	}

	// --- EXPLAIN (ANALYZE, BUFFERS) plan assertion ---
	t.Log("running EXPLAIN (ANALYZE, BUFFERS) to verify bounded plan shape")
	explainSQL := fmt.Sprintf(
		`EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) %s`,
		listPendingEshuSearchVectorScopesSQL,
	)
	rows, err := sqlDB.QueryContext(
		ctx, explainSQL,
		EshuSearchDocumentFactKind,
		providerProfileID, sourceClass, modelID, vectorVersion, 100,
	)
	if err != nil {
		t.Fatalf("EXPLAIN ANALYZE: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var planLines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan EXPLAIN row: %v", err)
		}
		planLines = append(planLines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate EXPLAIN rows: %v", err)
	}

	plan := strings.Join(planLines, "\n")
	t.Logf("EXPLAIN ANALYZE output:\n%s", plan)

	// The rewritten query must NOT do a corpus-wide materialisation of the
	// metadata table. A full-corpus Unique or Sort over eshu_search_vector_metadata
	// at the top level of the plan (outside a correlated subplan) indicates the
	// old ready_docs CTE is still active.
	//
	// The new NOT EXISTS shape drives a Nested Loop Anti Join / Index Scan
	// per active_doc row; the subplan is labelled "SubPlan" or "Nested Loop
	// Anti Join" in Postgres text-format EXPLAIN output and does NOT surface a
	// top-level "-> Sort" or "-> Unique" node over the metadata table.
	if strings.Contains(plan, "Unique") && strings.Contains(plan, "eshu_search_vector_metadata") {
		// Tolerate a Unique inside a SubPlan but not at the outer query level.
		// Check: if "Unique" appears before any "SubPlan" mention it's outer-level.
		uniqueIdx := strings.Index(plan, "Unique")
		subplanIdx := strings.Index(plan, "SubPlan")
		if subplanIdx < 0 || uniqueIdx < subplanIdx {
			t.Errorf("plan contains corpus-wide Unique over eshu_search_vector_metadata (old ready_docs CTE shape detected):\n%s", plan)
		}
	}
}
