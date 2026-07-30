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
// Non-serialization proof (issue #5849): the first probe call blocks (via
// firstProbeEntered/secondProbeEntered) until a second, independent probe
// call arrives or overlapTimeout elapses. This does NOT rest on "32
// concurrent callers guarantee overlap" — that premise is false: c.loading
// (identity_epoch_cache.go) goes non-nil only after the leader's probe
// returns, so a straggler goroutine that reaches the loading check after
// that point correctly skips probing, and how many of the 32 callers race in
// before then is scheduling-dependent (can legitimately be 31, not 32, under
// -race on a loaded host). What actually guarantees overlap under the fixed
// code is that mu is released before the probe: the leader blocks inside the
// probe holding no lock, so every other caller can reach its own probe call
// unimpeded by mu, and the leader's generous timeout gives one of them time
// to arrive as the second entrant. Under the seeded P1-A bug (mu held across
// the probe), the leader blocks while still holding mu, so no other caller
// can even reach QueryContext — the handshake never completes and the test
// fails deterministically, every run.
type concurrentSingleflightQueryer struct {
	mu                 sync.Mutex
	probeCalls         int
	loadCalls          int
	firstProbeEntered  chan struct{}
	secondProbeEntered chan struct{}
	overlapTimeout     time.Duration
	overlapAchieved    bool
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

	// Probe query: a deterministic two-probe handshake proves overlap
	// directly instead of inferring it from a peak-count sample or elapsed
	// wall-clock time. The first probe call to arrive blocks until a second,
	// independent probe call arrives (proving mu was not held across the
	// first call) or overlapTimeout elapses (deadlock/serialization
	// detector, not a timing assertion).
	q.mu.Lock()
	q.probeCalls++
	n := q.probeCalls
	q.mu.Unlock()

	switch n {
	case 1:
		close(q.firstProbeEntered)
		select {
		case <-q.secondProbeEntered:
			q.mu.Lock()
			q.overlapAchieved = true
			q.mu.Unlock()
		case <-time.After(q.overlapTimeout):
			// Leave overlapAchieved false: no second probe arrived in time,
			// meaning mu (or some other chokepoint) serialized the callers.
		}
	case 2:
		close(q.secondProbeEntered)
	}

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

	// overlapTimeout is a deadlock/serialization detector, not a timing
	// assertion: it only needs to be long enough for a goroutine scheduled
	// on a loaded host to reach its probe call. Kept generous (seconds, not
	// milliseconds) so it never contributes to flakiness; a real
	// serialization bug (the seeded P1-A regression proven below) fails this
	// test on every run regardless of how long the timeout is.
	const overlapTimeout = 5 * time.Second
	q := &concurrentSingleflightQueryer{
		firstProbeEntered:  make(chan struct{}),
		secondProbeEntered: make(chan struct{}),
		overlapTimeout:     overlapTimeout,
		factRow:            factRow,
		probeRow:           []any{int64(1), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ""},
	}

	store := newFactStoreWithCache(q, 0)

	const numGoroutines = 32
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	results := make([][]facts.Envelope, numGoroutines)
	errs := make([]error, numGoroutines)

	for i := range numGoroutines {
		go func(idx int) {
			defer wg.Done()
			loaded, err := store.ListActiveContainerImageIdentityFacts(context.Background())
			results[idx] = loaded
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	for i := range numGoroutines {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if len(results[i]) != 1 {
			t.Fatalf("goroutine %d: len = %d, want 1", i, len(results[i]))
		}
	}

	q.mu.Lock()
	loadCalls, probeCalls, overlapAchieved := q.loadCalls, q.probeCalls, q.overlapAchieved
	q.mu.Unlock()
	if loadCalls != 1 {
		t.Fatalf("load page queries = %d, want 1 (singleflight)", loadCalls)
	}
	t.Logf("probeCalls=%d loadCalls=%d overlapAchieved=%v", probeCalls, loadCalls, overlapAchieved)

	// Non-serialization proof (issue #5849 fix): assert the real invariant —
	// mu is not held across the epoch probe — via a deterministic two-probe
	// handshake instead of a scheduling-dependent peak-count sample or a
	// wall-clock bound. This replaces an exact-equality bound
	// (peakInFlightProbes == numGoroutines) rejected on review: c.loading
	// only goes non-nil after the leader's probe returns, so a straggler
	// goroutine that reaches the loading check after that point correctly
	// skips probing, and exactly how many of the 32 callers race in before
	// then is scheduling-dependent — a busy host can legitimately leave it
	// at 31 on fully correct code. The handshake sidesteps that: it only
	// requires that a second, independent probe call arrive while the first
	// is still outstanding, which the fixed code guarantees deterministically
	// (mu is free during the leader's probe, so every other caller can reach
	// its own probe call) and the seeded P1-A bug fails deterministically
	// (mu is held, so no other caller can even reach QueryContext).
	if !overlapAchieved {
		t.Fatalf("epoch probes never overlapped within %v (mu must not serialize the epoch probe across any caller)", overlapTimeout)
	}
}
