// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchvector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestBuilderPersistsReadyVectorsForActiveDocuments(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	doc := searchDocument("doc-1", "repo-1", "API handler", "handlers/api.go")
	docs := &recordingDocumentStore{rows: []postgres.EshuSearchDocumentRow{{
		ScopeID:      "repo-1",
		GenerationID: "gen-active",
		Document:     doc,
	}}}
	values := &recordingVectorValueStore{}
	metadata := &recordingVectorMetadataStore{}
	embedder := &recordingEmbedder{dims: 3, vectors: map[string][]float64{
		searchhybrid.DocumentText(doc): {0.25, -0.5, 1},
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
		Limit:              50,
	})
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	if result.DocumentCount != 1 || result.VectorCount != 1 || result.FailedCount != 0 {
		t.Fatalf("result = %#v, want one ready vector", result)
	}
	if got, want := docs.filter.ScopeID, "repo-1"; got != want {
		t.Fatalf("document filter scope = %q, want %q", got, want)
	}
	if got, want := docs.filter.Limit, 50; got != want {
		t.Fatalf("document filter limit = %d, want %d", got, want)
	}
	if got, want := len(docs.filters), 1; got != want {
		t.Fatalf("document list calls = %d, want %d", got, want)
	}
	if len(values.rows) != 1 {
		t.Fatalf("value rows = %d, want 1", len(values.rows))
	}
	value := values.rows[0]
	if got, want := value.ProviderProfileID, "local"; got != want {
		t.Fatalf("value provider profile = %q, want %q", got, want)
	}
	if got, want := value.SourceClass, "search_documents"; got != want {
		t.Fatalf("value source class = %q, want %q", got, want)
	}
	if got, want := value.EmbeddingContentHash, searchhybrid.DocumentContentHash(doc); got != want {
		t.Fatalf("content hash = %q, want %q", got, want)
	}
	if got, want := value.VectorValues, []float64{0.25, -0.5, 1}; !sameVector(got, want) {
		t.Fatalf("vector = %#v, want %#v", got, want)
	}
	if got, want := len(metadata.rows), 1; got != want {
		t.Fatalf("metadata rows = %d, want %d", got, want)
	}
	meta := metadata.rows[0]
	if got, want := meta.ProviderProfileID, "local"; got != want {
		t.Fatalf("metadata provider profile = %q, want %q", got, want)
	}
	if got, want := meta.SourceClass, "search_documents"; got != want {
		t.Fatalf("metadata source class = %q, want %q", got, want)
	}
	if meta.BuildState != postgres.EshuSearchVectorBuildStateReady {
		t.Fatalf("metadata state = %q, want ready", meta.BuildState)
	}
	if meta.LastSuccessAt == nil || !meta.LastSuccessAt.Equal(now) {
		t.Fatalf("last success = %v, want %v", meta.LastSuccessAt, now)
	}
	if embedder.calls[0] != searchhybrid.DocumentText(doc) {
		t.Fatalf("embedded text = %q, want document text", embedder.calls[0])
	}
}

func TestBuilderPagesThroughAllActiveDocuments(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 12, 45, 0, 0, time.UTC)
	first := searchDocument("doc-1", "repo-1", "API handler", "handlers/api.go")
	second := searchDocument("doc-2", "repo-1", "Reducer", "reducer.go")
	docs := &recordingDocumentStore{
		pages: [][]postgres.EshuSearchDocumentRow{
			{{
				ScopeID:      "repo-1",
				GenerationID: "gen-active",
				Document:     first,
			}},
			{{
				ScopeID:      "repo-1",
				GenerationID: "gen-active",
				Document:     second,
			}},
			{},
		},
	}
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
		Limit:              1,
	})
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	if result.DocumentCount != 2 || result.VectorCount != 2 || result.FailedCount != 0 {
		t.Fatalf("result = %#v, want two ready vectors", result)
	}
	if got, want := len(docs.filters), 3; got != want {
		t.Fatalf("document list calls = %d, want %d", got, want)
	}
	for i, wantOffset := range []int{0, 1, 2} {
		if got := docs.filters[i].Offset; got != wantOffset {
			t.Fatalf("call %d offset = %d, want %d", i, got, wantOffset)
		}
	}
	if got, want := len(values.rows), 2; got != want {
		t.Fatalf("value rows = %d, want %d", got, want)
	}
	if got, want := len(metadata.rows), 2; got != want {
		t.Fatalf("metadata rows = %d, want %d", got, want)
	}
}

