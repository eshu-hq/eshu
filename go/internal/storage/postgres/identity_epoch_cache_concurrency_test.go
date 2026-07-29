// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// concurrentSingleflightQueryer is a query-text-routing fake used only by
// TestIdentityEpochCacheConcurrentSingleflight. The corrected get() (issue
// #5438 P1-A: mu released across the epoch probe) legitimately lets many
// concurrent callers probe in parallel before exactly one wins the
// double-checked-locking loading-flag race and becomes the reload leader —
// so the number and interleaving of probe calls is not deterministic. The
// shared fakeExecQueryer's strict FIFO response queue assumes a fixed call
// order and breaks under that real race (a probe call can consume a
// load-page response or vice versa, failing on a Scan column-count
// mismatch — reproduced before writing this fake). This fake instead
// returns a stable probe row on every probe call (idempotent, matching a
// stable epoch) and fails loudly if the load-page query — which singleflight
// must serialize to exactly one caller — is ever issued more than once.
//
// probeDelay simulates a DB round-trip. inFlightProbes/peakInFlightProbes
// count, at every instant, how many probe calls are concurrently inside that
// round-trip — a direct, wall-clock-free measurement of non-serialization
// (issue #5849): under the P1-A bug, probes fully serialize behind mu,
// pinning peakInFlightProbes at 1 regardless of host scheduling; under the
// fix, mu is released before the probe, so 32 concurrent callers guarantee
// overlap. This replaces a prior wall-clock bound that could fail under host
// load even with fully correct, non-serialized behavior.
type concurrentSingleflightQueryer struct {
	mu                 sync.Mutex
	probeCalls         int
	loadCalls          int
	inFlightProbes     int
	peakInFlightProbes int
	probeDelay         time.Duration
	factRow            []any
	probeRow           []any
}

func (q *concurrentSingleflightQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("concurrentSingleflightQueryer: unexpected ExecContext call")
}

func (q *concurrentSingleflightQueryer) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if strings.Contains(query, "LIMIT") {
		q.mu.Lock()
		q.loadCalls++
		n := q.loadCalls
		q.mu.Unlock()
		if n > 1 {
			return nil, fmt.Errorf("load page query called %d times, want at most 1 (singleflight broken)", n)
		}
		return &queueFakeRows{rows: [][]any{q.factRow}}, nil
	}

	// Probe query: simulate a DB round-trip, tracking peak concurrency across
	// the simulated round-trip so the test can assert non-serialization
	// directly instead of inferring it from elapsed wall-clock time.
	q.mu.Lock()
	q.inFlightProbes++
	if q.inFlightProbes > q.peakInFlightProbes {
		q.peakInFlightProbes = q.inFlightProbes
	}
	q.mu.Unlock()

	if q.probeDelay > 0 {
		time.Sleep(q.probeDelay)
	}

	q.mu.Lock()
	q.probeCalls++
	q.inFlightProbes--
	q.mu.Unlock()
	return &queueFakeRows{rows: [][]any{q.probeRow}}, nil
}

func TestIdentityEpochCacheConcurrentSingleflight(t *testing.T) {
	t.Parallel()

	factRow := []any{
		"fact-1", "scope-1", "gen-1",
		"oci_registry.image_tag_observation", "stable-key-1", "1.0.0",
		"oci_registry", int64(0), "reported", "oci_registry",
		"source-key-1", "uri://example", "rec-1",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		false,
		[]byte(`{}`),
	}

	const probeDelay = 20 * time.Millisecond
	q := &concurrentSingleflightQueryer{
		probeDelay: probeDelay,
		factRow:    factRow,
		probeRow:   []any{int64(1), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ""},
	}

	store := newFactStoreWithCache(q, 0)

	const numGoroutines = 32
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	results := make([][]facts.Envelope, numGoroutines)
	errs := make([]error, numGoroutines)

	start := time.Now()
	for i := range numGoroutines {
		go func(idx int) {
			defer wg.Done()
			loaded, err := store.ListActiveContainerImageIdentityFacts(context.Background())
			results[idx] = loaded
			errs[idx] = err
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	for i := range numGoroutines {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if len(results[i]) != 1 {
			t.Fatalf("goroutine %d: len = %d, want 1", i, len(results[i]))
		}
	}

	q.mu.Lock()
	loadCalls, probeCalls, peakInFlight := q.loadCalls, q.probeCalls, q.peakInFlightProbes
	q.mu.Unlock()
	if loadCalls != 1 {
		t.Fatalf("load page queries = %d, want 1 (singleflight)", loadCalls)
	}
	t.Logf("probeCalls=%d loadCalls=%d peakInFlightProbes=%d elapsed=%v (probeDelay=%v, logged for diagnostics only, not asserted)",
		probeCalls, loadCalls, peakInFlight, elapsed, probeDelay)

	// Non-serialization proof (issue #5849 fix): assert directly, via the
	// observed peak in-flight probe count, that concurrent callers' epoch
	// probes overlap — not by inferring it from elapsed wall-clock time. A
	// busy host can delay every goroutine's wake-up by an unbounded amount
	// regardless of whether mu serializes the probe, so any fixed multiple
	// of probeDelay can fail on correct code (reproduced for #5849).
	//
	// The bound is exact equality, not merely ">1": the only gate before the
	// probe call is a sub-microsecond uncontended mutex check against an
	// empty cache, far faster than the 20ms probeDelay, so every one of the
	// numGoroutines callers necessarily reaches its probe call before the
	// first one returns — there is no chokepoint that could let some
	// callers through while serializing others. Measured across 200+ runs
	// each way: peakInFlightProbes == numGoroutines on every correct run,
	// == 1 on every run of the seeded P1-A bug, never anything in between.
	// A looser bound (e.g. ">1") would go green on a run showing only 2 of
	// 32 probes overlapping, which would actually indicate a different,
	// subtler serialization regression than the one this test guards.
	if peakInFlight != numGoroutines {
		t.Fatalf("peak concurrent probes = %d, want %d (mu must not serialize the epoch probe across any caller)", peakInFlight, numGoroutines)
	}
}
