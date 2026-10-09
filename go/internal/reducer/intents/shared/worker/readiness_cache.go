// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// roundReadinessCacheKey is one normalized (phase key, phase) pair. Fields
// are trimmed at insert and at lookup so padded keys hit the same entry
// as their trimmed form, matching the store prefetch closures.
type roundReadinessCacheKey struct {
	key   gpphase.PhaseKey
	phase gpphase.Phase
}

// roundReadinessAnswer is one cached readiness verdict.
type roundReadinessAnswer struct {
	ready bool
	found bool
}

// roundReadinessCache accumulates readiness answers across the widen rounds
// of a single SelectPartitionBatch call (#7724 1B). Each widen round
// re-scans a superset of the previous window, so without the cache every
// round re-queries every key it already answered; with the cache only the
// delta (keys never answered before) reaches the store.
//
// Safety: phase rows are publish-once commit markers, so a cached
// ready=true cannot go stale-false, and a cached not-ready only defers a
// row one cycle (blocked rows are never completed). The cache MUST NOT
// serve the #7121 drainSupersededBlockedRows re-read: that read exists to
// catch a producer that published between the first readiness read and the
// superseded lookup, and serving it from cache reintroduces the exact
// publish-between-reads race. The drain always receives the raw prefetch.
// Acceptance answers are never cached either: acceptance advances on
// generation activation, and a stale cached acceptance can misjudge a
// new-generation row as stale, completing it and losing its edge
// permanently.
//
// One cache serves one selection call from its single goroutine; it is not
// safe for concurrent use.
type roundReadinessCache struct {
	inner  gpphase.ReadinessPrefetch
	cached map[roundReadinessCacheKey]roundReadinessAnswer
}

// newRoundReadinessCache wraps inner with a cross-round answer cache. The
// caller must only wrap a non-nil prefetch.
func newRoundReadinessCache(inner gpphase.ReadinessPrefetch) *roundReadinessCache {
	return &roundReadinessCache{inner: inner, cached: make(map[roundReadinessCacheKey]roundReadinessAnswer)}
}

// prefetch implements gpphase.ReadinessPrefetch: it queries the inner
// prefetch for keys never answered before and returns a lookup that merges
// the cached answers with this round's fresh ones. Hits are recorded into
// the context stats. An inner error fails the call and caches nothing, so
// the next round retries the keys.
func (c *roundReadinessCache) prefetch(
	ctx context.Context,
	keys []gpphase.PhaseKey,
	phase gpphase.Phase,
) (gpphase.ReadinessLookup, error) {
	normalizedPhase := gpphase.Phase(strings.TrimSpace(string(phase)))
	var toQuery []gpphase.PhaseKey
	queued := make(map[roundReadinessCacheKey]struct{}, len(keys))
	hits := 0
	for _, key := range keys {
		normalized := normalizeReadinessCacheKey(key)
		entry := roundReadinessCacheKey{key: normalized, phase: normalizedPhase}
		if _, ok := c.cached[entry]; ok {
			hits++
			continue
		}
		if _, dup := queued[entry]; dup {
			continue
		}
		queued[entry] = struct{}{}
		// Invalid keys pass through uncached: the inner prefetch owns
		// the invalid-key contract (it skips them without a query),
		// and this round's lookup below answers them from the inner
		// result, exactly as without the cache.
		if normalized.Validate() != nil {
			toQuery = append(toQuery, key)
			continue
		}
		toQuery = append(toQuery, normalized)
	}

	innerLookup, err := c.inner(ctx, toQuery, phase)
	if err != nil {
		return nil, err
	}
	for _, key := range toQuery {
		normalized := normalizeReadinessCacheKey(key)
		if normalized.Validate() != nil {
			continue
		}
		ready, found := innerLookup(normalized, phase)
		c.cached[roundReadinessCacheKey{key: normalized, phase: normalizedPhase}] = roundReadinessAnswer{ready: ready, found: found}
	}
	sharedintent.RecordPrefetch(ctx, sharedintent.PrefetchKindReadiness, 0, 0, 0, hits, 0)

	return func(key gpphase.PhaseKey, phase gpphase.Phase) (bool, bool) {
		normalized := normalizeReadinessCacheKey(key)
		if answer, ok := c.cached[roundReadinessCacheKey{key: normalized, phase: gpphase.Phase(strings.TrimSpace(string(phase)))}]; ok {
			return answer.ready, answer.found
		}
		return innerLookup(key, phase)
	}, nil
}

// normalizeReadinessCacheKey trims a phase key's fields for cache
// comparison, mirroring the store prefetch closures.
func normalizeReadinessCacheKey(key gpphase.PhaseKey) gpphase.PhaseKey {
	key.ScopeID = strings.TrimSpace(key.ScopeID)
	key.AcceptanceUnitID = strings.TrimSpace(key.AcceptanceUnitID)
	key.SourceRunID = strings.TrimSpace(key.SourceRunID)
	key.GenerationID = strings.TrimSpace(key.GenerationID)
	key.Keyspace = gpphase.Keyspace(strings.TrimSpace(string(key.Keyspace)))
	return key
}
