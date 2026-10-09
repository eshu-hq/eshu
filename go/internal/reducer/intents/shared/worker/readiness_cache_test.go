// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

func readinessCacheTestKey(scope, unit, run, gen string) gpphase.PhaseKey {
	return gpphase.PhaseKey{
		ScopeID: scope, AcceptanceUnitID: unit, SourceRunID: run,
		GenerationID: gen, Keyspace: gpphase.KeyspaceCodeEntitiesUID,
	}
}

const readinessCacheTestPhase = gpphase.PhaseCanonicalNodesCommitted

// TestRoundReadinessCacheServesSecondRoundFromCache proves the #7724 1B
// cross-round cache: keys answered in round one are not re-queried in
// round two, and the merged lookup still answers them.
func TestRoundReadinessCacheServesSecondRoundFromCache(t *testing.T) {
	t.Parallel()

	var innerCalls [][]gpphase.PhaseKey
	inner := func(_ context.Context, keys []gpphase.PhaseKey, _ gpphase.Phase) (gpphase.ReadinessLookup, error) {
		innerCalls = append(innerCalls, keys)
		return func(key gpphase.PhaseKey, _ gpphase.Phase) (bool, bool) {
			if key.ScopeID == "scope-a" {
				return true, true
			}
			return false, false
		}, nil
	}

	cache := newRoundReadinessCache(inner)
	ctx := context.Background()
	round1, err := cache.prefetch(ctx, []gpphase.PhaseKey{readinessCacheTestKey("scope-a", "unit-a", "run-1", "gen-1")}, readinessCacheTestPhase)
	if err != nil {
		t.Fatalf("round 1 prefetch() error = %v", err)
	}
	round2, err := cache.prefetch(ctx, []gpphase.PhaseKey{readinessCacheTestKey("scope-a", "unit-a", "run-1", "gen-1")}, readinessCacheTestPhase)
	if err != nil {
		t.Fatalf("round 2 prefetch() error = %v", err)
	}

	if len(innerCalls) != 2 {
		t.Fatalf("inner prefetch called %d times, want 2 (once per round, second with no new keys)", len(innerCalls))
	}
	if len(innerCalls[1]) != 0 {
		t.Fatalf("round 2 queried %d keys, want 0 (all cached)", len(innerCalls[1]))
	}
	ready, found := round2(readinessCacheTestKey("scope-a", "unit-a", "run-1", "gen-1"), readinessCacheTestPhase)
	if !found || !ready {
		t.Fatalf("round 2 lookup = (%v, %v), want (true, true)", ready, found)
	}
	if ready, found := round1(readinessCacheTestKey("scope-a", "unit-a", "run-1", "gen-1"), readinessCacheTestPhase); !found || !ready {
		t.Fatalf("round 1 lookup = (%v, %v), want (true, true)", ready, found)
	}
}

// TestRoundReadinessCacheQueriesOnlyNewKeys proves widen rounds fetch only
// the delta: round one's keys are served from cache while new keys go to
// the store, and both answer through the merged lookup.
func TestRoundReadinessCacheQueriesOnlyNewKeys(t *testing.T) {
	t.Parallel()

	var innerCalls [][]gpphase.PhaseKey
	inner := func(_ context.Context, keys []gpphase.PhaseKey, _ gpphase.Phase) (gpphase.ReadinessLookup, error) {
		innerCalls = append(innerCalls, keys)
		return func(key gpphase.PhaseKey, _ gpphase.Phase) (bool, bool) {
			return key.ScopeID == "scope-b", true
		}, nil
	}

	cache := newRoundReadinessCache(inner)
	ctx := context.Background()
	keyA := readinessCacheTestKey("scope-a", "unit-a", "run-1", "gen-1")
	keyB := readinessCacheTestKey("scope-b", "unit-b", "run-1", "gen-1")
	if _, err := cache.prefetch(ctx, []gpphase.PhaseKey{keyA}, readinessCacheTestPhase); err != nil {
		t.Fatalf("round 1 prefetch() error = %v", err)
	}
	round2, err := cache.prefetch(ctx, []gpphase.PhaseKey{keyA, keyB}, readinessCacheTestPhase)
	if err != nil {
		t.Fatalf("round 2 prefetch() error = %v", err)
	}

	if len(innerCalls) != 2 {
		t.Fatalf("inner prefetch called %d times, want 2", len(innerCalls))
	}
	if len(innerCalls[1]) != 1 || innerCalls[1][0] != keyB {
		t.Fatalf("round 2 queried %v, want only keyB", innerCalls[1])
	}
	if ready, found := round2(keyA, readinessCacheTestPhase); !found || ready {
		t.Fatalf("round 2 lookup(keyA) = (%v, %v), want cached (false, true)", ready, found)
	}
	if ready, found := round2(keyB, readinessCacheTestPhase); !found || !ready {
		t.Fatalf("round 2 lookup(keyB) = (%v, %v), want fresh (true, true)", ready, found)
	}
}

