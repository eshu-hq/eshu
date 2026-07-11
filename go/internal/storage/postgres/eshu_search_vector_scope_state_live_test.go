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

// TestEshuSearchVectorScopeStateSeederEquivalenceLive proves:
//
//  1. The seeder populates eshu_search_document_projection_state rows correctly.
//  2. The seeder records conservative building vector-scope rows, then the
//     scheduler's exact check/CAS marks only complete scopes ready.
//  3. EXACT EQUIVALENCE: after scheduler finalization, the OLD pending lister
//     and the NEW pending lister
//     produce the same set of pending scopes (symmetric diff 0/0).
//  4. A count-equal-but-stale scope IS pending (regression against a count
//     shortcut).
//  5. CAS: FinalizeReady with stale fence/revision returns false; current
//     returns true; duplicate current is idempotent.
//  6. EXPLAIN the NEW query — no fact_records scan node.
//
// Set ESHU_SEARCH_VECTOR_SCOPE_STATE_LIVE=1 and ESHU_POSTGRES_DSN to run.
func TestEshuSearchVectorScopeStateSeederEquivalenceLive(t *testing.T) {
	if os.Getenv("ESHU_SEARCH_VECTOR_SCOPE_STATE_LIVE") != "1" {
		t.Skip("set ESHU_SEARCH_VECTOR_SCOPE_STATE_LIVE=1 and ESHU_POSTGRES_DSN to run")
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

	// Unique prefix isolates this test run.
	prefix := fmt.Sprintf("4233-live-%d", time.Now().UnixNano())
	providerProfileID := "semantic-search-default"
	sourceClass := "search_documents"
	modelID := "local-hash-v1"
	vectorVersion := "vector-v1"
	identity := EshuSearchVectorIdentity{
		ProviderProfileID:  providerProfileID,
		SourceClass:        sourceClass,
		EmbeddingModelID:   modelID,
		VectorIndexVersion: vectorVersion,
	}
	now := time.Now().UTC()

	// Helper: insert a search-document fact.
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
			factID, scopeID, genID, EshuSearchDocumentFactKind, factID,
			now, tombstone, payload,
		); err != nil {
			t.Fatalf("insert fact %s: %v", factID, err)
		}
		if tombstone {
			return
		}
		if _, err := sqlDB.ExecContext(
			ctx, `
			INSERT INTO eshu_search_index_documents
			  (scope_id, generation_id, document_id, fact_id, repo_id, source_kind,
			   content_hash, document, document_length, updated_at)
			VALUES ($1,$2,$3,$4,$1,'code_entity',$5,
			        jsonb_build_object('ID',$3::text),1,$6)
			ON CONFLICT (scope_id,generation_id,document_id) DO NOTHING`,
			scopeID, genID, docID, factID, contentHash, now,
		); err != nil {
			t.Fatalf("insert search index document %s: %v", factID, err)
		}
	}

	// Helper: insert a vector metadata row.
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

	// Helper: insert a vector value row.
	insertValue := func(scopeID, genID, docID, contentHash string) {
		t.Helper()
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

	// Helper: create scope with active generation.
	createScope := func(scopeID, genID string) {
		t.Helper()
		if _, err := sqlDB.ExecContext(
			ctx, `
			INSERT INTO ingestion_scopes
			  (scope_id, scope_kind, source_system, source_key, collector_kind,
			   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
			VALUES ($1::text, 'repository', 'git', $1::text, 'git', $1::text, $2, $2, 'active', $3::text,
			        jsonb_build_object('repo_id', $1::text))
			ON CONFLICT (scope_id) DO NOTHING`,
			scopeID, now, genID,
		); err != nil {
			t.Fatalf("insert ingestion_scope %s: %v", scopeID, err)
		}
		if _, err := sqlDB.ExecContext(
			ctx, `
			INSERT INTO scope_generations
			  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
			VALUES ($1, $2, 'manual', $3, $3, 'active', $3)
			ON CONFLICT (generation_id) DO NOTHING`,
			genID, scopeID, now,
		); err != nil {
			t.Fatalf("insert scope_generation %s: %v", genID, err)
		}
	}

	// ---- Setup 6 scopes ----
	// Scope A: complete (all docs have metadata+value with matching hash)
	scopeA := prefix + ":scope-a"
	genA := prefix + ":gen-a"
	createScope(scopeA, genA)
	insertFact(prefix+":a-doc1-fact", scopeA, genA, prefix+":a-doc1", "a-hash1", false)
	insertMeta(scopeA, genA, prefix+":a-doc1", "a-hash1", "ready")
	insertValue(scopeA, genA, prefix+":a-doc1", "a-hash1")
	insertFact(prefix+":a-doc2-fact", scopeA, genA, prefix+":a-doc2", "a-hash2", false)
	insertMeta(scopeA, genA, prefix+":a-doc2", "a-hash2", "disabled")

	// Scope B: missing vector entirely (pending)
	scopeB := prefix + ":scope-b"
	genB := prefix + ":gen-b"
	createScope(scopeB, genB)
	insertFact(prefix+":b-doc1-fact", scopeB, genB, prefix+":b-doc1", "b-hash1", false)

	// Scope C: count-equal-but-stale — has metadata with matching count
	// but different content hash (pending despite same doc count)
	scopeC := prefix + ":scope-c"
	genC := prefix + ":gen-c"
	createScope(scopeC, genC)
	insertFact(prefix+":c-doc1-fact", scopeC, genC, prefix+":c-doc1", "c-hash-new", false)
	insertMeta(scopeC, genC, prefix+":c-doc1", "c-hash-old", "ready")
	insertValue(scopeC, genC, prefix+":c-doc1", "c-hash-old")

	// Scope D: build_state='ready' but NO value row (pending)
	scopeD := prefix + ":scope-d"
	genD := prefix + ":gen-d"
	createScope(scopeD, genD)
	insertFact(prefix+":d-doc1-fact", scopeD, genD, prefix+":d-doc1", "d-hash1", false)
	insertMeta(scopeD, genD, prefix+":d-doc1", "d-hash1", "ready")
	// No value row

	// Scope E: empty (no facts) → should get projection_state with count=0,
	// but NO vector_scope_state row (document_count=0 skip)
	scopeE := prefix + ":scope-e"
	genE := prefix + ":gen-e"
	createScope(scopeE, genE)

	// Scope F: complete with two docs
	scopeF := prefix + ":scope-f"
	genF := prefix + ":gen-f"
	createScope(scopeF, genF)
	insertFact(prefix+":f-doc1-fact", scopeF, genF, prefix+":f-doc1", "f-hash1", false)
	insertMeta(scopeF, genF, prefix+":f-doc1", "f-hash1", "ready")
	insertValue(scopeF, genF, prefix+":f-doc1", "f-hash1")
	insertFact(prefix+":f-doc2-fact", scopeF, genF, prefix+":f-doc2", "f-hash2", false)
	insertMeta(scopeF, genF, prefix+":f-doc2", "f-hash2", "disabled")

	// Cleanup: ON DELETE CASCADE handles everything connected to the scope.
	t.Cleanup(func() {
		cleanCtx := context.Background()
		for _, sid := range []string{scopeA, scopeB, scopeC, scopeD, scopeE, scopeF} {
			_, _ = sqlDB.ExecContext(cleanCtx,
				`DELETE FROM ingestion_scopes WHERE scope_id = $1`, sid)
		}
	})

	// --- Run the seeder ---
	t.Log("running seeder...")
	if err := SeedSearchVectorScopeState(ctx, db, identity); err != nil {
		t.Fatalf("SeedSearchVectorScopeState: %v", err)
	}

	// --- Assert projection_state rows ---
	for _, tc := range []struct {
		scopeID      string
		wantCount    int64
		wantState    string
		wantRevision int64
		wantFence    int64
	}{
		{scopeA, 2, "ready", 1, 1},
		{scopeB, 1, "ready", 1, 1},
		{scopeC, 1, "ready", 1, 1},
		{scopeD, 1, "ready", 1, 1},
		{scopeE, 0, "ready", 1, 1},
		{scopeF, 2, "ready", 1, 1},
	} {
		var docCount int64
		var state string
		var revision, fence int64
		err := sqlDB.QueryRowContext(ctx, `
			SELECT document_count, state, projection_revision, build_fence
			FROM eshu_search_document_projection_state
			WHERE scope_id = $1`, tc.scopeID).Scan(&docCount, &state, &revision, &fence)
		if err != nil {
			t.Fatalf("query projection_state for %s: %v", tc.scopeID, err)
		}
		if docCount != tc.wantCount {
			t.Errorf("%s document_count = %d, want %d", tc.scopeID, docCount, tc.wantCount)
		}
		if state != tc.wantState {
			t.Errorf("%s state = %s, want %s", tc.scopeID, state, tc.wantState)
		}
		if revision != tc.wantRevision {
			t.Errorf("%s revision = %d, want %d", tc.scopeID, revision, tc.wantRevision)
		}
		if fence != tc.wantFence {
			t.Errorf("%s fence = %d, want %d", tc.scopeID, fence, tc.wantFence)
		}
	}

	// --- Assert conservative vector_scope_state seed rows. ---
	for _, scopeID := range []string{scopeA, scopeB, scopeC, scopeD, scopeE, scopeF} {
		var count int
		if err := sqlDB.QueryRowContext(ctx, `
			SELECT count(*) FROM eshu_search_vector_scope_state
			WHERE scope_id = $1 AND state = 'building'`, scopeID).Scan(&count); err != nil {
			t.Fatalf("query vector_scope_state for %s: %v", scopeID, err)
		}
		want := 1
		if scopeID == scopeE {
			want = 0
		}
		if count != want {
			t.Errorf("%s vector_scope_state building rows = %d, want %d", scopeID, count, want)
		}
	}

	// Simulate the bounded scheduler's exact completion check and ready CAS for
	// the two complete scopes before comparing pending-set semantics.
	newStore := NewEshuSearchVectorScopeStateStore(db)
	for _, completeScope := range []struct{ scopeID, generationID string }{
		{scopeA, genA},
		{scopeF, genF},
	} {
		complete, err := newStore.ScopeVectorComplete(ctx, completeScope.scopeID, completeScope.generationID, identity)
		if err != nil {
			t.Fatalf("ScopeVectorComplete %s: %v", completeScope.scopeID, err)
		}
		if !complete {
			t.Fatalf("ScopeVectorComplete %s = false, want true", completeScope.scopeID)
		}
		ok, err := newStore.FinalizeReady(ctx, completeScope.scopeID, completeScope.generationID, identity, 1, 1)
		if err != nil {
			t.Fatalf("FinalizeReady %s: %v", completeScope.scopeID, err)
		}
		if !ok {
			t.Fatalf("FinalizeReady %s = false, want true", completeScope.scopeID)
		}
	}

	// --- EXACT EQUIVALENCE: OLD pending vs NEW pending ---
	t.Log("computing old pending set...")
	oldStore := NewEshuSearchVectorPendingStore(db)
	oldReq := EshuSearchVectorPendingRequest{
		ProviderProfileID:  providerProfileID,
		SourceClass:        sourceClass,
		EmbeddingModelID:   modelID,
		VectorIndexVersion: vectorVersion,
		Limit:              1000,
	}
	oldScopes, err := oldStore.ListPendingSearchVectorScopes(ctx, oldReq)
	if err != nil {
		t.Fatalf("OLD ListPendingSearchVectorScopes: %v", err)
	}

	t.Log("computing new pending set...")
	newReq := EshuSearchVectorPendingRequest{
		ProviderProfileID:  providerProfileID,
		SourceClass:        sourceClass,
		EmbeddingModelID:   modelID,
		VectorIndexVersion: vectorVersion,
		Limit:              1000,
	}
	newScopes, err := newStore.ListPendingSearchVectorScopes(ctx, newReq)
	if err != nil {
		t.Fatalf("NEW ListPendingSearchVectorScopes: %v", err)
	}

	// Filter to only our test scopes (ignore pre-existing data).
	isTestScope := func(scopeID string) bool {
		return strings.HasPrefix(scopeID, prefix+":")
	}
	filterTestScopes := func(scopes []EshuSearchVectorPendingScope) map[string]bool {
		set := make(map[string]bool)
		for _, s := range scopes {
			if isTestScope(s.ScopeID) {
				set[s.ScopeID] = true
			}
		}
		return set
	}

	oldSet := filterTestScopes(oldScopes)
	newSet := filterTestScopes(newScopes)

	t.Logf("old pending test scopes: %v", mapKeys(oldSet))
	t.Logf("new pending test scopes: %v", mapKeys(newSet))

	// Symmetric difference: old minus new
	oldMinusNew := 0
	for sid := range oldSet {
		if !newSet[sid] {
			oldMinusNew++
			t.Errorf("OLD pending contains %s but NEW does not", sid)
		}
	}
	// New minus old
	newMinusOld := 0
	for sid := range newSet {
		if !oldSet[sid] {
			newMinusOld++
			t.Errorf("NEW pending contains %s but OLD does not", sid)
		}
	}
	t.Logf("exact equivalence: old=%d new=%d old-minus-new=%d new-minus-old=%d",
		len(oldSet), len(newSet), oldMinusNew, newMinusOld)
	if oldMinusNew != 0 || newMinusOld != 0 {
		t.Fatalf("symmetric diff not 0/0: old-minus-new=%d new-minus-old=%d", oldMinusNew, newMinusOld)
	}

	// --- Count-equal-but-stale: scope C MUST be pending ---
	if !oldSet[scopeC] {
		t.Errorf("scope C (count-equal-but-stale) not in OLD pending set")
	}
	if !newSet[scopeC] {
		t.Errorf("scope C (count-equal-but-stale) not in NEW pending set")
	}

	// --- CAS: FinalizeReady for scope A (should succeed) ---
	psStore := NewEshuSearchDocumentProjectionStateStore(db)
	ok, err := psStore.FinalizeReady(ctx, scopeA, genA, 1, 1, 2)
	if err != nil {
		t.Fatalf("FinalizeReady scope A: %v", err)
	}
	if !ok {
		t.Error("FinalizeReady scope A (current) returned false, want true")
	}
	// Duplicate call (same revision, current fence) — idempotent.
	ok, err = psStore.FinalizeReady(ctx, scopeA, genA, 1, 1, 2)
	if err != nil {
		t.Fatalf("FinalizeReady scope A (duplicate): %v", err)
	}
	if !ok {
		t.Error("FinalizeReady scope A (duplicate current) returned false, want true (idempotent)")
	}
	// Stale fence: should return false.
	ok, err = psStore.FinalizeReady(ctx, scopeA, genA, 1, 0, 2)
	if err != nil {
		t.Fatalf("FinalizeReady scope A (stale fence): %v", err)
	}
	if ok {
		t.Error("FinalizeReady scope A (stale fence) returned true, want false")
	}
	// Stale revision: should return false.
	ok, err = psStore.FinalizeReady(ctx, scopeA, genA, 2, 1, 2)
	if err != nil {
		t.Fatalf("FinalizeReady scope A (stale revision): %v", err)
	}
	if ok {
		t.Error("FinalizeReady scope A (stale revision) returned true, want false")
	}

	// --- CAS: FinalizeReady for vector scope state (scope F) ---
	vsStore := NewEshuSearchVectorScopeStateStore(db)
	ok, err = vsStore.FinalizeReady(ctx, scopeF, genF, identity, 1, 1)
	if err != nil {
		t.Fatalf("Vector FinalizeReady scope F: %v", err)
	}
	if !ok {
		t.Error("Vector FinalizeReady scope F returned false, want true")
	}
	// Stale fence.
	ok, err = vsStore.FinalizeReady(ctx, scopeF, genF, identity, 1, 0)
	if err != nil {
		t.Fatalf("Vector FinalizeReady scope F (stale fence): %v", err)
	}
	if ok {
		t.Error("Vector FinalizeReady scope F (stale fence) returned true, want false")
	}

	// --- EXPLAIN the NEW query: assert no fact_records scan ---
	t.Log("EXPLAIN new pending query...")
	explainSQL := fmt.Sprintf(
		`EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) %s`,
		listPendingSearchVectorScopesScopedSQL,
	)
	rows, err := sqlDB.QueryContext(
		ctx, explainSQL,
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

	// The new query MUST NOT scan fact_records — it operates from the
	// versioned state tables only.
	if strings.Contains(plan, "fact_records") {
		t.Errorf("NEW plan contains fact_records scan node (unbounded plan):\n%s", plan)
	}
}

func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
