package searchvector

import (
	"context"
	"errors"
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
	if len(values.rows) != 1 {
		t.Fatalf("value rows = %d, want 1", len(values.rows))
	}
	value := values.rows[0]
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
		"embedding model id",
		"vector index version",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("validation error missing %q: %v", want, err)
		}
	}
}

type recordingDocumentStore struct {
	filter postgres.EshuSearchDocumentFilter
	rows   []postgres.EshuSearchDocumentRow
}

func (s *recordingDocumentStore) ListActiveDocuments(
	_ context.Context,
	filter postgres.EshuSearchDocumentFilter,
) ([]postgres.EshuSearchDocumentRow, error) {
	s.filter = filter
	return append([]postgres.EshuSearchDocumentRow(nil), s.rows...), nil
}

type recordingVectorMetadataStore struct {
	rows []postgres.EshuSearchVectorMetadata
}

func (s *recordingVectorMetadataStore) Upsert(_ context.Context, row postgres.EshuSearchVectorMetadata) error {
	s.rows = append(s.rows, row)
	return nil
}

type recordingVectorValueStore struct {
	rows []postgres.EshuSearchVectorValue
}

func (s *recordingVectorValueStore) Upsert(_ context.Context, row postgres.EshuSearchVectorValue) error {
	s.rows = append(s.rows, row)
	return nil
}

type recordingEmbedder struct {
	dims    int
	err     error
	vectors map[string][]float64
	calls   []string
}

func (e *recordingEmbedder) Embed(text string) ([]float64, error) {
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
