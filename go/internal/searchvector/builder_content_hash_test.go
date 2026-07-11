// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchvector

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuilderPersistsTheProjectedDocumentContentHash proves the vector sidecar
// uses the persisted projection token that its pending selector compares,
// rather than recomputing a different hash after JSON normalization.
func TestBuilderPersistsTheProjectedDocumentContentHash(t *testing.T) {
	t.Parallel()

	doc := searchDocument("doc-invalid-utf8", "repo-1", "Invalid UTF-8", "invalid.js")
	doc.ContextText = "prefix\xffsuffix"
	const projectedContentHash = "projected-before-json-normalization"
	docs := &recordingDocumentStore{rows: []postgres.EshuSearchDocumentRow{{
		ScopeID:      "repo-1",
		GenerationID: "gen-active",
		ContentHash:  projectedContentHash,
		Document:     doc,
	}}}
	values := &recordingVectorValueStore{}
	metadata := &recordingVectorMetadataStore{}
	embedder := &recordingEmbedder{dims: 2, vectors: map[string][]float64{
		searchhybrid.DocumentText(doc): {1, 0},
	}}

	_, err := (Builder{Documents: docs, Metadata: metadata, Values: values, Embedder: embedder}).Build(
		context.Background(),
		BuildRequest{
			ScopeID:            "repo-1",
			ProjectionRevision: 7,
			BuildFence:         9,
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
		},
	)
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	if got := values.rows[0].EmbeddingContentHash; got != projectedContentHash {
		t.Fatalf("value content hash = %q, want persisted projection hash %q", got, projectedContentHash)
	}
	if got := metadata.rows[0].EmbeddingContentHash; got != projectedContentHash {
		t.Fatalf("metadata content hash = %q, want persisted projection hash %q", got, projectedContentHash)
	}
	if got := values.rows[0].ProjectionRevision; got != 7 {
		t.Fatalf("value projection revision = %d, want 7", got)
	}
	if got := values.rows[0].BuildFence; got != 9 {
		t.Fatalf("value build fence = %d, want 9", got)
	}
	if got := metadata.rows[0].ProjectionRevision; got != 7 {
		t.Fatalf("metadata projection revision = %d, want 7", got)
	}
	if got := metadata.rows[0].BuildFence; got != 9 {
		t.Fatalf("metadata build fence = %d, want 9", got)
	}
}
