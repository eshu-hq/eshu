// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultSemanticSearchIndexCacheEntries = 8
	maxSemanticSearchIndexCacheEntries     = 32
	semanticSearchCacheStateHit            = "hit"
	semanticSearchCacheStateMiss           = "miss"
	semanticSearchCacheStateCoalesced      = "coalesced"
	semanticSearchCacheStateBypass         = "bypass_unready"
	semanticSearchCacheStateRetry          = "retry_snapshot_changed"
)

var (
	errSemanticSearchCacheSnapshotChanged = errors.New("semantic search cache snapshot changed during build")
	errSemanticSearchCacheIndexUnready    = errors.New("semantic search cache index became unready during build")
)

type semanticSearchIndexCacheKey struct {
	ScopeID                    string
	RepoID                     string
	GenerationID               string
	ProviderProfileID          string
	SourceClass                string
	EmbeddingModelID           string
	VectorIndexVersion         string
	VectorRetrieval            searchhybrid.VectorRetrievalMode
	FilterSignature            string
	DocumentProjectionRevision int64
	DocumentCount              int
	VectorProjectionRevision   int64
	VectorBuildFence           int64
	CorpusLimit                int
}

type semanticSearchCachedIndex struct {
	index                *searchhybrid.Index
	indexedDocumentCount int
	corpusMayBeTruncated bool
}

type semanticSearchIndexCacheValue struct {
	key   semanticSearchIndexCacheKey
	entry *semanticSearchCachedIndex
}

type semanticSearchIndexCacheBuild struct {
	done  chan struct{}
	entry *semanticSearchCachedIndex
	err   error
}

type semanticSearchCacheIndexUnreadyError struct {
	build semanticSearchPersistedIndexBuild
}

func (e *semanticSearchCacheIndexUnreadyError) Error() string {
	return errSemanticSearchCacheIndexUnready.Error()
}

func (e *semanticSearchCacheIndexUnreadyError) Unwrap() error {
	return errSemanticSearchCacheIndexUnready
}

// semanticSearchIndexCache is a bounded process-local LRU. A build is
// coalesced only with the same exact durable snapshot and corpus-filter key;
// different repositories or revisions build concurrently.
type semanticSearchIndexCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[semanticSearchIndexCacheKey]*list.Element
	order    *list.List
	builds   map[semanticSearchIndexCacheKey]*semanticSearchIndexCacheBuild
}

func newSemanticSearchIndexCache(capacity int) *semanticSearchIndexCache {
	capacity = normalizeSemanticSearchIndexCacheEntries(capacity)
	return &semanticSearchIndexCache{
		capacity: capacity,
		entries:  make(map[semanticSearchIndexCacheKey]*list.Element, capacity),
		order:    list.New(),
		builds:   make(map[semanticSearchIndexCacheKey]*semanticSearchIndexCacheBuild),
	}
}

func (c *semanticSearchIndexCache) load(
	ctx context.Context,
	key semanticSearchIndexCacheKey,
	loader func() (*semanticSearchCachedIndex, error),
) (*semanticSearchCachedIndex, string, error) {
	c.mu.Lock()
	if element, ok := c.entries[key]; ok {
		c.order.MoveToFront(element)
		entry := element.Value.(semanticSearchIndexCacheValue).entry
		c.mu.Unlock()
		return entry, semanticSearchCacheStateHit, nil
	}
	if build, ok := c.builds[key]; ok {
		c.mu.Unlock()
		select {
		case <-build.done:
			return build.entry, semanticSearchCacheStateCoalesced, build.err
		case <-ctx.Done():
			return nil, semanticSearchCacheStateCoalesced, ctx.Err()
		}
	}
	build := &semanticSearchIndexCacheBuild{done: make(chan struct{})}
	c.builds[key] = build
	c.mu.Unlock()

	entry, err := loader()
	c.mu.Lock()
	build.entry = entry
	build.err = err
	if err == nil && entry != nil {
		c.insertLocked(key, entry)
	}
	delete(c.builds, key)
	close(build.done)
	c.mu.Unlock()
	return entry, semanticSearchCacheStateMiss, err
}

