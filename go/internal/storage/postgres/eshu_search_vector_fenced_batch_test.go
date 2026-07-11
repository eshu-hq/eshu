// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSearchVectorBatchUpsertsCarryProjectionAndBuildFences(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 20, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	valueStore := NewEshuSearchVectorValueStore(db)
	metadataStore := NewEshuSearchVectorMetadataStore(db)
	value := EshuSearchVectorValue{
		ScopeID: "scope-1", GenerationID: "gen-1", DocumentID: "doc-1",
		ProviderProfileID: "local", SourceClass: "search_documents",
		EmbeddingModelID: "local-hash-v1", EmbeddingDimensions: 2,
		EmbeddingContentHash: "hash-1", VectorIndexVersion: "vector-v1",
		VectorValues: []float64{1, 0}, CreatedAt: now, UpdatedAt: now,
		ProjectionRevision: 7, BuildFence: 9,
	}
	metadata := EshuSearchVectorMetadata{
		ScopeID: "scope-1", GenerationID: "gen-1", DocumentID: "doc-1",
		ProviderProfileID: "local", SourceClass: "search_documents",
		EmbeddingModelID: "local-hash-v1", EmbeddingDimensions: 2,
		EmbeddingContentHash: "hash-1", VectorIndexVersion: "vector-v1",
		BuildState: EshuSearchVectorBuildStateReady, CreatedAt: now, UpdatedAt: now,
		ProjectionRevision: 7, BuildFence: 9,
	}

	if err := valueStore.UpsertBatch(context.Background(), []EshuSearchVectorValue{value}); err != nil {
		t.Fatalf("value UpsertBatch error = %v", err)
	}
	if err := metadataStore.UpsertBatch(context.Background(), []EshuSearchVectorMetadata{metadata}); err != nil {
		t.Fatalf("metadata UpsertBatch error = %v", err)
	}
	if len(db.execs) != 2 {
		t.Fatalf("execs = %d, want 2", len(db.execs))
	}
	for i, exec := range db.execs {
		for _, want := range []string{
			"projection.state = 'ready'",
			"projection.projection_revision = row.projection_revision",
			"vector_scope.build_fence = row.build_fence",
			"vector_scope.state = 'building'",
			"document.content_hash = row.embedding_content_hash",
			"scope.active_generation_id = row.generation_id",
		} {
			if !strings.Contains(exec.query, want) {
				t.Fatalf("exec %d missing fence %q:\n%s", i, want, exec.query)
			}
		}
		if got := exec.args[len(exec.args)-2]; got != int64(7) {
			t.Fatalf("exec %d projection revision = %v, want 7", i, got)
		}
		if got := exec.args[len(exec.args)-1]; got != int64(9) {
			t.Fatalf("exec %d build fence = %v, want 9", i, got)
		}
	}
}

func TestSearchVectorBatchUpsertsRejectPartialFences(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	db := &fakeExecQueryer{}
	err := NewEshuSearchVectorValueStore(db).UpsertBatch(context.Background(), []EshuSearchVectorValue{{
		ScopeID: "scope-1", GenerationID: "gen-1", DocumentID: "doc-1",
		ProviderProfileID: "local", SourceClass: "search_documents",
		EmbeddingModelID: "local-hash-v1", EmbeddingDimensions: 1,
		EmbeddingContentHash: "hash-1", VectorIndexVersion: "vector-v1",
		VectorValues: []float64{1}, CreatedAt: now, UpdatedAt: now,
		ProjectionRevision: 7,
	}})
	if err == nil || !strings.Contains(err.Error(), "projection revision and build fence together") {
		t.Fatalf("UpsertBatch error = %v, want partial-fence rejection", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("execs = %d, want 0", len(db.execs))
	}
}
