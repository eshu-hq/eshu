// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestEshuSearchVectorValueListActivePGXLive proves that the production pgx
// driver decodes the persisted float8[] payload through the public store path.
// It is read-only and skips unless explicitly enabled against retained data.
func TestEshuSearchVectorValueListActivePGXLive(t *testing.T) {
	if os.Getenv("ESHU_SEARCH_VECTOR_VALUES_SCAN_LIVE") != "1" {
		t.Skip("set ESHU_SEARCH_VECTOR_VALUES_SCAN_LIVE=1 and ESHU_POSTGRES_DSN to run")
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var filter EshuSearchVectorValueFilter
	var documentID string
	err = sqlDB.QueryRowContext(ctx, `
SELECT vec.scope_id,
       vec.provider_profile_id,
       vec.source_class,
       vec.embedding_model_id,
       vec.vector_index_version,
       vec.document_id
FROM eshu_search_vector_values vec
JOIN ingestion_scopes scope
  ON scope.scope_id = vec.scope_id
 AND scope.active_generation_id = vec.generation_id
JOIN eshu_search_vector_metadata meta
  ON meta.scope_id = vec.scope_id
 AND meta.generation_id = vec.generation_id
 AND meta.document_id = vec.document_id
 AND meta.provider_profile_id = vec.provider_profile_id
 AND meta.source_class = vec.source_class
 AND meta.embedding_model_id = vec.embedding_model_id
 AND meta.vector_index_version = vec.vector_index_version
 AND meta.embedding_content_hash = vec.embedding_content_hash
 AND meta.build_state = 'ready'
LIMIT 1
`).Scan(
		&filter.ScopeID,
		&filter.ProviderProfileID,
		&filter.SourceClass,
		&filter.EmbeddingModelID,
		&filter.VectorIndexVersion,
		&documentID,
	)
	if err != nil {
		t.Fatalf("select retained active vector identity: %v", err)
	}
	filter.DocumentIDs = []string{documentID}
	filter.Limit = 1

	rows, err := NewEshuSearchVectorValueStore(SQLDB{DB: sqlDB}).ListActive(ctx, filter)
	if err != nil {
		t.Fatalf("ListActive() pgx float8[] scan error = %v", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("ListActive() rows = %d, want %d", got, want)
	}
	if got, want := len(rows[0].VectorValues), rows[0].EmbeddingDimensions; got != want {
		t.Fatalf("vector dimensions = %d, want %d", got, want)
	}
}