// TestRoundReadinessCacheNormalizesKeys proves padded keys hit the same
// cache entry as their trimmed form.
func TestRoundReadinessCacheNormalizesKeys(t *testing.T) {
	t.Parallel()

	var innerCalls [][]gpphase.PhaseKey
	inner := func(_ context.Context, keys []gpphase.PhaseKey, _ gpphase.Phase) (gpphase.ReadinessLookup, error) {
		innerCalls = append(innerCalls, keys)
		return func(gpphase.PhaseKey, gpphase.Phase) (bool, bool) { return true, true }, nil
	}

	cache := newRoundReadinessCache(inner)
	ctx := context.Background()
	trimmed := readinessCacheTestKey("scope-a", "unit-a", "run-1", "gen-1")
	padded := readinessCacheTestKey("  scope-a ", "unit-a\t", "run-1", "gen-1")
	if _, err := cache.prefetch(ctx, []gpphase.PhaseKey{trimmed}, readinessCacheTestPhase); err != nil {
		t.Fatalf("round 1 prefetch() error = %v", err)
	}
	round2, err := cache.prefetch(ctx, []gpphase.PhaseKey{padded}, readinessCacheTestPhase)
	if err != nil {
		t.Fatalf("round 2 prefetch() error = %v", err)
	}
	if len(innerCalls) != 2 {
		t.Fatalf("inner prefetch called %d times, want 2", len(innerCalls))
	}
	if len(innerCalls[1]) != 0 {
		t.Fatalf("round 2 queried %d keys, want 0 (padded key hits the trimmed entry)", len(innerCalls[1]))
	}
	if ready, found := round2(padded, readinessCacheTestPhase); !found || !ready {
		t.Fatalf("round 2 lookup(padded) = (%v, %v), want (true, true)", ready, found)
	}
	if ready, found := round2(trimmed, readinessCacheTestPhase); !found || !ready {
		t.Fatalf("round 2 lookup(trimmed) = (%v, %v), want (true, true)", ready, found)
	}
}

// TestRoundReadinessCacheErrorFailsWithoutPoisoning proves an inner error
// fails the prefetch and caches nothing: the next round retries the keys.
func TestRoundReadinessCacheErrorFailsWithoutPoisoning(t *testing.T) {
	t.Parallel()

	calls := 0
	inner := func(_ context.Context, keys []gpphase.PhaseKey, _ gpphase.Phase) (gpphase.ReadinessLookup, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("boom")
		}
		return func(gpphase.PhaseKey, gpphase.Phase) (bool, bool) { return true, true }, nil
	}

	cache := newRoundReadinessCache(inner)
	ctx := context.Background()
	key := readinessCacheTestKey("scope-a", "unit-a", "run-1", "gen-1")
	if _, err := cache.prefetch(ctx, []gpphase.PhaseKey{key}, readinessCacheTestPhase); err == nil {
		t.Fatal("prefetch() error = nil on inner failure, want error")
	}
	round2, err := cache.prefetch(ctx, []gpphase.PhaseKey{key}, readinessCacheTestPhase)
	if err != nil {
		t.Fatalf("round 2 prefetch() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("inner prefetch called %d times, want 2 (retry after error)", calls)
	}
	if ready, found := round2(key, readinessCacheTestPhase); !found || !ready {
		t.Fatalf("round 2 lookup = (%v, %v), want (true, true)", ready, found)
	}
}

// TestRoundReadinessCacheRecordsHits proves cache hits land in the context
// stats (#7724 telemetry: per-visit cache-hits).
func TestRoundReadinessCacheRecordsHits(t *testing.T) {
	t.Parallel()

	inner := func(_ context.Context, _ []gpphase.PhaseKey, _ gpphase.Phase) (gpphase.ReadinessLookup, error) {
		return func(gpphase.PhaseKey, gpphase.Phase) (bool, bool) { return false, true }, nil
	}

	var stats sharedintent.PrefetchStats
	ctx := sharedintent.ContextWithPrefetchStats(context.Background(), &stats)
	cache := newRoundReadinessCache(inner)
	key := readinessCacheTestKey("scope-a", "unit-a", "run-1", "gen-1")
	if _, err := cache.prefetch(ctx, []gpphase.PhaseKey{key}, readinessCacheTestPhase); err != nil {
		t.Fatalf("round 1 prefetch() error = %v", err)
	}
	if _, err := cache.prefetch(ctx, []gpphase.PhaseKey{key}, readinessCacheTestPhase); err != nil {
		t.Fatalf("round 2 prefetch() error = %v", err)
	}

	if stats.Readiness.CacheHits != 1 {
		t.Fatalf("readiness cache hits = %d, want 1", stats.Readiness.CacheHits)
	}
}