func (c *semanticSearchIndexCache) insertLocked(
	key semanticSearchIndexCacheKey,
	entry *semanticSearchCachedIndex,
) {
	if existing, ok := c.entries[key]; ok {
		existing.Value = semanticSearchIndexCacheValue{key: key, entry: entry}
		c.order.MoveToFront(existing)
		return
	}
	element := c.order.PushFront(semanticSearchIndexCacheValue{key: key, entry: entry})
	c.entries[key] = element
	for c.order.Len() > c.capacity {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		value := oldest.Value.(semanticSearchIndexCacheValue)
		delete(c.entries, value.key)
		c.order.Remove(oldest)
	}
}

func normalizeSemanticSearchIndexCacheEntries(entries int) int {
	if entries <= 0 {
		return defaultSemanticSearchIndexCacheEntries
	}
	if entries > maxSemanticSearchIndexCacheEntries {
		return maxSemanticSearchIndexCacheEntries
	}
	return entries
}

func (h *PersistedLocalSemanticSearchHybrid) searchCached(
	ctx context.Context,
	query semanticSearchIndexQuery,
) (semanticSearchIndexResult, error) {
	for attempt := 0; attempt < 2; attempt++ {
		snapshot, err := h.Snapshots.Load(ctx, h.snapshotRequest(query.ScopeID))
		if err != nil {
			return semanticSearchIndexResult{}, err
		}
		if !snapshot.Cacheable() {
			annotateSemanticSearchIndexCache(ctx, semanticSearchCacheStateBypass)
			return h.searchUncached(ctx, query)
		}
		key := h.cacheKey(query, snapshot)
		entry, cacheState, err := h.cache.load(ctx, key, func() (*semanticSearchCachedIndex, error) {
			build, buildErr := h.buildPersistedIndex(ctx, query)
			if buildErr != nil {
				return nil, buildErr
			}
			if build.state != "ready" || build.entry == nil {
				return nil, &semanticSearchCacheIndexUnreadyError{build: build}
			}
			after, loadErr := h.Snapshots.Load(ctx, h.snapshotRequest(query.ScopeID))
			if loadErr != nil {
				return nil, loadErr
			}
			if after != snapshot {
				return nil, errSemanticSearchCacheSnapshotChanged
			}
			return build.entry, nil
		})
		if err == nil {
			annotateSemanticSearchIndexCache(ctx, cacheState)
			return h.searchReadyIndex(ctx, query, entry)
		}
		if errors.Is(err, errSemanticSearchCacheSnapshotChanged) ||
			(semanticSearchCacheBuildContextEnded(err) && ctx.Err() == nil) {
			annotateSemanticSearchIndexCache(ctx, semanticSearchCacheStateRetry)
			continue
		}
		var unreadyErr *semanticSearchCacheIndexUnreadyError
		if errors.As(err, &unreadyErr) {
			annotateSemanticSearchIndexCache(ctx, semanticSearchCacheStateBypass)
			return h.keywordFallback(
				ctx,
				query,
				unreadyErr.build.docs,
				unreadyErr.build.state,
				unreadyErr.build.rowCount,
			)
		}
		return semanticSearchIndexResult{}, err
	}
	annotateSemanticSearchIndexCache(ctx, semanticSearchCacheStateBypass)
	return h.searchUncached(ctx, query)
}

