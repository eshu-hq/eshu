package query

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestSemanticSearchHandlerPersistedVectorsServeReadySemanticPath(t *testing.T) {
	t.Parallel()

	payments := semanticSearchDocumentFixture("searchdoc:payments", "repo-payments", "Payments", "refund checkout")
	billing := semanticSearchDocumentFixture("searchdoc:billing", "repo-payments", "Billing", "invoice ledger")
	documents := &fakeSemanticSearchDocumentStore{
		rows: []semanticSearchDocumentRow{{Document: payments}, {Document: billing}},
	}
	metadata := &fakeSemanticSearchVectorMetadataStore{
		rows: []postgres.EshuSearchVectorMetadata{
			readySemanticSearchVectorMetadata(payments, 2),
			readySemanticSearchVectorMetadata(billing, 2),
		},
	}
	values := &fakeSemanticSearchVectorValueStore{
		rows: []postgres.EshuSearchVectorValue{
			semanticSearchVectorValue(payments, []float64{1, 0}),
			semanticSearchVectorValue(billing, []float64{0, 1}),
		},
	}
	embedder := &fakeSemanticSearchEmbedder{
		dims:    2,
		vectors: map[string][]float64{"refund": {1, 0}},
	}
	handler := &SemanticSearchHandler{
		Index: &fakeSemanticSearchIndexStore{},
		LocalHybrid: NewPersistedLocalSemanticSearchHybrid(
			documents,
			metadata,
			values,
			embedder,
			DefaultPersistedLocalSemanticSearchHybridConfig(),
		),
		Profile: ProfileProduction,
	}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "refund",
		"mode":       "semantic",
		"limit":      5,
		"timeout_ms": 250,
	})
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := documents.calls, 1; got != want {
		t.Fatalf("document store calls = %d, want %d", got, want)
	}
	if got, want := metadata.calls, 1; got != want {
		t.Fatalf("metadata store calls = %d, want %d", got, want)
	}
	if got, want := metadata.query.ProviderProfileID, "local"; got != want {
		t.Fatalf("metadata provider profile = %q, want %q", got, want)
	}
	if got, want := metadata.query.SourceClass, "search_documents"; got != want {
		t.Fatalf("metadata source class = %q, want %q", got, want)
	}
	if got, want := values.calls, 1; got != want {
		t.Fatalf("value store calls = %d, want %d", got, want)
	}
	if got, want := values.query.ProviderProfileID, "local"; got != want {
		t.Fatalf("value provider profile = %q, want %q", got, want)
	}
	if got, want := values.query.SourceClass, "search_documents"; got != want {
		t.Fatalf("value source class = %q, want %q", got, want)
	}
	if got, want := fmt.Sprint(metadata.query.DocumentIDs), "[searchdoc:billing searchdoc:payments]"; got != want {
		t.Fatalf("metadata document ids = %s, want %s", got, want)
	}
	if got, want := fmt.Sprint(values.query.DocumentIDs), "[searchdoc:billing searchdoc:payments]"; got != want {
		t.Fatalf("value document ids = %s, want %s", got, want)
	}
	if got, want := embedder.calls, []string{"refund"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("embedder calls = %v, want %v", got, want)
	}
	data := semanticSearchEnvelopeData(t, rec)
	if got, want := data["retrieval_state"], "semantic_active"; got != want {
		t.Fatalf("retrieval_state = %#v, want %#v", got, want)
	}
	results := data["results"].([]any)
	if len(results) == 0 {
		t.Fatal("results empty, want persisted vector result")
	}
	result := results[0].(map[string]any)
	if got, want := result["search_method"], "vector"; got != want {
		t.Fatalf("search_method = %#v, want %#v", got, want)
	}
	document := result["document"].(map[string]any)
	if got, want := document["id"], "searchdoc:payments"; got != want {
		t.Fatalf("top document = %#v, want %#v", got, want)
	}
	metadataOut := result["metadata"].(map[string]any)
	if got, want := metadataOut["vector_source"], "persisted_local"; got != want {
		t.Fatalf("vector_source = %#v, want %#v", got, want)
	}
}

