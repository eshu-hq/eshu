// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestPersistedSemanticSearchCachesStableReadySnapshotAcrossQueries(t *testing.T) {
	t.Parallel()

	hybrid, documents, metadata, values, snapshots := newSemanticSearchCacheTestHybrid(t, 2)
	query := semanticSearchCacheTestQuery("refund")
	first, err := hybrid.Search(context.Background(), query)
	if err != nil {
		t.Fatalf("first Search() error = %v", err)
	}
	second, err := hybrid.Search(context.Background(), query)
	if err != nil {
		t.Fatalf("second Search() error = %v", err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("cached Search() = %#v, want exact first result %#v", second, first)
	}

	if got, want := documents.callCount(), 1; got != want {
		t.Fatalf("document loads = %d, want %d", got, want)
	}
	if got, want := metadata.callCount(), 1; got != want {
		t.Fatalf("metadata loads = %d, want %d", got, want)
	}
	if got, want := values.callCount(), 1; got != want {
		t.Fatalf("vector loads = %d, want %d", got, want)
	}
	if got, want := snapshots.callCount(), 3; got != want {
		t.Fatalf("snapshot loads = %d, want %d (miss before/after plus hit validation)", got, want)
	}
}

func TestPersistedSemanticSearchInvalidatesCacheOnProjectionRevisionChange(t *testing.T) {
	t.Parallel()

	hybrid, documents, metadata, values, snapshots := newSemanticSearchCacheTestHybrid(t, 2)
	revision1 := semanticSearchCacheTestSnapshot(1)
	revision2 := semanticSearchCacheTestSnapshot(2)
	snapshots.setSequence(revision1, revision1, revision2, revision2, revision2)
	query := semanticSearchCacheTestQuery("refund")

	for run := 0; run < 3; run++ {
		query.Request.Query = []string{"refund", "checkout", "payments"}[run]
		if _, err := hybrid.Search(context.Background(), query); err != nil {
			t.Fatalf("Search() run %d error = %v", run+1, err)
		}
	}

	if got, want := documents.callCount(), 2; got != want {
		t.Fatalf("document loads = %d, want %d after revision invalidation", got, want)
	}
	if got, want := metadata.callCount(), 2; got != want {
		t.Fatalf("metadata loads = %d, want %d after revision invalidation", got, want)
	}
	if got, want := values.callCount(), 2; got != want {
		t.Fatalf("vector loads = %d, want %d after revision invalidation", got, want)
	}
	if got, want := snapshots.callCount(), 5; got != want {
		t.Fatalf("snapshot loads = %d, want %d", got, want)
	}
}

func TestPersistedSemanticSearchCoalescesConcurrentBuildForSameSnapshot(t *testing.T) {
	t.Parallel()

	hybrid, documents, metadata, values, snapshots := newSemanticSearchCacheTestHybrid(t, 2)
	documents.started = make(chan struct{})
	documents.release = make(chan struct{})
	snapshots.loaded = make(chan struct{}, 3)
	query := semanticSearchCacheTestQuery("refund")

	first := make(chan error, 1)
	go func() {
		_, err := hybrid.Search(context.Background(), query)
		first <- err
	}()
	select {
	case <-documents.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first build did not reach document load")
	}
	waitForSemanticSearchSnapshotLoad(t, snapshots.loaded, "first request")

	second := make(chan error, 1)
	go func() {
		_, err := hybrid.Search(context.Background(), query)
		second <- err
	}()
	waitForSemanticSearchSnapshotLoad(t, snapshots.loaded, "coalesced request")
	close(documents.release)

	for run, result := range []<-chan error{first, second} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatalf("concurrent Search() run %d error = %v", run+1, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("concurrent Search() run %d timed out", run+1)
		}
	}
	if got, want := documents.callCount(), 1; got != want {
		t.Fatalf("document loads = %d, want one coalesced build", got)
	}
	if got, want := metadata.callCount(), 1; got != want {
		t.Fatalf("metadata loads = %d, want one coalesced build", got)
	}
	if got, want := values.callCount(), 1; got != want {
		t.Fatalf("vector loads = %d, want one coalesced build", got)
	}
}

