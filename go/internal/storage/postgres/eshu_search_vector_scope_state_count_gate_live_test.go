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

const scopeVectorCompleteFactReferenceSQL = `
SELECT NOT EXISTS (
    SELECT 1
    FROM fact_records fact
    JOIN ingestion_scopes scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    WHERE fact.scope_id = $1
      AND fact.generation_id = $2
      AND fact.fact_kind = $3
      AND fact.is_tombstone = FALSE
      AND NOT EXISTS (
        SELECT 1
        FROM eshu_search_vector_metadata meta
        LEFT JOIN eshu_search_vector_values value
          ON value.scope_id = meta.scope_id
         AND value.generation_id = meta.generation_id
         AND value.document_id = meta.document_id
         AND value.provider_profile_id = meta.provider_profile_id
         AND value.source_class = meta.source_class
         AND value.embedding_model_id = meta.embedding_model_id
         AND value.vector_index_version = meta.vector_index_version
         AND value.embedding_content_hash = meta.embedding_content_hash
        WHERE meta.scope_id = fact.scope_id
          AND meta.generation_id = fact.generation_id
          AND meta.document_id = fact.payload->>'document_id'
          AND meta.provider_profile_id = $4
          AND meta.source_class = $5
          AND meta.embedding_model_id = $6
          AND meta.vector_index_version = $7
          AND meta.embedding_content_hash = fact.payload->>'content_hash'
          AND (meta.build_state = 'disabled'
               OR (meta.build_state = 'ready' AND value.document_id IS NOT NULL))
      )
) AS complete`

