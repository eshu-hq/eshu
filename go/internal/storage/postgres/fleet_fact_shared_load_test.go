// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// countingFleetQueryer is a routing-free fake for the #9697 shared-load
// regression: every QueryContext call serves the same single envelope row and
// counts the call. Concurrent callers racing one fleet-wide load must share
// the leader's single paginated pass, so 16 concurrent intents cost exactly 1
// underlying query sequence instead of 16.
type countingFleetQueryer struct {
	mu    sync.Mutex
	calls int
	row   []any
	// rowForCall, when set, serves a per-call row so sequential-freshness
	// tests can move the database state between calls.
	rowForCall func(call int) []any
}

func (q *countingFleetQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("countingFleetQueryer: unexpected ExecContext call")
}

func (q *countingFleetQueryer) QueryContext(ctx context.Context, _ string, _ ...any) (db.Rows, error) {
	q.mu.Lock()
	q.calls++
	call := q.calls
	q.mu.Unlock()
	row := q.row
	if q.rowForCall != nil {
		row = q.rowForCall(call)
	}
	if call == 1 {
		// Dwell the leader's pass so every spawned goroutine parks on the
		// flight instead of arriving after it closed: without this the
		// instant fake lets stragglers start their own passes, which would
		// fail the test for scheduling reasons rather than sharing reasons.
		// The dwell is overlap margin, not a timing assertion — 500ms
		// against microsecond-scale flight joins.
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &queueFakeRows{rows: [][]any{row}}, nil
}

// TestSharedFleetLoadServesFreshTruthSequentially pins the #9697 P0
// invalidation contract: the group retains nothing, so a second sequential
// call re-reads the database and sees the moved generation instead of a
// stale cached set.
func TestSharedFleetLoadServesFreshTruthSequentially(t *testing.T) {
	t.Parallel()

	slsaRow := func(factID string) []any {
		return []any{
			factID, "scope-sbom", "gen-1",
			"attestation.slsa_provenance", "stable-key-1", "1.0.0",
			"sbom_attestation", int64(0), "reported", "sbom_attestation",
			"source-key-1", "uri://example", "rec-1",
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			false,
			[]byte(`{}`),
		}
	}
	q := &countingFleetQueryer{
		rowForCall: func(call int) []any {
			return slsaRow(fmt.Sprintf("fact-slsa-%d", call))
		},
	}
	store := NewFactStore(q)

	first, err := store.ListActiveContainerImageSLSAFacts(context.Background())
	if err != nil {
		t.Fatalf("first ListActiveContainerImageSLSAFacts: %v", err)
	}
	second, err := store.ListActiveContainerImageSLSAFacts(context.Background())
	if err != nil {
		t.Fatalf("second ListActiveContainerImageSLSAFacts: %v", err)
	}
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("lens = %d/%d, want 1/1", len(first), len(second))
	}
	if first[0].FactID == second[0].FactID {
		t.Fatalf("second call FactID = %q, same as first: the group must not retain rows across calls",
			second[0].FactID)
	}
	if got, want := second[0].FactID, "fact-slsa-2"; got != want {
		t.Fatalf("second call FactID = %q, want %q (fresh database state)", got, want)
	}
}

func (q *countingFleetQueryer) queryCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.calls
}

// TestSharedFleetLoadCoalescesConcurrentSLSALoads is the #9697 failing-first
// regression for the per-intent fleet-wide SLSA reloads: 16 workers racing
// the same cross-scope SLSA set must share one underlying load.
func TestSharedFleetLoadCoalescesConcurrentSLSALoads(t *testing.T) {
	t.Parallel()

	row := []any{
		"fact-slsa-1", "scope-sbom", "gen-1",
		"attestation.slsa_provenance", "stable-key-1", "1.0.0",
		"sbom_attestation", int64(0), "reported", "sbom_attestation",
		"source-key-1", "uri://example", "rec-1",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		false,
		[]byte(`{}`),
	}
	q := &countingFleetQueryer{row: row}
	store := NewFactStore(q)

	const numGoroutines = 16
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	errs := make([]error, numGoroutines)
	counts := make([]int, numGoroutines)
	for i := range numGoroutines {
		go func(idx int) {
			defer wg.Done()
			loaded, err := store.ListActiveContainerImageSLSAFacts(context.Background())
			errs[idx] = err
			counts[idx] = len(loaded)
		}(i)
	}
	wg.Wait()

	for i := range numGoroutines {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if counts[i] != 1 {
			t.Fatalf("goroutine %d: len = %d, want 1", i, counts[i])
		}
	}
	if got, want := q.queryCount(), 1; got != want {
		t.Fatalf("underlying SLSA page queries = %d, want %d: concurrent fleet loads must share one pass",
			got, want)
	}
}

// TestSharedFleetLoadCoalescesConcurrentRepositoryCatalogLoads pins the same
// #9697 bound for the cross-scope repository catalog: 16 workers racing the
// catalog share one underlying paginated pass.
func TestSharedFleetLoadCoalescesConcurrentRepositoryCatalogLoads(t *testing.T) {
	t.Parallel()

	row := []any{
		"fact-repo-1", "scope-git", "gen-1",
		"repository", "stable-key-1", "1.0.0",
		"git", int64(0), "reported", "git",
		"source-key-1", "uri://example", "rec-1",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		false,
		[]byte(`{"graph_id":"repo-1","name":"r"}`),
	}
	q := &countingFleetQueryer{row: row}
	store := NewFactStore(q)

	const numGoroutines = 16
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	errs := make([]error, numGoroutines)
	counts := make([]int, numGoroutines)
	for i := range numGoroutines {
		go func(idx int) {
			defer wg.Done()
			loaded, err := store.ListActiveRepositoryFacts(context.Background())
			errs[idx] = err
			counts[idx] = len(loaded)
		}(i)
	}
	wg.Wait()

	for i := range numGoroutines {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if counts[i] != 1 {
			t.Fatalf("goroutine %d: len = %d, want 1", i, counts[i])
		}
	}
	if got, want := q.queryCount(), 1; got != want {
		t.Fatalf("underlying catalog page queries = %d, want %d: concurrent fleet loads must share one pass",
			got, want)
	}
}
