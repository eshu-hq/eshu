// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchembed"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestSemanticSearchIndexCachePGXLive proves exact result reuse and removes
// repeated corpus/vector loads against an explicitly enabled retained corpus.
func TestSemanticSearchIndexCachePGXLive(t *testing.T) {
	if os.Getenv("ESHU_SEMANTIC_SEARCH_CACHE_LIVE") != "1" {
		t.Skip("set ESHU_SEMANTIC_SEARCH_CACHE_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	scopeID, repoID, queryText := retainedSemanticSearchCacheFixture(t, ctx, db)
	embedder, err := searchembed.NewHashEmbedder(searchembed.DefaultDimensions)
	if err != nil {
		t.Fatalf("create hash embedder: %v", err)
	}
	config := DefaultPersistedLocalSemanticSearchHybridConfig()
	request := semanticSearchIndexQuery{
		Request: searchretrieval.Request{
			Query: queryText, Scope: searchretrieval.Scope{RepoID: repoID},
			Mode: searchbench.ModeSemantic, Limit: 11, Timeout: 5 * time.Second,
		},
		ScopeID: scopeID,
		RepoID:  repoID,
	}

	uncached := NewPersistedLocalSemanticSearchHybrid(
		NewPostgresSemanticSearchIndexStore(db),
		pgstatus.NewEshuSearchVectorMetadataStore(pgstatus.SQLDB{DB: db}),
		pgstatus.NewEshuSearchVectorValueStore(pgstatus.SQLDB{DB: db}),
		embedder,
		config,
	)
	uncachedStarted := time.Now()
	uncachedResult, err := uncached.Search(ctx, request)
	if err != nil {
		t.Fatalf("uncached Search() error = %v", err)
	}
	uncachedDuration := time.Since(uncachedStarted)

	documents := &liveCountingSemanticSearchDocumentStore{inner: NewPostgresSemanticSearchIndexStore(db)}
	metadata := &liveCountingSemanticSearchMetadataStore{
		inner: pgstatus.NewEshuSearchVectorMetadataStore(pgstatus.SQLDB{DB: db}),
	}
	values := &liveCountingSemanticSearchValueStore{
		inner: pgstatus.NewEshuSearchVectorValueStore(pgstatus.SQLDB{DB: db}),
	}
	snapshots := &liveCountingSemanticSearchSnapshotStore{
		inner: NewPostgresSemanticSearchSnapshotStore(pgstatus.SQLDB{DB: db}),
	}
	cached := NewCachedPersistedLocalSemanticSearchHybrid(
		documents, metadata, values, snapshots, embedder, config,
	)
	missStarted := time.Now()
	missResult, err := cached.Search(ctx, request)
	if err != nil {
		t.Fatalf("cached miss Search() error = %v", err)
	}
	missDuration := time.Since(missStarted)
	if !reflect.DeepEqual(missResult, uncachedResult) {
		t.Fatal("cached miss result differs from uncached exact baseline")
	}

	hitDurations := make([]time.Duration, 10)
	for i := range hitDurations {
		started := time.Now()
		result, searchErr := cached.Search(ctx, request)
		hitDurations[i] = time.Since(started)
		if searchErr != nil {
			t.Fatalf("cached hit %d Search() error = %v", i+1, searchErr)
		}
		if !reflect.DeepEqual(result, uncachedResult) {
			t.Fatalf("cached hit %d differs from uncached exact baseline", i+1)
		}
	}
	if documents.calls != 1 || metadata.calls != 1 || values.calls != 1 {
		t.Fatalf("cached corpus loads documents/metadata/vectors = %d/%d/%d, want 1/1/1", documents.calls, metadata.calls, values.calls)
	}
	if got, want := snapshots.calls, 12; got != want {
		t.Fatalf("snapshot loads = %d, want %d (miss before/after plus ten hits)", got, want)
	}
	hitMedian := semanticSearchDurationMedian(hitDurations)
	if hitMedian >= missDuration || hitMedian >= uncachedDuration {
		t.Fatalf("cache hit median %s must beat miss %s and uncached %s", hitMedian, missDuration, uncachedDuration)
	}
	t.Logf("uncached=%s cached_miss=%s cached_hit_median=%s corpus_loads=1", uncachedDuration, missDuration, hitMedian)
}

func retainedSemanticSearchCacheFixture(t *testing.T, ctx context.Context, db *sql.DB) (string, string, string) {
	t.Helper()
	var scopeID, repoID, queryText string
	err := db.QueryRowContext(ctx, `
SELECT scope.scope_id, scope.payload->>'repo_id', term.term
FROM ingestion_scopes scope
JOIN eshu_search_document_projection_state projection
  ON projection.scope_id = scope.scope_id AND projection.generation_id = scope.active_generation_id
JOIN eshu_search_vector_scope_state vector
  ON vector.scope_id = scope.scope_id AND vector.generation_id = scope.active_generation_id
JOIN LATERAL (
    SELECT indexed.term FROM eshu_search_index_terms indexed
    WHERE indexed.scope_id = scope.scope_id AND indexed.generation_id = scope.active_generation_id
      AND length(indexed.term) >= 4
    LIMIT 1
) term ON true
WHERE projection.state = 'ready' AND projection.document_count > 0
  AND vector.state = 'ready' AND vector.projection_revision = projection.projection_revision
  AND vector.provider_profile_id = 'local' AND vector.source_class = 'search_documents'
  AND vector.embedding_model_id = 'local-hash-v1' AND vector.vector_index_version = 'vector-v1'
  AND NULLIF(scope.payload->>'repo_id', '') IS NOT NULL
ORDER BY projection.document_count DESC, scope.scope_id ASC LIMIT 1
`).Scan(&scopeID, &repoID, &queryText)
	if err != nil {
		t.Fatalf("select retained semantic-search cache fixture: %v", err)
	}
	return scopeID, repoID, queryText
}

func semanticSearchDurationMedian(values []time.Duration) time.Duration {
	ordered := append([]time.Duration(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	return ordered[len(ordered)/2]
}

type liveCountingSemanticSearchDocumentStore struct {
	inner PostgresSemanticSearchIndexStore
	calls int
}

func (s *liveCountingSemanticSearchDocumentStore) ListActiveDocuments(ctx context.Context, query semanticSearchDocumentQuery) ([]semanticSearchDocumentRow, error) {
	s.calls++
	return s.inner.ListActiveDocuments(ctx, query)
}

type liveCountingSemanticSearchMetadataStore struct {
	inner pgstatus.EshuSearchVectorMetadataStore
	calls int
}

func (s *liveCountingSemanticSearchMetadataStore) ListActive(ctx context.Context, filter pgstatus.EshuSearchVectorMetadataFilter) ([]pgstatus.EshuSearchVectorMetadata, error) {
	s.calls++
	return s.inner.ListActive(ctx, filter)
}

type liveCountingSemanticSearchValueStore struct {
	inner pgstatus.EshuSearchVectorValueStore
	calls int
}

func (s *liveCountingSemanticSearchValueStore) ListActive(ctx context.Context, filter pgstatus.EshuSearchVectorValueFilter) ([]pgstatus.EshuSearchVectorValue, error) {
	s.calls++
	return s.inner.ListActive(ctx, filter)
}

type liveCountingSemanticSearchSnapshotStore struct {
	inner PostgresSemanticSearchSnapshotStore
	calls int
}

func (s *liveCountingSemanticSearchSnapshotStore) Load(ctx context.Context, request SemanticSearchSnapshotRequest) (SemanticSearchSnapshot, error) {
	s.calls++
	return s.inner.Load(ctx, request)
}