func semanticSearchCacheBuildContextEnded(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

type semanticSearchPersistedIndexBuild struct {
	entry    *semanticSearchCachedIndex
	docs     []searchdocs.Document
	rowCount int
	state    string
}

func (h *PersistedLocalSemanticSearchHybrid) buildPersistedIndex(
	ctx context.Context,
	query semanticSearchIndexQuery,
) (semanticSearchPersistedIndexBuild, error) {
	rows, err := h.Documents.ListActiveDocuments(ctx, semanticSearchDocumentQuery{
		ScopeID:     query.ScopeID,
		RepoID:      query.RepoID,
		SourceKinds: query.SourceKinds,
		Languages:   query.Languages,
		Limit:       h.Config.CorpusLimit,
	})
	if err != nil {
		return semanticSearchPersistedIndexBuild{}, err
	}
	docs := semanticSearchDocumentsFiltered(rows, query.Languages)
	vectors, state, err := h.readyVectors(ctx, docs, query.ScopeID)
	if err != nil {
		return semanticSearchPersistedIndexBuild{}, err
	}
	build := semanticSearchPersistedIndexBuild{docs: docs, rowCount: len(rows), state: state}
	if state != "ready" {
		return build, nil
	}
	index, err := searchhybrid.NewIndex(docs, searchhybrid.Options{
		MaxDocuments:               h.Config.CorpusLimit,
		Embedder:                   h.Embedder,
		PrecomputedDocumentVectors: vectors,
		VectorRetrieval:            h.Config.VectorRetrieval,
	})
	if err != nil {
		build.state = "index_unready"
		return build, nil
	}
	build.entry = &semanticSearchCachedIndex{
		index:                index,
		indexedDocumentCount: index.Size(),
		corpusMayBeTruncated: index.Overflow() > 0 || len(rows) >= h.Config.CorpusLimit,
	}
	return build, nil
}

func (h *PersistedLocalSemanticSearchHybrid) searchReadyIndex(
	ctx context.Context,
	query semanticSearchIndexQuery,
	entry *semanticSearchCachedIndex,
) (semanticSearchIndexResult, error) {
	if entry == nil || entry.index == nil {
		return semanticSearchIndexResult{}, fmt.Errorf("semantic search cached index is required")
	}
	candidates, err := (searchhybrid.Backend{Index: entry.index}).Search(ctx, query.Request)
	if err != nil {
		return semanticSearchIndexResult{}, err
	}
	annotateSemanticSearchCandidates(candidates, map[string]string{
		"vector_source":          "persisted_local",
		"vector_retrieval_state": "ready",
	})
	return semanticSearchIndexResult{
		Candidates:           candidates,
		IndexedDocumentCount: entry.indexedDocumentCount,
		CorpusLimit:          h.Config.CorpusLimit,
		CorpusMayBeTruncated: entry.corpusMayBeTruncated,
		RetrievalState:       semanticSearchActiveRetrievalState(query.Request.Mode, candidates),
	}, nil
}

func (h *PersistedLocalSemanticSearchHybrid) snapshotRequest(scopeID string) SemanticSearchSnapshotRequest {
	return SemanticSearchSnapshotRequest{
		ScopeID:            scopeID,
		ProviderProfileID:  h.Config.ProviderProfileID,
		SourceClass:        h.Config.SourceClass,
		EmbeddingModelID:   h.Config.EmbeddingModelID,
		VectorIndexVersion: h.Config.VectorIndexVersion,
	}
}

func (h *PersistedLocalSemanticSearchHybrid) cacheKey(
	query semanticSearchIndexQuery,
	snapshot SemanticSearchSnapshot,
) semanticSearchIndexCacheKey {
	return semanticSearchIndexCacheKey{
		ScopeID:                    query.ScopeID,
		RepoID:                     query.RepoID,
		GenerationID:               snapshot.GenerationID,
		ProviderProfileID:          h.Config.ProviderProfileID,
		SourceClass:                h.Config.SourceClass,
		EmbeddingModelID:           h.Config.EmbeddingModelID,
		VectorIndexVersion:         h.Config.VectorIndexVersion,
		VectorRetrieval:            h.Config.VectorRetrieval,
		FilterSignature:            semanticSearchCacheFilterSignature(query),
		DocumentProjectionRevision: snapshot.DocumentProjectionRevision,
		DocumentCount:              snapshot.DocumentCount,
		VectorProjectionRevision:   snapshot.VectorProjectionRevision,
		VectorBuildFence:           snapshot.VectorBuildFence,
		CorpusLimit:                h.Config.CorpusLimit,
	}
}

func semanticSearchCacheFilterSignature(query semanticSearchIndexQuery) string {
	values := make([]string, 0, len(query.SourceKinds)+len(query.Languages))
	for _, kind := range query.SourceKinds {
		values = append(values, "source:"+string(kind))
	}
	for _, language := range query.Languages {
		values = append(values, "language:"+strings.ToLower(strings.TrimSpace(language)))
	}
	sort.Strings(values)
	var builder strings.Builder
	previous := ""
	for _, value := range values {
		if value == previous {
			continue
		}
		_, _ = fmt.Fprintf(&builder, "%d:%s;", len(value), value)
		previous = value
	}
	return builder.String()
}

func annotateSemanticSearchIndexCache(ctx context.Context, state string) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(attribute.String("search.index_cache", state))
}