func TestPersistedLocalSemanticSearchHybridUsesConfiguredVectorRetrieval(t *testing.T) {
	t.Parallel()

	cross := semanticSearchDocumentFixture("searchdoc:cross", "repo-payments", "cross-best", "cross-best body")
	weaker := semanticSearchDocumentFixture("searchdoc:weaker", "repo-payments", "axis", "axis body")
	documents := &fakeSemanticSearchDocumentStore{
		rows: []semanticSearchDocumentRow{{Document: cross}, {Document: weaker}},
	}
	metadata := &fakeSemanticSearchVectorMetadataStore{
		rows: []postgres.EshuSearchVectorMetadata{
			readySemanticSearchVectorMetadata(cross, 2),
			readySemanticSearchVectorMetadata(weaker, 2),
		},
	}
	values := &fakeSemanticSearchVectorValueStore{
		rows: []postgres.EshuSearchVectorValue{
			semanticSearchVectorValue(cross, []float64{0.50, 0.51}),
			semanticSearchVectorValue(weaker, []float64{1, 0}),
		},
	}
	embedder := &fakeSemanticSearchEmbedder{
		dims:    2,
		vectors: map[string][]float64{"tilted-query": {0.51, 0.50}},
	}
	config := DefaultPersistedLocalSemanticSearchHybridConfig()
	config.VectorRetrieval = searchhybrid.VectorRetrievalApproximate
	handler := &SemanticSearchHandler{
		Index: &fakeSemanticSearchIndexStore{},
		LocalHybrid: NewPersistedLocalSemanticSearchHybrid(
			documents,
			metadata,
			values,
			embedder,
			config,
		),
		Profile: ProfileProduction,
	}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "tilted-query",
		"mode":       "semantic",
		"limit":      1,
		"timeout_ms": 250,
	})
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	data := semanticSearchEnvelopeData(t, rec)
	results := data["results"].([]any)
	if len(results) == 0 {
		t.Fatal("results empty, want persisted vector result")
	}
	document := results[0].(map[string]any)["document"].(map[string]any)
	if got, want := document["id"], "searchdoc:cross"; got != want {
		t.Fatalf("top document = %#v, want %#v", got, want)
	}
}

func TestDefaultPersistedLocalSemanticSearchHybridConfigUsesAutoVectorRetrieval(t *testing.T) {
	t.Parallel()

	config := DefaultPersistedLocalSemanticSearchHybridConfig()

	if got, want := config.VectorRetrieval, searchhybrid.VectorRetrievalAuto; got != want {
		t.Fatalf("VectorRetrieval = %q, want %q", got, want)
	}
	if got, want := config.ProviderProfileID, "local"; got != want {
		t.Fatalf("ProviderProfileID = %q, want %q", got, want)
	}
	if got, want := config.SourceClass, "search_documents"; got != want {
		t.Fatalf("SourceClass = %q, want %q", got, want)
	}
}

func TestSemanticSearchHandlerPersistedVectorsReportIndexUnreadyForPartialState(t *testing.T) {
	t.Parallel()

	payments := semanticSearchDocumentFixture("searchdoc:payments", "repo-payments", "Payments", "refund checkout")
	billing := semanticSearchDocumentFixture("searchdoc:billing", "repo-payments", "Billing", "invoice ledger")
	documents := &fakeSemanticSearchDocumentStore{
		rows: []semanticSearchDocumentRow{{Document: payments}, {Document: billing}},
	}
	metadata := &fakeSemanticSearchVectorMetadataStore{
		rows: []postgres.EshuSearchVectorMetadata{readySemanticSearchVectorMetadata(payments, 2)},
	}
	values := &fakeSemanticSearchVectorValueStore{
		rows: []postgres.EshuSearchVectorValue{semanticSearchVectorValue(payments, []float64{1, 0})},
	}
	handler := &SemanticSearchHandler{
		Index: &fakeSemanticSearchIndexStore{},
		LocalHybrid: NewPersistedLocalSemanticSearchHybrid(
			documents,
			metadata,
			values,
			&fakeSemanticSearchEmbedder{dims: 2, vectors: map[string][]float64{"refund": {1, 0}}},
			DefaultPersistedLocalSemanticSearchHybridConfig(),
		),
		Profile: ProfileProduction,
	}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "refund",
		"mode":       "hybrid",
		"limit":      5,
		"timeout_ms": 250,
	})
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	data := semanticSearchEnvelopeData(t, rec)
	if got, want := data["retrieval_state"], "index_unready"; got != want {
		t.Fatalf("retrieval_state = %#v, want %#v", got, want)
	}
	results := data["results"].([]any)
	if len(results) == 0 {
		t.Fatal("results empty, want keyword fallback result")
	}
	result := results[0].(map[string]any)
	if got, want := result["search_method"], "bm25"; got != want {
		t.Fatalf("search_method = %#v, want %#v", got, want)
	}
	metadataOut := result["metadata"].(map[string]any)
	if got, want := metadataOut["vector_retrieval_state"], "index_unready"; got != want {
		t.Fatalf("vector_retrieval_state = %#v, want %#v", got, want)
	}
}