func TestBuilderAnchorsPagedBuildToFirstGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 13, 0, 0, 0, time.UTC)
	firstOld := searchDocument("doc-old-1", "repo-1", "API handler", "handlers/api.go")
	secondOld := searchDocument("doc-old-2", "repo-1", "Reducer", "reducer.go")
	secondNew := searchDocument("doc-new-2", "repo-1", "Fresh reducer", "reducer.go")
	docs := &generationSwitchingDocumentStore{
		oldRows: []postgres.EshuSearchDocumentRow{
			{ScopeID: "repo-1", GenerationID: "gen-old", Document: firstOld},
			{ScopeID: "repo-1", GenerationID: "gen-old", Document: secondOld},
		},
		newRows: []postgres.EshuSearchDocumentRow{
			{ScopeID: "repo-1", GenerationID: "gen-new", Document: firstOld},
			{ScopeID: "repo-1", GenerationID: "gen-new", Document: secondNew},
		},
	}
	values := &recordingVectorValueStore{}
	metadata := &recordingVectorMetadataStore{}
	embedder := &recordingEmbedder{dims: 2, vectors: map[string][]float64{
		searchhybrid.DocumentText(firstOld):  {1, 0},
		searchhybrid.DocumentText(secondOld): {0, 1},
		searchhybrid.DocumentText(secondNew): {0.5, 0.5},
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
		Limit:              1,
	})
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	if result.DocumentCount != 2 || result.VectorCount != 2 || result.FailedCount != 0 {
		t.Fatalf("result = %#v, want two ready vectors from first generation", result)
	}
	if got, want := len(docs.filters), 3; got != want {
		t.Fatalf("document list calls = %d, want %d", got, want)
	}
	for i, filter := range docs.filters[1:] {
		if got, want := filter.GenerationID, "gen-old"; got != want {
			t.Fatalf("call %d generation anchor = %q, want %q", i+1, got, want)
		}
	}
	for _, row := range values.rows {
		if got, want := row.GenerationID, "gen-old"; got != want {
			t.Fatalf("value generation = %q, want %q", got, want)
		}
	}
}

func TestBuilderRecordsEmbeddingFailureAsBoundedMetadata(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 12, 15, 0, 0, time.UTC)
	doc := searchDocument("doc-fail", "repo-1", "Parser", "parser.go")
	docs := &recordingDocumentStore{rows: []postgres.EshuSearchDocumentRow{{
		ScopeID:      "repo-1",
		GenerationID: "gen-active",
		Document:     doc,
	}}}
	values := &recordingVectorValueStore{}
	metadata := &recordingVectorMetadataStore{}
	embedder := &recordingEmbedder{dims: 2, err: errors.New("model refused raw text")}

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
	if err == nil {
		t.Fatal("Build error = nil, want bounded failure")
	}
	if !strings.Contains(err.Error(), FailureClassEmbedder) {
		t.Fatalf("error = %v, want failure class %q", err, FailureClassEmbedder)
	}
	if result.DocumentCount != 1 || result.VectorCount != 0 || result.FailedCount != 1 {
		t.Fatalf("result = %#v, want one failed document", result)
	}
	if len(values.rows) != 0 {
		t.Fatalf("value rows = %d, want 0", len(values.rows))
	}
	if len(metadata.rows) != 1 {
		t.Fatalf("metadata rows = %d, want 1", len(metadata.rows))
	}
	meta := metadata.rows[0]
	if meta.BuildState != postgres.EshuSearchVectorBuildStateFailed {
		t.Fatalf("metadata state = %q, want failed", meta.BuildState)
	}
	if meta.FailureClass != FailureClassEmbedder {
		t.Fatalf("failure class = %q, want %q", meta.FailureClass, FailureClassEmbedder)
	}
	if meta.LastSuccessAt != nil {
		t.Fatalf("last success = %v, want nil", meta.LastSuccessAt)
	}
}

