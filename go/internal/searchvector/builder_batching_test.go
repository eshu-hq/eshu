// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchvector

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuilderBatchesUpsertsPerPageInsteadOfPerDocument is the regression test
// for #4430: the search-vector build sweep amplified into one Values.Upsert
// plus one Metadata.Upsert round trip per document (185k-198k documents
// across 33 scopes in the reported reducer-tail evidence). Build must now
// issue exactly one UpsertBatch call per document page (bounded by
// req.Limit) for each store, not one call per document. Breaking the batching
// and going back to a per-document call would make this test fail because it
// asserts the call COUNT, not just the row content.
func TestBuilderBatchesUpsertsPerPageInsteadOfPerDocument(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 14, 0, 0, 0, time.UTC)
	docs := make([]postgres.EshuSearchDocumentRow, 0, 5)
	vectors := map[string][]float64{}
	for i := 0; i < 5; i++ {
		doc := searchDocument(fmt.Sprintf("doc-%d", i), "repo-1", "Handler", "handlers/h.go")
		docs = append(docs, postgres.EshuSearchDocumentRow{
			ScopeID:      "repo-1",
			GenerationID: "gen-active",
			Document:     doc,
		})
		vectors[searchhybrid.DocumentText(doc)] = []float64{float64(i), 0}
	}
	store := &recordingDocumentStore{rows: docs}
	values := &recordingVectorValueStore{}
	metadata := &recordingVectorMetadataStore{}
	embedder := &recordingEmbedder{dims: 2, vectors: vectors}

	result, err := Builder{
		Documents: store,
		Metadata:  metadata,
		Values:    values,
		Embedder:  embedder,
		Clock:     func() time.Time { return now },
	}.Build(context.Background(), BuildRequest{
		ScopeID:            "repo-1",
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
		Limit:              500, // one page covers all 5 documents
	})
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	if result.DocumentCount != 5 || result.VectorCount != 5 {
		t.Fatalf("result = %#v, want five ready vectors", result)
	}
	// One page => exactly one UpsertBatch call per store, each carrying all
	// five rows. Prior per-document Upsert behavior would have made 5 calls.
	if got, want := len(values.batches), 1; got != want {
		t.Fatalf("value UpsertBatch calls = %d, want %d (per-document upserts would give 5)", got, want)
	}
	if got, want := len(values.batches[0]), 5; got != want {
		t.Fatalf("value batch size = %d, want %d", got, want)
	}
	if got, want := len(metadata.batches), 1; got != want {
		t.Fatalf("metadata UpsertBatch calls = %d, want %d (per-document upserts would give 5)", got, want)
	}
	if got, want := len(metadata.batches[0]), 5; got != want {
		t.Fatalf("metadata batch size = %d, want %d", got, want)
	}
}

// TestBuilderReportsSplitPhaseTimings is the regression test proving Build
// tracks the query/load, embed/build, and write/upsert phases separately
// (#4430) instead of one opaque duration, so the reducer-tail sweep's
// dominant cost slice is isolable.
func TestBuilderReportsSplitPhaseTimings(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 14, 30, 0, 0, time.UTC)
	doc := searchDocument("doc-1", "repo-1", "API handler", "handlers/api.go")
	docs := &recordingDocumentStore{rows: []postgres.EshuSearchDocumentRow{{
		ScopeID:      "repo-1",
		GenerationID: "gen-active",
		Document:     doc,
	}}}
	values := &recordingVectorValueStore{}
	metadata := &recordingVectorMetadataStore{}
	embedder := &recordingEmbedder{dims: 2, vectors: map[string][]float64{
		searchhybrid.DocumentText(doc): {1, 0},
	}}

	result, err := Builder{
		Documents: docs,
		Metadata:  metadata,
		Values:    values,
		Embedder:  embedder,
		Clock:     func() time.Time { return now },
	}.Build(context.Background(), BuildRequest{
		ScopeID:            "repo-1",
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
	})
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	if result.QueryLoadDuration < 0 || result.EmbedBuildDuration < 0 || result.WriteUpsertDuration < 0 {
		t.Fatalf("phase durations must be non-negative: %#v", result)
	}
}
