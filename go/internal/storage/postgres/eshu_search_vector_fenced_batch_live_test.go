// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/lib/pq"
)

// TestEshuSearchVectorFencedBatchRejectsStaleWorkerLive proves that vector
// value and metadata upserts accept the current build owner while rejecting a
// stale worker after the scope fence advances. Set
// ESHU_SEARCH_VECTOR_SCOPE_STATE_LIVE=1 and ESHU_POSTGRES_DSN to run.
func TestEshuSearchVectorFencedBatchRejectsStaleWorkerLive(t *testing.T) {
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
	if err := ApplyBootstrap(ctx, db); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	prefix := fmt.Sprintf("4233-fence-%d", time.Now().UnixNano())
	scopeID := prefix + ":scope"
	generationID := prefix + ":generation"
	documentID := prefix + ":document"
	now := time.Now().UTC()
	const (
		providerProfileID = "semantic-search-default"
		sourceClass       = "search_documents"
		modelID           = "local-hash-v1"
		vectorVersion     = "vector-v1"
		contentHash       = "current-content-hash"
	)

	seedStatements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO ingestion_scopes
  (scope_id,scope_kind,source_system,source_key,collector_kind,partition_key,
   observed_at,ingested_at,status,active_generation_id,payload)
VALUES ($1,'repository','git',$1,'git',$1,$2,$2,'active',$3,
        jsonb_build_object('repo_id',$1::text))`, []any{scopeID, now, generationID}},
		{`INSERT INTO scope_generations
  (generation_id,scope_id,trigger_kind,observed_at,ingested_at,status,activated_at)
VALUES ($1,$2,'manual',$3,$3,'active',$3)`, []any{generationID, scopeID, now}},
		{
			`INSERT INTO eshu_search_index_documents
  (scope_id,generation_id,document_id,fact_id,repo_id,source_kind,content_hash,
   document,document_length,updated_at)
VALUES ($1,$2,$3,$4,$1,'code_entity',$5,
        jsonb_build_object('ID',$3::text),1,$6)`,
			[]any{scopeID, generationID, documentID, prefix + ":fact", contentHash, now},
		},
		{`INSERT INTO eshu_search_document_projection_state
  (scope_id,generation_id,projection_revision,build_fence,state,document_count,updated_at)
VALUES ($1,$2,2,2,'ready',1,$3)`, []any{scopeID, generationID, now}},
		{
			`INSERT INTO eshu_search_vector_scope_state
  (scope_id,generation_id,provider_profile_id,source_class,embedding_model_id,
   vector_index_version,projection_revision,build_fence,state,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,2,2,'building',$7)`,
			[]any{scopeID, generationID, providerProfileID, sourceClass, modelID, vectorVersion, now},
		},
	}
	for i, statement := range seedStatements {
		if _, err := sqlDB.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed fenced vector scope statement %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		_, _ = sqlDB.ExecContext(context.Background(),
			`DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
	})

	valueStore := NewEshuSearchVectorValueStore(db)
	metadataStore := NewEshuSearchVectorMetadataStore(db)
	write := func(revision, fence int64, vector []float64, failureClass string, updatedAt time.Time) {
		t.Helper()
		value := EshuSearchVectorValue{
			ScopeID: scopeID, GenerationID: generationID, DocumentID: documentID,
			ProviderProfileID: providerProfileID, SourceClass: sourceClass,
			EmbeddingModelID: modelID, EmbeddingDimensions: len(vector),
			EmbeddingContentHash: contentHash, VectorIndexVersion: vectorVersion,
			VectorValues: vector, CreatedAt: now, UpdatedAt: updatedAt,
			ProjectionRevision: revision, BuildFence: fence,
		}
		metadata := EshuSearchVectorMetadata{
			ScopeID: scopeID, GenerationID: generationID, DocumentID: documentID,
			ProviderProfileID: providerProfileID, SourceClass: sourceClass,
			EmbeddingModelID: modelID, EmbeddingDimensions: len(vector),
			EmbeddingContentHash: contentHash, VectorIndexVersion: vectorVersion,
			BuildState: EshuSearchVectorBuildStateReady, FailureClass: failureClass,
			CreatedAt: now, UpdatedAt: updatedAt, LastSuccessAt: &updatedAt,
			ProjectionRevision: revision, BuildFence: fence,
		}
		if err := valueStore.UpsertBatch(ctx, []EshuSearchVectorValue{value}); err != nil {
			t.Fatalf("value UpsertBatch revision=%d fence=%d: %v", revision, fence, err)
		}
		if err := metadataStore.UpsertBatch(ctx, []EshuSearchVectorMetadata{metadata}); err != nil {
			t.Fatalf("metadata UpsertBatch revision=%d fence=%d: %v", revision, fence, err)
		}
	}

	currentUpdatedAt := now.Add(time.Second)
	write(2, 2, []float64{1, 0}, "", currentUpdatedAt)
	assertFencedVectorRowLive(t, ctx, sqlDB, scopeID, generationID, documentID,
		[]float64{1, 0}, "", currentUpdatedAt)

	if _, err := sqlDB.ExecContext(ctx, `
UPDATE eshu_search_vector_scope_state
SET build_fence = 3, updated_at = $1
WHERE scope_id = $2 AND generation_id = $3`, now.Add(2*time.Second), scopeID, generationID); err != nil {
		t.Fatalf("advance build fence: %v", err)
	}
	staleUpdatedAt := now.Add(3 * time.Second)
	write(2, 2, []float64{0, 1}, "stale-worker", staleUpdatedAt)
	assertFencedVectorRowLive(t, ctx, sqlDB, scopeID, generationID, documentID,
		[]float64{1, 0}, "", currentUpdatedAt)
}

func assertFencedVectorRowLive(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
	documentID string,
	wantVector []float64,
	wantFailureClass string,
	wantUpdatedAt time.Time,
) {
	t.Helper()
	var vector []float64
	var valueUpdatedAt time.Time
	if err := db.QueryRowContext(ctx, `
SELECT vector_values, updated_at
FROM eshu_search_vector_values
WHERE scope_id=$1 AND generation_id=$2 AND document_id=$3`,
		scopeID, generationID, documentID).Scan(pq.Array(&vector), &valueUpdatedAt); err != nil {
		t.Fatalf("read vector value: %v", err)
	}
	if fmt.Sprint(vector) != fmt.Sprint(wantVector) || !valueUpdatedAt.Equal(wantUpdatedAt) {
		t.Fatalf("vector row = %v at %s, want %v at %s",
			vector, valueUpdatedAt, wantVector, wantUpdatedAt)
	}
	var failureClass string
	var metadataUpdatedAt time.Time
	if err := db.QueryRowContext(ctx, `
SELECT COALESCE(failure_class,''), updated_at
FROM eshu_search_vector_metadata
WHERE scope_id=$1 AND generation_id=$2 AND document_id=$3`,
		scopeID, generationID, documentID).Scan(&failureClass, &metadataUpdatedAt); err != nil {
		t.Fatalf("read vector metadata: %v", err)
	}
	if failureClass != wantFailureClass || !metadataUpdatedAt.Equal(wantUpdatedAt) {
		t.Fatalf("metadata row failure=%q at %s, want %q at %s",
			failureClass, metadataUpdatedAt, wantFailureClass, wantUpdatedAt)
	}
}