func TestBuilderMarksPolicyDeniedDocumentsDisabledWithoutEmbedding(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 12, 20, 0, 0, time.UTC)
	allowed := searchDocument("doc-allowed", "repo-1", "API handler", "handlers/api.go")
	denied := searchDocument("doc-denied", "repo-1", "Private model", "private/model.go")
	docs := &recordingDocumentStore{rows: []postgres.EshuSearchDocumentRow{
		{ScopeID: "repo-1", GenerationID: "gen-active", Document: allowed},
		{ScopeID: "repo-1", GenerationID: "gen-active", Document: denied},
	}}
	values := &recordingVectorValueStore{}
	metadata := &recordingVectorMetadataStore{}
	embedder := &recordingEmbedder{dims: 3, vectors: map[string][]float64{
		searchhybrid.DocumentText(allowed): {0.25, -0.5, 1},
	}}

	result, err := Builder{
		Documents: docs,
		Metadata:  metadata,
		Values:    values,
		Embedder:  embedder,
		Clock:     func() time.Time { return now },
		DocumentAllowed: func(row postgres.EshuSearchDocumentRow) bool {
			return row.Document.ID == allowed.ID
		},
	}.Build(context.Background(), BuildRequest{
		ScopeID:            "repo-1",
		ProviderProfileID:  "semantic-search-default",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "search-embed-v1",
		VectorIndexVersion: "vector-v1",
	})
	if err != nil {
		t.Fatalf("Build error = %v, want nil", err)
	}
	if result.DocumentCount != 2 || result.VectorCount != 1 || result.DisabledCount != 1 || result.FailedCount != 0 {
		t.Fatalf("result = %#v, want one ready vector and one policy-disabled document", result)
	}
	if got, want := len(embedder.calls), 1; got != want {
		t.Fatalf("embedder calls = %d, want %d", got, want)
	}
	if got, want := len(values.rows), 1; got != want {
		t.Fatalf("value rows = %d, want %d", got, want)
	}
	if got, want := len(metadata.rows), 2; got != want {
		t.Fatalf("metadata rows = %d, want %d", got, want)
	}
	disabled := metadata.rows[1]
	if disabled.DocumentID != denied.ID {
		t.Fatalf("disabled document = %q, want %q", disabled.DocumentID, denied.ID)
	}
	if disabled.BuildState != postgres.EshuSearchVectorBuildStateDisabled {
		t.Fatalf("disabled state = %q, want disabled", disabled.BuildState)
	}
	if disabled.FailureClass != FailureClassPolicyDenied {
		t.Fatalf("failure class = %q, want %q", disabled.FailureClass, FailureClassPolicyDenied)
	}
	if disabled.LastSuccessAt != nil {
		t.Fatalf("disabled last success = %v, want nil", disabled.LastSuccessAt)
	}
}

func TestBuilderRecordsInvalidVectorAsBoundedMetadata(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 12, 30, 0, 0, time.UTC)
	doc := searchDocument("doc-invalid", "repo-1", "Retriever", "retriever.go")
	docs := &recordingDocumentStore{rows: []postgres.EshuSearchDocumentRow{{
		ScopeID:      "repo-1",
		GenerationID: "gen-active",
		Document:     doc,
	}}}
	values := &recordingVectorValueStore{}
	metadata := &recordingVectorMetadataStore{}
	embedder := &recordingEmbedder{dims: 3, vectors: map[string][]float64{
		searchhybrid.DocumentText(doc): {0.25, 0.5},
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
	if err == nil {
		t.Fatal("Build error = nil, want invalid-vector failure")
	}
	if !strings.Contains(err.Error(), FailureClassInvalidVector) {
		t.Fatalf("error = %v, want failure class %q", err, FailureClassInvalidVector)
	}
	if result.DocumentCount != 1 || result.VectorCount != 0 || result.FailedCount != 1 {
		t.Fatalf("result = %#v, want one failed document", result)
	}
	if len(values.rows) != 0 {
		t.Fatalf("value rows = %d, want 0", len(values.rows))
	}
	if len(metadata.rows) != 1 {
		t.Fatalf("metadata rows = %d, want 1", len(metadata.rows))
	}
	if got, want := metadata.rows[0].FailureClass, FailureClassInvalidVector; got != want {
		t.Fatalf("failure class = %q, want %q", got, want)
	}
}

func TestBuilderValidatesBuildRequest(t *testing.T) {
	t.Parallel()

	_, err := Builder{}.Build(context.Background(), BuildRequest{})
	if err == nil {
		t.Fatal("Build error = nil, want validation error")
	}
	for _, want := range []string{
		"document store is required",
		"metadata store is required",
		"value store is required",
		"embedder is required",
		"scope id",
		"provider profile id",
		"source class",
		"embedding model id",
		"vector index version",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("validation error missing %q: %v", want, err)
		}
	}
}

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

