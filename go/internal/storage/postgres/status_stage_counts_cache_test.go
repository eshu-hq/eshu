// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"
)

// stageCountsCacheFakeQueryer answers stageCountsQuery only and counts how many
// times it was actually asked to run the query, so tests can assert the cache
// is suppressing repeat Postgres round trips rather than trusting call-site
// behavior.
type stageCountsCacheFakeQueryer struct {
	rows  [][]any
	calls int
}

func (q *stageCountsCacheFakeQueryer) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if query != stageCountsQuery {
		return nil, errUnexpectedStageCountsCacheQuery
	}
	q.calls++
	return &fakeRows{rows: q.rows}, nil
}

var errUnexpectedStageCountsCacheQuery = errUnexpectedQuery("unexpected query for stageCountsCacheFakeQueryer")

type errUnexpectedQuery string

func (e errUnexpectedQuery) Error() string { return string(e) }

// TestListStageCountsCacheServesRepeatReadsWithinTTL is the #4446 TDD
// regression for the caching half of the issue: listStageCounts must not
// re-run stageCountsQuery (which embeds activeFactWorkItemsCTE, the exact
// join the issue names as expensive) on every call within the cache TTL.
// StageStatusCount carries no asOf-relative fields (see status.go), so
// caching this specific query's row set introduces no staleness risk to any
// consumer.
func TestListStageCountsCacheServesRepeatReadsWithinTTL(t *testing.T) {
	t.Parallel()

	queryer := &stageCountsCacheFakeQueryer{
		rows: [][]any{
			{"reducer", "pending", int64(3)},
		},
	}
	store := NewStatusStore(queryer)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	store.stageCountsCache.nowFunc = func() time.Time { return now }

	first, err := listStageCounts(context.Background(), store.queryer, store.stageCountsCache)
	if err != nil {
		t.Fatalf("listStageCounts() first call error = %v, want nil", err)
	}
	if queryer.calls != 1 {
		t.Fatalf("queryer.calls after first read = %d, want 1", queryer.calls)
	}

	// Second read, still within TTL: must be served from cache, not Postgres.
	second, err := listStageCounts(context.Background(), store.queryer, store.stageCountsCache)
	if err != nil {
		t.Fatalf("listStageCounts() second call error = %v, want nil", err)
	}
	if queryer.calls != 1 {
		t.Fatalf("queryer.calls after cached read = %d, want 1 (cache should have served the read)", queryer.calls)
	}
	if len(second) != len(first) || second[0] != first[0] {
		t.Fatalf("listStageCounts() cached result = %+v, want %+v", second, first)
	}

	// Advance past the TTL: the next read must go back to Postgres.
	now = now.Add(statusStageCountsCacheTTL + time.Millisecond)
	if _, err := listStageCounts(context.Background(), store.queryer, store.stageCountsCache); err != nil {
		t.Fatalf("listStageCounts() post-TTL call error = %v, want nil", err)
	}
	if queryer.calls != 2 {
		t.Fatalf("queryer.calls after TTL expiry = %d, want 2 (cache should have expired)", queryer.calls)
	}
}

// TestListStageCountsCachePropagatesQueryErrors proves a Postgres error is
// never cached: a failing read must not poison subsequent calls within the
// TTL window, and the caller must still see the error.
func TestListStageCountsCachePropagatesQueryErrors(t *testing.T) {
	t.Parallel()

	queryer := &erroringQueryer{err: errStageCountsCacheProbe}
	store := NewStatusStore(queryer)

	if _, err := listStageCounts(context.Background(), store.queryer, store.stageCountsCache); err == nil {
		t.Fatal("listStageCounts() error = nil, want error")
	}

	// A cache that swallowed the error would report a "hit" on stale/empty
	// state here; assert the cache stayed cold instead.
	store.stageCountsCache.mu.Lock()
	warm := store.stageCountsCache.warm
	store.stageCountsCache.mu.Unlock()
	if warm {
		t.Fatal("stageCountsCache.warm = true after an errored read, want false (errors must not populate the cache)")
	}
}

type erroringQueryer struct {
	err error
}

func (q *erroringQueryer) QueryContext(_ context.Context, _ string, _ ...any) (Rows, error) {
	return nil, q.err
}

var errStageCountsCacheProbe = errUnexpectedQuery("stage counts cache probe error")