func TestSemanticSearchIndexCacheEvictsLeastRecentlyUsedEntry(t *testing.T) {
	t.Parallel()

	cache := newSemanticSearchIndexCache(2)
	loads := map[string]int{}
	load := func(key string) {
		t.Helper()
		cacheKey := semanticSearchIndexCacheKey{ScopeID: key}
		_, _, err := cache.load(context.Background(), cacheKey, func() (*semanticSearchCachedIndex, error) {
			loads[key]++
			return &semanticSearchCachedIndex{}, nil
		})
		if err != nil {
			t.Fatalf("cache.load(%q) error = %v", key, err)
		}
	}

	load("a")
	load("b")
	load("a")
	load("c")
	load("b")
	if got, want := loads["a"], 1; got != want {
		t.Fatalf("entry a loads = %d, want %d after recent access", got, want)
	}
	if got, want := loads["b"], 2; got != want {
		t.Fatalf("entry b loads = %d, want %d after LRU eviction", got, want)
	}
}

func TestSemanticSearchIndexCacheBuildsDifferentKeysConcurrently(t *testing.T) {
	t.Parallel()

	cache := newSemanticSearchIndexCache(2)
	started := make(chan string, 2)
	release := make(chan struct{})
	results := make(chan error, 2)
	for _, key := range []string{"scope-a", "scope-b"} {
		key := key
		go func() {
			_, _, err := cache.load(
				context.Background(),
				semanticSearchIndexCacheKey{ScopeID: key},
				func() (*semanticSearchCachedIndex, error) {
					started <- key
					<-release
					return &semanticSearchCachedIndex{}, nil
				},
			)
			results <- err
		}()
	}
	seen := make(map[string]bool, 2)
	for range 2 {
		select {
		case key := <-started:
			seen[key] = true
		case <-time.After(2 * time.Second):
			t.Fatal("different-key cache builds did not start concurrently")
		}
	}
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("cache build error = %v", err)
		}
	}
	if !seen["scope-a"] || !seen["scope-b"] {
		t.Fatalf("started keys = %v, want both independent scopes", seen)
	}
}

func TestSemanticSearchCacheFilterSignatureNormalizesOrderAndDuplicates(t *testing.T) {
	t.Parallel()

	unique := semanticSearchCacheTestQuery("refund")
	unique.Languages = []string{"go", "typescript"}
	duplicate := semanticSearchCacheTestQuery("refund")
	duplicate.Languages = []string{" TypeScript ", "go", "go"}
	if got, want := semanticSearchCacheFilterSignature(duplicate), semanticSearchCacheFilterSignature(unique); got != want {
		t.Fatalf("duplicate filter signature = %q, want normalized %q", got, want)
	}
}

func waitForSemanticSearchSnapshotLoad(t *testing.T, loaded <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-loaded:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not load durable snapshot", label)
	}
}

func TestPersistedSemanticSearchCacheKeyIncludesCorpusFilters(t *testing.T) {
	t.Parallel()

	hybrid, documents, _, _, _ := newSemanticSearchCacheTestHybrid(t, 2)
	query := semanticSearchCacheTestQuery("refund")
	query.Languages = []string{"go"}
	if _, err := hybrid.Search(context.Background(), query); err != nil {
		t.Fatalf("Go-filter Search() error = %v", err)
	}
	query.Languages = []string{"typescript"}
	if _, err := hybrid.Search(context.Background(), query); err != nil {
		t.Fatalf("TypeScript-filter Search() error = %v", err)
	}

	if got, want := documents.callCount(), 2; got != want {
		t.Fatalf("document loads = %d, want %d distinct filtered corpora", got, want)
	}
}

func newSemanticSearchCacheTestHybrid(
	t *testing.T,
	capacity int,
) (*PersistedLocalSemanticSearchHybrid, *countingSemanticSearchDocumentStore,
	*countingSemanticSearchMetadataStore, *countingSemanticSearchValueStore,
	*sequenceSemanticSearchSnapshotStore,
) {
	t.Helper()
	document := semanticSearchDocumentFixture(
		"searchdoc:payments",
		"repo-payments",
		"Payments",
		"refund checkout payments",
	)
	document.Labels = []string{"language:go", "language:typescript"}
	documents := &countingSemanticSearchDocumentStore{
		rows: []semanticSearchDocumentRow{{Document: document}},
	}
	metadata := &countingSemanticSearchMetadataStore{
		rows: []postgres.EshuSearchVectorMetadata{readySemanticSearchVectorMetadata(document, 2)},
	}
	values := &countingSemanticSearchValueStore{
		rows: []postgres.EshuSearchVectorValue{semanticSearchVectorValue(document, []float64{1, 0})},
	}
	snapshots := &sequenceSemanticSearchSnapshotStore{
		sequence: []SemanticSearchSnapshot{semanticSearchCacheTestSnapshot(1)},
	}
	config := DefaultPersistedLocalSemanticSearchHybridConfig()
	config.CacheEntries = capacity
	hybrid := NewCachedPersistedLocalSemanticSearchHybrid(
		documents,
		metadata,
		values,
		snapshots,
		&cacheTestSemanticSearchEmbedder{},
		config,
	)
	return hybrid, documents, metadata, values, snapshots
}