type recordingDocumentStore struct {
	filter  postgres.EshuSearchDocumentFilter
	filters []postgres.EshuSearchDocumentFilter
	rows    []postgres.EshuSearchDocumentRow
	pages   [][]postgres.EshuSearchDocumentRow
}

func (s *recordingDocumentStore) ListActiveDocuments(
	_ context.Context,
	filter postgres.EshuSearchDocumentFilter,
) ([]postgres.EshuSearchDocumentRow, error) {
	s.filter = filter
	s.filters = append(s.filters, filter)
	if len(s.pages) > 0 {
		page := s.pages[0]
		s.pages = s.pages[1:]
		return append([]postgres.EshuSearchDocumentRow(nil), page...), nil
	}
	return append([]postgres.EshuSearchDocumentRow(nil), s.rows...), nil
}

type generationSwitchingDocumentStore struct {
	filters []postgres.EshuSearchDocumentFilter
	oldRows []postgres.EshuSearchDocumentRow
	newRows []postgres.EshuSearchDocumentRow
}

func (s *generationSwitchingDocumentStore) ListActiveDocuments(
	_ context.Context,
	filter postgres.EshuSearchDocumentFilter,
) ([]postgres.EshuSearchDocumentRow, error) {
	s.filters = append(s.filters, filter)
	if filter.GenerationID == "gen-old" {
		return pageSearchDocumentRows(s.oldRows, filter.Offset, filter.Limit), nil
	}
	if filter.GenerationID == "gen-new" {
		return pageSearchDocumentRows(s.newRows, filter.Offset, filter.Limit), nil
	}
	if filter.Offset == 0 {
		return pageSearchDocumentRows(s.oldRows, filter.Offset, filter.Limit), nil
	}
	return pageSearchDocumentRows(s.newRows, filter.Offset, filter.Limit), nil
}

func pageSearchDocumentRows(
	rows []postgres.EshuSearchDocumentRow,
	offset int,
	limit int,
) []postgres.EshuSearchDocumentRow {
	if offset >= len(rows) {
		return nil
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	return append([]postgres.EshuSearchDocumentRow(nil), rows[offset:end]...)
}

type recordingVectorMetadataStore struct {
	rows    []postgres.EshuSearchVectorMetadata
	batches [][]postgres.EshuSearchVectorMetadata
}

func (s *recordingVectorMetadataStore) UpsertBatch(_ context.Context, rows []postgres.EshuSearchVectorMetadata) error {
	s.batches = append(s.batches, rows)
	s.rows = append(s.rows, rows...)
	return nil
}

type recordingVectorValueStore struct {
	rows    []postgres.EshuSearchVectorValue
	batches [][]postgres.EshuSearchVectorValue
}

func (s *recordingVectorValueStore) UpsertBatch(_ context.Context, rows []postgres.EshuSearchVectorValue) error {
	s.batches = append(s.batches, rows)
	s.rows = append(s.rows, rows...)
	return nil
}

type recordingEmbedder struct {
	dims    int
	err     error
	vectors map[string][]float64
	calls   []string
}

func (e *recordingEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	e.calls = append(e.calls, text)
	if e.err != nil {
		return nil, e.err
	}
	return append([]float64(nil), e.vectors[text]...), nil
}

func (e *recordingEmbedder) Dimensions() int {
	return e.dims
}

func searchDocument(id, repoID, title, path string) searchdocs.Document {
	return searchdocs.Document{
		ID:          id,
		RepoID:      repoID,
		SourceKind:  searchdocs.SourceKindCodeEntity,
		Title:       title,
		Path:        path,
		ContextText: "bounded context",
		Labels:      []string{"go", "handler"},
		GraphHandles: []searchdocs.GraphHandle{{
			Kind: "content_entity",
			ID:   id,
		}},
		TruthScope: searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived},
		Freshness:  searchdocs.Freshness{State: searchdocs.FreshnessFresh},
	}
}

func sameVector(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