func TestSemanticSearchHandlerPersistedVectorsReportIndexUnreadyForInvalidStates(t *testing.T) {
	t.Parallel()

	payments := semanticSearchDocumentFixture("searchdoc:payments", "repo-payments", "Payments", "refund checkout")
	baseMetadata := readySemanticSearchVectorMetadata(payments, 2)
	baseValue := semanticSearchVectorValue(payments, []float64{1, 0})
	tests := []struct {
		name     string
		metadata []postgres.EshuSearchVectorMetadata
		values   []postgres.EshuSearchVectorValue
	}{
		{name: "missing_metadata", values: []postgres.EshuSearchVectorValue{baseValue}},
		{name: "missing_value", metadata: []postgres.EshuSearchVectorMetadata{baseMetadata}},
		{
			name: "building",
			metadata: []postgres.EshuSearchVectorMetadata{
				semanticSearchVectorMetadataWithState(baseMetadata, postgres.EshuSearchVectorBuildStateBuilding),
			},
			values: []postgres.EshuSearchVectorValue{baseValue},
		},
		{
			name: "failed",
			metadata: []postgres.EshuSearchVectorMetadata{
				semanticSearchVectorMetadataWithState(baseMetadata, postgres.EshuSearchVectorBuildStateFailed),
			},
			values: []postgres.EshuSearchVectorValue{baseValue},
		},
		{
			name: "stale",
			metadata: []postgres.EshuSearchVectorMetadata{
				semanticSearchVectorMetadataWithState(baseMetadata, postgres.EshuSearchVectorBuildStateStale),
			},
			values: []postgres.EshuSearchVectorValue{baseValue},
		},
		{
			name:     "incompatible_content_hash",
			metadata: []postgres.EshuSearchVectorMetadata{baseMetadata},
			values: []postgres.EshuSearchVectorValue{
				semanticSearchVectorValueWithHash(baseValue, "sha256:wrong"),
			},
		},
		{
			name: "provider_profile_mismatch",
			metadata: []postgres.EshuSearchVectorMetadata{
				semanticSearchVectorMetadataWithProvider(baseMetadata, "semantic-search-default"),
			},
			values: []postgres.EshuSearchVectorValue{baseValue},
		},
		{
			name: "source_class_mismatch",
			metadata: []postgres.EshuSearchVectorMetadata{
				semanticSearchVectorMetadataWithSourceClass(baseMetadata, "documentation"),
			},
			values: []postgres.EshuSearchVectorValue{baseValue},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := &SemanticSearchHandler{
				Index: &fakeSemanticSearchIndexStore{},
				LocalHybrid: NewPersistedLocalSemanticSearchHybrid(
					&fakeSemanticSearchDocumentStore{rows: []semanticSearchDocumentRow{{Document: payments}}},
					&fakeSemanticSearchVectorMetadataStore{rows: tc.metadata},
					&fakeSemanticSearchVectorValueStore{rows: tc.values},
					&fakeSemanticSearchEmbedder{dims: 2, vectors: map[string][]float64{"refund": {1, 0}}},
					DefaultPersistedLocalSemanticSearchHybridConfig(),
				),
				Profile: ProfileProduction,
			}
			req := semanticSearchHTTPRequest(t, map[string]any{
				"repo_id":    "repo-payments",
				"query":      "refund",
				"mode":       "hybrid",
				"limit":      5,
				"timeout_ms": 250,
			})
			rec := httptest.NewRecorder()

			handler.search(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			data := semanticSearchEnvelopeData(t, rec)
			if got, want := data["retrieval_state"], "index_unready"; got != want {
				t.Fatalf("retrieval_state = %#v, want %#v", got, want)
			}
			results := data["results"].([]any)
			if len(results) == 0 {
				t.Fatal("results empty, want keyword fallback result")
			}
			result := results[0].(map[string]any)
			if got, want := result["search_method"], "bm25"; got != want {
				t.Fatalf("search_method = %#v, want %#v", got, want)
			}
			metadataOut := result["metadata"].(map[string]any)
			if got, want := metadataOut["vector_retrieval_state"], "index_unready"; got != want {
				t.Fatalf("vector_retrieval_state = %#v, want %#v", got, want)
			}
		})
	}
}