// TestScopeVectorCompleteCountGateAmortizationLive proves the count-gated
// indexed pending probe returns identical verdicts as the fact reference for
// every semantic case, and that the probe is not executed when the count gate
// rejects early.
func TestScopeVectorCompleteCountGateAmortizationLive(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	var ingestionScopesTable sql.NullString
	if err := sqlDB.QueryRowContext(ctx, `SELECT to_regclass('public.ingestion_scopes')`).Scan(&ingestionScopesTable); err != nil {
		t.Fatalf("probe bootstrap schema: %v", err)
	}
	if !ingestionScopesTable.Valid {
		if err := ApplyBootstrap(ctx, SQLDB{DB: sqlDB}); err != nil {
			t.Fatalf("apply bootstrap schema: %v", err)
		}
	}

	prefix := fmt.Sprintf("4233-cg-%d", time.Now().UnixNano())
	providerProfileID := "semantic-search-default"
	sourceClass := "search_documents"
	modelID := "local-hash-v1"
	vectorVersion := "vector-v1"
	identity := EshuSearchVectorIdentity{
		ProviderProfileID: providerProfileID, SourceClass: sourceClass,
		EmbeddingModelID: modelID, VectorIndexVersion: vectorVersion,
	}
	now := time.Now().UTC()

	store := NewEshuSearchVectorScopeStateStore(SQLDB{DB: sqlDB})

	// Helper: one scope + generation + facts + projection_state.
	setupScope := func(label string, docCount int64) (scopeID, genID string) {
		t.Helper()
		scopeID = prefix + ":" + label
		genID = prefix + ":gen-" + label
		// ingestion scope
		if _, err := sqlDB.ExecContext(ctx, `
			INSERT INTO ingestion_scopes (scope_id,scope_kind,source_system,source_key,collector_kind,partition_key,observed_at,ingested_at,status,active_generation_id,payload)
			VALUES ($1::text,'repository','git',$1::text,'git',$1::text,$2,$2,'active',$3::text,jsonb_build_object('repo_id',$1::text))
			ON CONFLICT (scope_id) DO NOTHING`, scopeID, now, genID); err != nil {
			t.Fatalf("%s: insert scope: %v", label, err)
		}
		if _, err := sqlDB.ExecContext(ctx, `
			INSERT INTO scope_generations (generation_id,scope_id,trigger_kind,observed_at,ingested_at,status,activated_at)
			VALUES ($1,$2,'manual',$3,$3,'active',$3)
			ON CONFLICT (generation_id) DO NOTHING`, genID, scopeID, now); err != nil {
			t.Fatalf("%s: insert generation: %v", label, err)
		}
		// projection_state with docCount ready
		if _, err := sqlDB.ExecContext(ctx, `
			INSERT INTO eshu_search_document_projection_state (scope_id,generation_id,projection_revision,build_fence,state,document_count,updated_at)
			VALUES ($1,$2,1,1,'ready',$3,$4)
			ON CONFLICT (scope_id,generation_id) DO NOTHING`,
			scopeID, genID, docCount, now); err != nil {
			t.Fatalf("%s: insert projection_state: %v", label, err)
		}
		return
	}
	insertFact := func(factID, scopeID, genID, docID, contentHash string, tombstone bool) {
		t.Helper()
		payload := fmt.Sprintf(`{"document_id":%q,"document":{"ID":%q},"content_hash":%q}`, docID, docID, contentHash)
		if _, err := sqlDB.ExecContext(ctx, `
			INSERT INTO fact_records (fact_id,scope_id,generation_id,fact_kind,stable_fact_key,source_system,source_fact_key,observed_at,ingested_at,is_tombstone,payload)
			VALUES ($1,$2,$3,$4,$5,'git',$5,$6,$6,$7,$8::jsonb)
			ON CONFLICT (fact_id) DO NOTHING`,
			factID, scopeID, genID, EshuSearchDocumentFactKind, factID, now, tombstone, payload); err != nil {
			t.Fatalf("insert fact %s: %v", factID, err)
		}
		if tombstone {
			return
		}
		if _, err := sqlDB.ExecContext(ctx, `
			INSERT INTO eshu_search_index_documents
			  (scope_id,generation_id,document_id,fact_id,repo_id,source_kind,content_hash,document,document_length,updated_at)
			VALUES ($1,$2,$3,$4,$1,'code_entity',$5,jsonb_build_object('ID',$3::text),1,$6)
			ON CONFLICT (scope_id,generation_id,document_id) DO NOTHING`,
			scopeID, genID, docID, factID, contentHash, now); err != nil {
			t.Fatalf("insert search index document %s: %v", factID, err)
		}
	}
	insertMeta := func(scopeID, genID, docID, contentHash, buildState string) {
		t.Helper()
		if _, err := sqlDB.ExecContext(ctx, `
			INSERT INTO eshu_search_vector_metadata (scope_id,generation_id,document_id,provider_profile_id,source_class,embedding_model_id,embedding_dimensions,embedding_content_hash,vector_index_version,build_state,created_at,updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,128,$7,$8,$9,$10,$10)
			ON CONFLICT ON CONSTRAINT eshu_search_vector_metadata_pkey DO NOTHING`,
			scopeID, genID, docID, providerProfileID, sourceClass, modelID, contentHash, vectorVersion, buildState, now); err != nil {
			t.Fatalf("insert metadata %s/%s: %v", scopeID, docID, err)
		}
	}
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
		if _, err := sqlDB.ExecContext(ctx, `
			INSERT INTO eshu_search_vector_values (scope_id,generation_id,document_id,provider_profile_id,source_class,embedding_model_id,embedding_dimensions,embedding_content_hash,vector_index_version,vector_values,created_at,updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,128,$7,$8,$9::double precision[],$10,$10)
			ON CONFLICT ON CONSTRAINT eshu_search_vector_values_pkey DO NOTHING`,
			scopeID, genID, docID, providerProfileID, sourceClass, modelID, contentHash, vectorVersion, vecLit, now); err != nil {
			t.Fatalf("insert value %s/%s: %v", scopeID, docID, err)
		}
	}

	// -- still-building: 3 docs, only 1 metadata (terminal_count < document_count) --
	sbScope, sbGen := setupScope("still-building", 3)
	insertFact(prefix+":sb-d1-fact", sbScope, sbGen, prefix+":sb-d1", "sb-h1", false)
	insertFact(prefix+":sb-d2-fact", sbScope, sbGen, prefix+":sb-d2", "sb-h2", false)
	insertFact(prefix+":sb-d3-fact", sbScope, sbGen, prefix+":sb-d3", "sb-h3", false)
	insertMeta(sbScope, sbGen, prefix+":sb-d1", "sb-h1", "ready")
	insertValue(sbScope, sbGen, prefix+":sb-d1", "sb-h1")

	// -- fully complete: 2 docs, both ready+value --
	fcScope, fcGen := setupScope("fully-complete", 2)
	insertFact(prefix+":fc-d1-fact", fcScope, fcGen, prefix+":fc-d1", "fc-h1", false)
	insertFact(prefix+":fc-d2-fact", fcScope, fcGen, prefix+":fc-d2", "fc-h2", false)
	insertMeta(fcScope, fcGen, prefix+":fc-d1", "fc-h1", "ready")
	insertValue(fcScope, fcGen, prefix+":fc-d1", "fc-h1")
	insertMeta(fcScope, fcGen, prefix+":fc-d2", "fc-h2", "disabled")

	// -- ready-without-value: metadata exists (state=ready) but no value row --
	rvScope, rvGen := setupScope("ready-no-value", 1)
	insertFact(prefix+":rv-d1-fact", rvScope, rvGen, prefix+":rv-d1", "rv-h1", false)
	insertMeta(rvScope, rvGen, prefix+":rv-d1", "rv-h1", "ready")
	// NO value row

	// -- stale-hash: metadata count == doc count, but hash mismatch --
	shScope, shGen := setupScope("stale-hash", 1)
	insertFact(prefix+":sh-d1-fact", shScope, shGen, prefix+":sh-d1", "sh-new", false)
	insertMeta(shScope, shGen, prefix+":sh-d1", "sh-old", "ready")
	insertValue(shScope, shGen, prefix+":sh-d1", "sh-old")

	// -- disabled-ok: metadata in disabled state with matching hash --
	doScope, doGen := setupScope("disabled-ok", 1)
	insertFact(prefix+":do-d1-fact", doScope, doGen, prefix+":do-d1", "do-h1", false)
	insertMeta(doScope, doGen, prefix+":do-d1", "do-h1", "disabled")

	// -- retired-extra: 2 metadata rows for 1 doc (terminal_count > document_count) --
	//    all rows match (complete), falls through to exact anti-join → true.
	reScope, reGen := setupScope("retired-extra", 1)
	insertFact(prefix+":re-d1-fact", reScope, reGen, prefix+":re-d1", "re-h1", false)
	insertMeta(reScope, reGen, prefix+":re-d1", "re-h1", "ready")
	insertValue(reScope, reGen, prefix+":re-d1", "re-h1")
	insertMeta(reScope, reGen, prefix+":re-d1-retired", "re-h1", "ready")
	insertValue(reScope, reGen, prefix+":re-d1-retired", "re-h1")

	t.Cleanup(func() {
		cleanCtx := context.Background()
		for _, sid := range []string{sbScope, fcScope, rvScope, shScope, doScope, reScope} {
			_, _ = sqlDB.ExecContext(cleanCtx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, sid)
		}
	})

	cases := []struct {
		label   string
		scopeID string
		genID   string
		want    bool
	}{
		{"still-building", sbScope, sbGen, false},
		{"fully-complete", fcScope, fcGen, true},
		{"ready-no-value", rvScope, rvGen, false},
		{"stale-hash", shScope, shGen, false},
		{"disabled-ok", doScope, doGen, true},
		{"retired-extra", reScope, reGen, true},
	}
	for _, c := range cases {
		complete, err := store.ScopeVectorComplete(ctx, c.scopeID, c.genID, identity)
		if err != nil {
			t.Fatalf("%s: ScopeVectorComplete error: %v", c.label, err)
		}
		if complete != c.want {
			t.Errorf("%s: ScopeVectorComplete = %v, want %v", c.label, complete, c.want)
		}
		var referenceComplete bool
		if err := sqlDB.QueryRowContext(
			ctx,
			scopeVectorCompleteFactReferenceSQL,
			c.scopeID,
			c.genID,
			EshuSearchDocumentFactKind,
			providerProfileID,
			sourceClass,
			modelID,
			vectorVersion,
		).Scan(&referenceComplete); err != nil {
			t.Fatalf("%s: fact reference completeness: %v", c.label, err)
		}
		if complete != referenceComplete {
			t.Errorf("%s: projected-document completeness = %v, fact reference = %v", c.label, complete, referenceComplete)
		}
	}

	// EXPLAIN proof: still-building scope's count gate rejects; indexed pending
	// probe never executes.
	explainSQL := "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) " + scopeVectorCompleteSQL
	rows, err := sqlDB.QueryContext(ctx, explainSQL,
		sbScope, sbGen, providerProfileID, sourceClass, modelID, vectorVersion)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var planLines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan EXPLAIN: %v", err)
		}
		planLines = append(planLines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate EXPLAIN: %v", err)
	}
	plan := strings.Join(planLines, "\n")
	t.Logf("count-gate EXPLAIN:\n%s", plan)

	if !strings.Contains(plan, "never executed") {
		t.Errorf("count-gate EXPLAIN: expected 'never executed' for exact anti-join subplan, plan:\n%s", plan)
	}
	// On small test datasets the planner may Seq Scan; on production-scale
	// data the existing eshu_search_vector_metadata_model_v2_idx is used.
	if !strings.Contains(plan, "eshu_search_vector_metadata") {
		t.Errorf("count-gate EXPLAIN: expected eshu_search_vector_metadata in plan, plan:\n%s", plan)
	}
}
