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

// TestBuilderUsesPendingVectorDocumentsWhenAvailable guards the reducer-tail
// budget: production Postgres can identify the exact active documents whose
// vector rows are missing or stale, so the builder must not fall back to
// rewriting every active document in a large scope.
func TestBuilderUsesPendingVectorDocumentsWhenAvailable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	activeDocs := make([]postgres.EshuSearchDocumentRow, 0, 5)
	pendingDocs := make([]postgres.EshuSearchDocumentRow, 0, 2)
	vectors := map[string][]float64{}
	for i := 0; i < 5; i++ {
		doc := searchDocument(fmt.Sprintf("doc-%d", i), "repo-1", "Handler", fmt.Sprintf("handlers/%d.go", i))
		row := postgres.EshuSearchDocumentRow{
			ScopeID:      "repo-1",
			GenerationID: "gen-active",
			Document:     doc,
		}
		activeDocs = append(activeDocs, row)
		if i >= 3 {
			pendingDocs = append(pendingDocs, row)
		}
		vectors[searchhybrid.DocumentText(doc)] = []float64{float64(i), 0}
	}
	store := &recordingPendingDocumentStore{recordingDocumentStore: recordingDocumentStore{
		rows:        activeDocs,
		pendingRows: pendingDocs,
	}}
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
		Limit:              2,
	})
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	if got, want := result.DocumentCount, 2; got != want {
		t.Fatalf("result.DocumentCount = %d, want %d", got, want)
	}
	if got, want := len(store.pendingFilters), 1; got != want {
		t.Fatalf("ListPendingVectorDocuments calls = %d, want %d", got, want)
	}
	if got, want := len(store.filters), 0; got != want {
		t.Fatalf("ListActiveDocuments calls = %d, want %d", got, want)
	}
	if got, want := len(values.rows), 2; got != want {
		t.Fatalf("vector rows = %d, want %d", got, want)
	}
	if got, want := values.rows[0].DocumentID, "doc-3"; got != want {
		t.Fatalf("first vector document = %q, want %q", got, want)
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

// TestBuilderDedupesDuplicateDocumentIDsWithinOnePage is a P1 regression test
// found during #4430 self-review: ListActiveDocuments does not deduplicate by
// document ID, and two fact_records rows sharing one document.id within a
// scope/generation is an acknowledged data shape in this codebase (see the
// pending-scope query's "two facts share the same document_id" case). Before
// batching, two Upsert calls for the same document ID were harmless
// (sequential single-row upserts never conflict with themselves). After
// batching, a single multi-row INSERT ... ON CONFLICT DO UPDATE statement
// errors if it contains two rows with the same conflict key
// ("ON CONFLICT DO UPDATE command cannot affect row a second time",
// reproduced live against Postgres 16 during review). Build must dedupe each
// page's batch by document ID (last write wins, matching the pre-#4430
// sequential outcome) before writing, so a duplicate never turns into a
// sweep failure that repeats every poll interval.
func TestBuilderDedupesDuplicateDocumentIDsWithinOnePage(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 15, 0, 0, 0, time.UTC)
	// Two distinct fact_records rows share document ID "dup-doc" but carry
	// different titles/paths, modeling two facts pointing at the same curated
	// document identity within one scope/generation.
	first := searchDocument("dup-doc", "repo-1", "First fact", "handlers/first.go")
	second := searchDocument("dup-doc", "repo-1", "Second fact", "handlers/second.go")
	docs := &recordingDocumentStore{rows: []postgres.EshuSearchDocumentRow{
		{ScopeID: "repo-1", GenerationID: "gen-active", Document: first},
		{ScopeID: "repo-1", GenerationID: "gen-active", Document: second},
	}}
	values := &recordingVectorValueStore{}
	metadata := &recordingVectorMetadataStore{}
	embedder := &recordingEmbedder{dims: 2, vectors: map[string][]float64{
		searchhybrid.DocumentText(first):  {1, 0},
		searchhybrid.DocumentText(second): {0, 1},
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
		Limit:              500,
	})
	if err != nil {
		t.Fatalf("Build error = %v, want nil (duplicate document IDs must not fail the sweep)", err)
	}
	if result.DocumentCount != 2 {
		t.Fatalf("result.DocumentCount = %d, want 2 (both fact rows are counted)", result.DocumentCount)
	}
	// Exactly one row per store, keyed by document ID: the batch is deduped
	// before the single UpsertBatch call, so a naive re-introduction of the
	// duplicate (removing dedupeValueBatch/dedupeMetadataBatch) would send two
	// same-key rows in one batch and fail against a real Postgres ON CONFLICT
	// DO UPDATE statement (proven separately in
	// eshu_search_vector_upsert_batch_scale_live_test.go's underlying store).
	if got, want := len(values.batches), 1; got != want {
		t.Fatalf("value UpsertBatch calls = %d, want %d", got, want)
	}
	if got, want := len(values.batches[0]), 1; got != want {
		t.Fatalf("value batch size = %d, want %d (deduped to one row per document id)", got, want)
	}
	if got, want := len(metadata.batches[0]), 1; got != want {
		t.Fatalf("metadata batch size = %d, want %d (deduped to one row per document id)", got, want)
	}
	// Last write wins, matching the pre-#4430 sequential Upsert outcome: the
	// second fact's vector/hash survives.
	if got, want := values.batches[0][0].VectorValues, []float64{0, 1}; !sameVector(got, want) {
		t.Fatalf("surviving vector = %#v, want %#v (last write should win)", got, want)
	}
}