func semanticSearchCacheTestQuery(query string) semanticSearchIndexQuery {
	return semanticSearchIndexQuery{
		Request: searchretrieval.Request{
			Query:   query,
			Scope:   searchretrieval.Scope{RepoID: "repo-payments"},
			Mode:    searchbench.ModeSemantic,
			Limit:   5,
			Timeout: time.Second,
		},
		ScopeID: "scope-payments",
		RepoID:  "repo-payments",
	}
}

func semanticSearchCacheTestSnapshot(revision int64) SemanticSearchSnapshot {
	return SemanticSearchSnapshot{
		GenerationID:               "generation-active",
		DocumentProjectionRevision: revision,
		DocumentCount:              1,
		VectorProjectionRevision:   revision,
		VectorBuildFence:           revision,
		VectorState:                "ready",
	}
}

type sequenceSemanticSearchSnapshotStore struct {
	mu       sync.Mutex
	sequence []SemanticSearchSnapshot
	calls    int
	loaded   chan struct{}
}

func (s *sequenceSemanticSearchSnapshotStore) Load(
	_ context.Context,
	_ SemanticSearchSnapshotRequest,
) (SemanticSearchSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.calls
	s.calls++
	if index >= len(s.sequence) {
		index = len(s.sequence) - 1
	}
	if s.loaded != nil {
		s.loaded <- struct{}{}
	}
	return s.sequence[index], nil
}

func (s *sequenceSemanticSearchSnapshotStore) setSequence(sequence ...SemanticSearchSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence = append([]SemanticSearchSnapshot(nil), sequence...)
	s.calls = 0
}

func (s *sequenceSemanticSearchSnapshotStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type countingSemanticSearchDocumentStore struct {
	mu             sync.Mutex
	rows           []semanticSearchDocumentRow
	calls          int
	started        chan struct{}
	release        chan struct{}
	blockFirstOnly bool
}

func (s *countingSemanticSearchDocumentStore) ListActiveDocuments(
	ctx context.Context,
	_ semanticSearchDocumentQuery,
) ([]semanticSearchDocumentRow, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	started := s.started
	release := s.release
	blockFirstOnly := s.blockFirstOnly
	s.mu.Unlock()
	if started != nil {
		select {
		case <-started:
		default:
			close(started)
		}
	}
	if release != nil && (!blockFirstOnly || call == 1) {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return append([]semanticSearchDocumentRow(nil), s.rows...), nil
}

func (s *countingSemanticSearchDocumentStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type countingSemanticSearchMetadataStore struct {
	mu    sync.Mutex
	rows  []postgres.EshuSearchVectorMetadata
	calls int
}

func (s *countingSemanticSearchMetadataStore) ListActive(
	_ context.Context,
	_ postgres.EshuSearchVectorMetadataFilter,
) ([]postgres.EshuSearchVectorMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return append([]postgres.EshuSearchVectorMetadata(nil), s.rows...), nil
}

func (s *countingSemanticSearchMetadataStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type countingSemanticSearchValueStore struct {
	mu    sync.Mutex
	rows  []postgres.EshuSearchVectorValue
	calls int
}

func (s *countingSemanticSearchValueStore) ListActive(
	_ context.Context,
	_ postgres.EshuSearchVectorValueFilter,
) ([]postgres.EshuSearchVectorValue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return append([]postgres.EshuSearchVectorValue(nil), s.rows...), nil
}

func (s *countingSemanticSearchValueStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type cacheTestSemanticSearchEmbedder struct{}

func (*cacheTestSemanticSearchEmbedder) Dimensions() int { return 2 }

func (*cacheTestSemanticSearchEmbedder) Embed(context.Context, string) ([]float64, error) {
	return []float64{1, 0}, nil
}

var (
	_ SemanticSearchDocumentStore       = (*countingSemanticSearchDocumentStore)(nil)
	_ SemanticSearchVectorMetadataStore = (*countingSemanticSearchMetadataStore)(nil)
	_ SemanticSearchVectorValueStore    = (*countingSemanticSearchValueStore)(nil)
)
