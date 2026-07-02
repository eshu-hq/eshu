// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/lib/pq"
)

// TestEshuSearchVectorUpsertBatchScaleLive is the before/after proof for
// #4430. It measures wall time for the same set of vector metadata+value rows
// written two ways against a live Postgres database:
//
//  1. "per_document" — the pre-#4430 code path: one Upsert round trip per
//     document (Values.Upsert then Metadata.Upsert), replicating what
//     searchvector.Builder did before batching.
//  2. "batched" — the #4430 code path: UpsertBatch, issuing one multi-row
//     INSERT ... ON CONFLICT statement per 500-row page.
//
// Both paths write into disjoint scopes so they do not share cache-warm
// advantage, and both are measured back-to-back in the same run against the
// same database so environment noise affects both equally.
//
// Correctness is proven alongside the timing by querying the raw columns
// directly (not via ValueStore.ListActive/MetadataStore.ListActive, which
// have an unrelated, pre-existing, out-of-scope-for-#4430 scan bug against
// the pgx stdlib driver: they scan vector_values without pq.Array, which
// fails outside the sqlmock-backed unit tests). This proves the batched path
// persists the same row count and content per document as the per-document
// path would have.
//
// Set ESHU_SEARCH_VECTOR_UPSERT_BATCH_SCALE_LIVE=1 and ESHU_POSTGRES_DSN to a
// live Postgres DSN to run this proof. The test is skipped when either env
// var is absent so the normal CI gate is unaffected. Scale defaults to the
// #4430 issue evidence (33 scopes x ~5800 docs ~= 191,400 rows); override
// with ESHU_SEARCH_VECTOR_UPSERT_BATCH_SCALE_SCOPES and
// ESHU_SEARCH_VECTOR_UPSERT_BATCH_SCALE_DOCS_PER_SCOPE for a faster local
// smoke run.
func TestEshuSearchVectorUpsertBatchScaleLive(t *testing.T) {
	if os.Getenv("ESHU_SEARCH_VECTOR_UPSERT_BATCH_SCALE_LIVE") != "1" {
		t.Skip("set ESHU_SEARCH_VECTOR_UPSERT_BATCH_SCALE_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}

	scopeCount := envInt(t, "ESHU_SEARCH_VECTOR_UPSERT_BATCH_SCALE_SCOPES", 33)
	docsPerScope := envInt(t, "ESHU_SEARCH_VECTOR_UPSERT_BATCH_SCALE_DOCS_PER_SCOPE", 5800)

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	db := SQLDB{DB: sqlDB}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	if err := ApplyBootstrap(ctx, db); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	prefix := fmt.Sprintf("4430-scale-%d", time.Now().UnixNano())
	providerProfileID := "semantic-search-default"
	sourceClass := "search_documents"
	modelID := "local-hash-v1"
	vectorVersion := "vector-v1"
	dims := 8
	now := time.Now().UTC()

	var seededScopeIDs []string
	t.Cleanup(func() {
		cleanCtx := context.Background()
		for _, scopeID := range seededScopeIDs {
			_, _ = sqlDB.ExecContext(cleanCtx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
		}
	})

	metadataStore := NewEshuSearchVectorMetadataStore(db)
	valueStore := NewEshuSearchVectorValueStore(db)

	seedScope := func(scopeSuffix string, docCount int) (scopeID, genID string, values []EshuSearchVectorValue, metas []EshuSearchVectorMetadata) {
		scopeID = fmt.Sprintf("%s:scope-%s", prefix, scopeSuffix)
		genID = fmt.Sprintf("%s:gen-%s", prefix, scopeSuffix)
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

		values = make([]EshuSearchVectorValue, docCount)
		metas = make([]EshuSearchVectorMetadata, docCount)
		rng := rand.New(rand.NewSource(42)) //nolint:gosec // deterministic synthetic fixture, not security-sensitive
		for i := 0; i < docCount; i++ {
			docID := fmt.Sprintf("doc-%d", i)
			vector := make([]float64, dims)
			for d := range vector {
				vector[d] = rng.Float64()
			}
			values[i] = EshuSearchVectorValue{
				ScopeID:              scopeID,
				GenerationID:         genID,
				DocumentID:           docID,
				ProviderProfileID:    providerProfileID,
				SourceClass:          sourceClass,
				EmbeddingModelID:     modelID,
				EmbeddingDimensions:  dims,
				EmbeddingContentHash: fmt.Sprintf("hash-%d", i),
				VectorIndexVersion:   vectorVersion,
				VectorValues:         vector,
				CreatedAt:            now,
				UpdatedAt:            now,
			}
			metas[i] = EshuSearchVectorMetadata{
				ScopeID:              scopeID,
				GenerationID:         genID,
				DocumentID:           docID,
				ProviderProfileID:    providerProfileID,
				SourceClass:          sourceClass,
				EmbeddingModelID:     modelID,
				EmbeddingDimensions:  dims,
				EmbeddingContentHash: fmt.Sprintf("hash-%d", i),
				VectorIndexVersion:   vectorVersion,
				BuildState:           EshuSearchVectorBuildStateReady,
				CreatedAt:            now,
				UpdatedAt:            now,
				LastSuccessAt:        &now,
			}
		}
		return scopeID, genID, values, metas
	}

	// --- Phase 1: "per_document" path replicates pre-#4430 Builder.Build,
	// which called Values.Upsert then Metadata.Upsert once per document. ---
	var perDocumentValues []EshuSearchVectorValue
	var perDocumentMetaRows []EshuSearchVectorMetadata
	seedStart := time.Now()
	for s := 0; s < scopeCount; s++ {
		scopeID, _, values, metas := seedScope(fmt.Sprintf("perdoc-%d", s), docsPerScope)
		seededScopeIDs = append(seededScopeIDs, scopeID)
		perDocumentValues = append(perDocumentValues, values...)
		perDocumentMetaRows = append(perDocumentMetaRows, metas...)
	}
	t.Logf("seed (excluded from measured write time): %v for %d scopes x %d docs", time.Since(seedStart), scopeCount, docsPerScope)

	writeStart := time.Now()
	for i := range perDocumentValues {
		if err := valueStore.Upsert(ctx, perDocumentValues[i]); err != nil {
			t.Fatalf("per-document value upsert %d: %v", i, err)
		}
		if err := metadataStore.Upsert(ctx, perDocumentMetaRows[i]); err != nil {
			t.Fatalf("per-document metadata upsert %d: %v", i, err)
		}
	}
	perDocumentDuration := time.Since(writeStart)

	// --- Phase 2: "batched" path is the #4430 fix: UpsertBatch. ---
	var batchedValues []EshuSearchVectorValue
	var batchedMetaRows []EshuSearchVectorMetadata
	for s := 0; s < scopeCount; s++ {
		scopeID, _, values, metas := seedScope(fmt.Sprintf("batched-%d", s), docsPerScope)
		seededScopeIDs = append(seededScopeIDs, scopeID)
		batchedValues = append(batchedValues, values...)
		batchedMetaRows = append(batchedMetaRows, metas...)
	}

	batchStart := time.Now()
	if err := valueStore.UpsertBatch(ctx, batchedValues); err != nil {
		t.Fatalf("batched value upsert: %v", err)
	}
	if err := metadataStore.UpsertBatch(ctx, batchedMetaRows); err != nil {
		t.Fatalf("batched metadata upsert: %v", err)
	}
	batchedDuration := time.Since(batchStart)

	totalDocs := scopeCount * docsPerScope
	t.Logf(
		"#4430 before/after: per_document=%v (%d rows, %.3fms/row) batched=%v (%d rows, %.3fms/row) speedup=%.1fx",
		perDocumentDuration, totalDocs, float64(perDocumentDuration.Milliseconds())/float64(totalDocs),
		batchedDuration, totalDocs, float64(batchedDuration.Milliseconds())/float64(totalDocs),
		float64(perDocumentDuration)/float64(batchedDuration),
	)

	if batchedDuration >= perDocumentDuration {
		t.Fatalf(
			"batched upsert (%v) did not beat per-document upsert (%v) for %d rows; #4430 fix did not isolate the sweep cost",
			batchedDuration, perDocumentDuration, totalDocs,
		)
	}

	// --- Correctness: batched writes must persist the same row count and
	// content as the per-document path would have. Queries the raw columns
	// directly (see doc comment: ListActive has an unrelated pgx scan bug). ---
	var (
		scannedDocumentID string
		scannedDimensions int
		scannedVector     []float64
	)
	if err := sqlDB.QueryRowContext(
		ctx, `
		SELECT document_id, embedding_dimensions, vector_values
		FROM eshu_search_vector_values
		WHERE scope_id = $1 AND document_id = 'doc-0'
		  AND provider_profile_id = $2 AND source_class = $3
		  AND embedding_model_id = $4 AND vector_index_version = $5`,
		batchedValues[0].ScopeID, providerProfileID, sourceClass, modelID, vectorVersion,
	).Scan(&scannedDocumentID, &scannedDimensions, pq.Array(&scannedVector)); err != nil {
		t.Fatalf("query batched value row: %v", err)
	}
	if scannedDocumentID != "doc-0" {
		t.Fatalf("batched value document id = %q, want doc-0", scannedDocumentID)
	}
	if scannedDimensions != dims {
		t.Fatalf("batched value dims = %d, want %d", scannedDimensions, dims)
	}
	if len(scannedVector) != dims {
		t.Fatalf("batched value scanned vector length = %d, want %d", len(scannedVector), dims)
	}

	var (
		count      int
		buildState string
	)
	if err := sqlDB.QueryRowContext(
		ctx, `
		SELECT COUNT(*), MIN(build_state)
		FROM eshu_search_vector_metadata
		WHERE scope_id = $1 AND provider_profile_id = $2 AND source_class = $3
		  AND embedding_model_id = $4 AND vector_index_version = $5
		  AND build_state = 'ready'`,
		batchedValues[0].ScopeID, providerProfileID, sourceClass, modelID, vectorVersion,
	).Scan(&count, &buildState); err != nil {
		t.Fatalf("query batched metadata rows: %v", err)
	}
	if count != docsPerScope {
		t.Fatalf("batched metadata ready row count = %d, want %d", count, docsPerScope)
	}
	if buildState != string(EshuSearchVectorBuildStateReady) {
		t.Fatalf("batched metadata build state = %q, want ready", buildState)
	}
}

func envInt(t *testing.T, key string, fallback int) int {
	t.Helper()
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("invalid %s=%q: %v", key, raw, err)
	}
	return value
}
