// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type searchVectorQueryOnly struct{ queryer db.Queryer }

func (q searchVectorQueryOnly) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return q.queryer.QueryContext(ctx, query, args...)
}

func TestEshuSearchVectorQueryOnlyReaders(t *testing.T) {
	t.Parallel()

	metadataDB := &fakeExecQueryer{queryResponses: []queueFakeRows{{}, {}}}
	metadata := NewEshuSearchVectorMetadataReader(searchVectorQueryOnly{metadataDB})
	if _, writable := any(metadata).(db.Executor); writable {
		t.Fatal("metadata reader exposes ExecContext")
	}
	metadataFilter := EshuSearchVectorMetadataFilter{
		ScopeID: "repo-1", ProviderProfileID: "local", SourceClass: "search_documents",
		EmbeddingModelID: "local-hash-v1", VectorIndexVersion: "vector-v1", Limit: 10,
	}
	if _, err := metadata.ListActive(context.Background(), metadataFilter); err != nil {
		t.Fatalf("metadata ListActive: %v", err)
	}
	if _, err := metadata.Status(context.Background(), EshuSearchVectorStatusRequest{
		ScopeID: "repo-1", ProviderProfileID: "local", SourceClass: "search_documents",
		EmbeddingModelID: "local-hash-v1", VectorIndexVersion: "vector-v1",
	}); err != nil {
		t.Fatalf("metadata Status: %v", err)
	}
	if got := len(metadataDB.queries); got != 2 {
		t.Fatalf("metadata queries = %d, want 2", got)
	}
	if got := metadataDB.queries[0].query; got != listActiveEshuSearchVectorMetadataSQL {
		t.Fatal("metadata reader changed ListActive SQL")
	}
	if got := metadataDB.queries[1].query; got != eshuSearchVectorStatusSQL {
		t.Fatal("metadata reader changed Status SQL")
	}
	if got := len(metadataDB.execs); got != 0 {
		t.Fatalf("metadata writes = %d, want 0", got)
	}

	valueDB := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	values := NewEshuSearchVectorValueReader(searchVectorQueryOnly{valueDB})
	if _, writable := any(values).(db.Executor); writable {
		t.Fatal("value reader exposes ExecContext")
	}
	if _, err := values.ListActive(context.Background(), EshuSearchVectorValueFilter{
		ScopeID: "repo-1", ProviderProfileID: "local", SourceClass: "search_documents",
		EmbeddingModelID: "local-hash-v1", VectorIndexVersion: "vector-v1", Limit: 10,
	}); err != nil {
		t.Fatalf("value ListActive: %v", err)
	}
	if got := len(valueDB.queries); got != 1 {
		t.Fatalf("value queries = %d, want 1", got)
	}
	if got := valueDB.queries[0].query; got != listActiveEshuSearchVectorValuesSQL {
		t.Fatal("value reader changed ListActive SQL")
	}
	if got := len(valueDB.execs); got != 0 {
		t.Fatalf("value writes = %d, want 0", got)
	}
}