type fakeSemanticSearchVectorMetadataStore struct {
	rows  []postgres.EshuSearchVectorMetadata
	query postgres.EshuSearchVectorMetadataFilter
	calls int
	err   error
}

func (s *fakeSemanticSearchVectorMetadataStore) ListActive(
	_ context.Context,
	filter postgres.EshuSearchVectorMetadataFilter,
) ([]postgres.EshuSearchVectorMetadata, error) {
	s.calls++
	s.query = filter
	if s.err != nil {
		return nil, s.err
	}
	return append([]postgres.EshuSearchVectorMetadata(nil), s.rows...), nil
}

type fakeSemanticSearchVectorValueStore struct {
	rows  []postgres.EshuSearchVectorValue
	query postgres.EshuSearchVectorValueFilter
	calls int
	err   error
}

func (s *fakeSemanticSearchVectorValueStore) ListActive(
	_ context.Context,
	filter postgres.EshuSearchVectorValueFilter,
) ([]postgres.EshuSearchVectorValue, error) {
	s.calls++
	s.query = filter
	if s.err != nil {
		return nil, s.err
	}
	return append([]postgres.EshuSearchVectorValue(nil), s.rows...), nil
}

type fakeSemanticSearchEmbedder struct {
	dims    int
	vectors map[string][]float64
	calls   []string
}

func (e *fakeSemanticSearchEmbedder) Dimensions() int { return e.dims }

func (e *fakeSemanticSearchEmbedder) Embed(text string) ([]float64, error) {
	e.calls = append(e.calls, text)
	vector, ok := e.vectors[text]
	if !ok {
		return nil, fmt.Errorf("unexpected embed text %q", text)
	}
	return append([]float64(nil), vector...), nil
}

func readySemanticSearchVectorMetadata(doc searchdocs.Document, dimensions int) postgres.EshuSearchVectorMetadata {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	return postgres.EshuSearchVectorMetadata{
		ScopeID:              doc.RepoID,
		GenerationID:         "gen-active",
		DocumentID:           doc.ID,
		ProviderProfileID:    "local",
		SourceClass:          "search_documents",
		EmbeddingModelID:     "local-hash-v1",
		EmbeddingDimensions:  dimensions,
		EmbeddingContentHash: searchhybrid.DocumentContentHash(doc),
		VectorIndexVersion:   "vector-v1",
		BuildState:           postgres.EshuSearchVectorBuildStateReady,
		CreatedAt:            now,
		UpdatedAt:            now,
		LastSuccessAt:        &now,
	}
}

func semanticSearchVectorMetadataWithState(
	row postgres.EshuSearchVectorMetadata,
	state postgres.EshuSearchVectorBuildState,
) postgres.EshuSearchVectorMetadata {
	row.BuildState = state
	return row
}

func semanticSearchVectorMetadataWithProvider(
	row postgres.EshuSearchVectorMetadata,
	providerProfileID string,
) postgres.EshuSearchVectorMetadata {
	row.ProviderProfileID = providerProfileID
	return row
}

func semanticSearchVectorMetadataWithSourceClass(
	row postgres.EshuSearchVectorMetadata,
	sourceClass string,
) postgres.EshuSearchVectorMetadata {
	row.SourceClass = sourceClass
	return row
}

func semanticSearchVectorValue(doc searchdocs.Document, vector []float64) postgres.EshuSearchVectorValue {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	return postgres.EshuSearchVectorValue{
		ScopeID:              doc.RepoID,
		GenerationID:         "gen-active",
		DocumentID:           doc.ID,
		ProviderProfileID:    "local",
		SourceClass:          "search_documents",
		EmbeddingModelID:     "local-hash-v1",
		EmbeddingDimensions:  len(vector),
		EmbeddingContentHash: searchhybrid.DocumentContentHash(doc),
		VectorIndexVersion:   "vector-v1",
		VectorValues:         append([]float64(nil), vector...),
		CreatedAt:            now,
		UpdatedAt:            now,
	}
}

func semanticSearchVectorValueWithHash(
	row postgres.EshuSearchVectorValue,
	contentHash string,
) postgres.EshuSearchVectorValue {
	row.EmbeddingContentHash = contentHash
	return row
}
