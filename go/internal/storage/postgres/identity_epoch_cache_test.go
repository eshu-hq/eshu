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

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// testInstruments creates a test-only telemetry.Instruments for cache tests.
func testInstruments() *telemetry.Instruments {
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider().Meter("identity-cache-test"))
	if err != nil {
		panic("telemetry.NewInstruments in test: " + err.Error())
	}
	return inst
}

// probeQueryRow returns a single-row fake response for the epoch probe:
// count, COALESCE(max(observed_at), ...), active_fingerprint. fingerprint is
// a string (md5 digest in production; tests use short literals for clarity).
func probeQueryRow(count int, maxObservedAt time.Time, fingerprint string) queueFakeRows {
	return queueFakeRows{
		rows: [][]any{{
			int64(count),
			maxObservedAt,
			fingerprint,
		}},
	}
}

// newFactStoreWithCache creates a FactStore with a wired identity cache for testing.
func newFactStoreWithCache(db ExecQueryer, maxBytes int64) *FactStore {
	cache, err := NewIdentityEpochCache(testInstruments(), maxBytes)
	if err != nil {
		panic("NewIdentityEpochCache in test: " + err.Error())
	}
	return &FactStore{
		db:            db,
		identityCache: cache,
	}
}

func TestIdentityEpochCacheHit(t *testing.T) {
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

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ""),
			{rows: [][]any{factRow}},
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ""),
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ""),
		},
	}

	store := newFactStoreWithCache(db, 0)

	loaded1, err := store.ListActiveContainerImageIdentityFacts(context.Background())
	if err != nil {
		t.Fatalf("first ListActiveContainerImageIdentityFacts: %v", err)
	}
	if len(loaded1) != 1 {
		t.Fatalf("first call len = %d, want 1", len(loaded1))
	}

	loaded2, err := store.ListActiveContainerImageIdentityFacts(context.Background())
	if err != nil {
		t.Fatalf("second ListActiveContainerImageIdentityFacts: %v", err)
	}
	if len(loaded2) != 1 {
		t.Fatalf("second call len = %d, want 1", len(loaded2))
	}

	var loadQueries int
	for _, q := range db.queries {
		if strings.Contains(q.query, "LIMIT") {
			loadQueries++
		}
	}
	if loadQueries != 1 {
		t.Fatalf("load page queries = %d, want 1 (cache served second call)", loadQueries)
	}
}

func TestIdentityEpochCacheMissChangedCount(t *testing.T) {
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

	factRow2 := []any{
		"fact-2", "scope-2", "gen-2",
		"aws_image_reference", "stable-key-2", "1.0.0",
		"aws_cloud", int64(0), "reported", "aws",
		"source-key-2", "arn:aws:...", "rec-2",
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		false,
		[]byte(`{}`),
	}

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ""),
			{rows: [][]any{factRow}},
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ""),
			probeQueryRow(2, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), ""),
			{rows: [][]any{factRow, factRow2}},
			probeQueryRow(2, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), ""),
		},
	}

	store := newFactStoreWithCache(db, 0)

	loaded1, err := store.ListActiveContainerImageIdentityFacts(context.Background())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(loaded1) != 1 {
		t.Fatalf("first call len = %d, want 1", len(loaded1))
	}

	loaded2, err := store.ListActiveContainerImageIdentityFacts(context.Background())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(loaded2) != 2 {
		t.Fatalf("second call len = %d, want 2 (epoch changed → reload)", len(loaded2))
	}

	var loadQueries int
	for _, q := range db.queries {
		if strings.Contains(q.query, "LIMIT") {
			loadQueries++
		}
	}
	if loadQueries != 2 {
		t.Fatalf("load page queries = %d, want 2 (both calls loaded)", loadQueries)
	}
}

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
// probeDelay simulates a DB round-trip so the test can prove non-serialization:
// under the P1-A bug (mu held across the probe), N concurrent callers would
// each wait for their own probe behind the lock, so wall time would scale
// with N x probeDelay; under the fix, concurrent probes overlap and the
// whole batch completes in a small constant multiple of probeDelay.
type concurrentSingleflightQueryer struct {
	mu         sync.Mutex
	probeCalls int
	loadCalls  int
	probeDelay time.Duration
	factRow    []any
	probeRow   []any
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

	// Probe query: simulate a DB round-trip so concurrent probes overlap in
	// wall time only if mu is genuinely released across the probe.
	if q.probeDelay > 0 {
		time.Sleep(q.probeDelay)
	}
	q.mu.Lock()
	q.probeCalls++
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
	loadCalls, probeCalls := q.loadCalls, q.probeCalls
	q.mu.Unlock()
	if loadCalls != 1 {
		t.Fatalf("load page queries = %d, want 1 (singleflight)", loadCalls)
	}
	t.Logf("probeCalls=%d loadCalls=%d elapsed=%v (probeDelay=%v)", probeCalls, loadCalls, elapsed, probeDelay)

	// Non-serialization proof (P1-A fix): 32 concurrent callers each need at
	// least one probe. If mu were held across the probe (the bug), those
	// probes would serialize behind the lock and wall time would scale
	// with N x probeDelay (32 x 20ms = 640ms). With mu released across the
	// probe (the fix), concurrent probes overlap, bounding wall time to a
	// small constant multiple of probeDelay regardless of N. The bound below
	// is generous enough to absorb scheduler jitter while still failing on
	// real O(N) serialization.
	maxAllowed := 10 * probeDelay
	if elapsed > maxAllowed {
		t.Fatalf("32 concurrent callers took %v, want <= %v (mu must not serialize the epoch probe across callers)", elapsed, maxAllowed)
	}
}

func TestIdentityEpochCacheCommitMidLoad(t *testing.T) {
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

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ""),
			{rows: [][]any{factRow}},
			probeQueryRow(2, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), ""),
			probeQueryRow(2, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), ""),
			{rows: [][]any{factRow, factRow}},
			probeQueryRow(2, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), ""),
		},
	}

	store := newFactStoreWithCache(db, 0)

	loaded1, err := store.ListActiveContainerImageIdentityFacts(context.Background())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(loaded1) != 1 {
		t.Fatalf("first call len = %d, want 1 (served uncached due to mid-load commit)", len(loaded1))
	}

	loaded2, err := store.ListActiveContainerImageIdentityFacts(context.Background())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(loaded2) != 2 {
		t.Fatalf("second call len = %d, want 2 (reloaded after mid-load commit)", len(loaded2))
	}

	var loadQueries int
	for _, q := range db.queries {
		if strings.Contains(q.query, "LIMIT") {
			loadQueries++
		}
	}
	if loadQueries != 2 {
		t.Fatalf("load page queries = %d, want 2 (first uncached, second reload)", loadQueries)
	}
}

func TestIdentityEpochCacheCapExceeded(t *testing.T) {
	t.Parallel()

	largePayload := make([]byte, 0, 5100)
	largePayload = append(largePayload, []byte(`{"data":"`)...)
	for i := 0; i < 5000; i++ {
		largePayload = append(largePayload, 'a')
	}
	largePayload = append(largePayload, []byte(`"}`)...)
	factRow := []any{
		"fact-1", "scope-1", "gen-1",
		"oci_registry.image_tag_observation", "stable-key-1", "1.0.0",
		"oci_registry", int64(0), "reported", "oci_registry",
		"source-key-1", "uri://example", "rec-1",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		false,
		largePayload,
	}

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ""),
			{rows: [][]any{factRow}},
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ""),
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ""),
			{rows: [][]any{factRow}},
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ""),
		},
	}

	store := newFactStoreWithCache(db, 100)

	loaded1, err := store.ListActiveContainerImageIdentityFacts(context.Background())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(loaded1) != 1 {
		t.Fatalf("first call len = %d, want 1", len(loaded1))
	}

	loaded2, err := store.ListActiveContainerImageIdentityFacts(context.Background())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(loaded2) != 1 {
		t.Fatalf("second call len = %d, want 1", len(loaded2))
	}

	var loadQueries int
	for _, q := range db.queries {
		if strings.Contains(q.query, "LIMIT") {
			loadQueries++
		}
	}
	if loadQueries != 2 {
		t.Fatalf("load page queries = %d, want 2 (cap exceeded → passthrough each time)", loadQueries)
	}
}

func TestIdentityEpochProbeFilterDrift(t *testing.T) {
	t.Parallel()

	probeFilter := extractIdentityFilter(probeIdentityEpochQuery)
	loadFilter := extractIdentityFilter(listActiveContainerImageIdentityFactsQuery)

	if probeFilter != loadFilter {
		t.Fatalf("probe filter != load filter:\nprobe: %s\nload: %s", probeFilter, loadFilter)
	}

	for _, want := range []string{
		"oci_registry.image_tag_observation",
		"oci_registry.image_manifest",
		"oci_registry.image_index",
		"aws_image_reference",
		"azure_image_reference",
		"gcp_image_reference",
		"aws_relationship",
		"content_entity",
	} {
		if !strings.Contains(probeFilter, want) {
			t.Fatalf("filter missing %q", want)
		}
	}
}

func TestIdentityEpochCacheDefensiveCopy(t *testing.T) {
	t.Parallel()

	factRow := []any{
		"fact-1", "scope-1", "gen-1",
		"oci_registry.image_tag_observation", "stable-key-1", "1.0.0",
		"oci_registry", int64(0), "reported", "oci_registry",
		"source-key-1", "uri://example", "rec-1",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		false,
		[]byte(`{"key":"value"}`),
	}

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ""),
			{rows: [][]any{factRow}},
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ""),
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ""),
		},
	}

	store := newFactStoreWithCache(db, 0)

	loaded1, err := store.ListActiveContainerImageIdentityFacts(context.Background())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if loaded1[0].Payload == nil {
		t.Fatal("Payload is nil after first call")
	}

	loaded2, err := store.ListActiveContainerImageIdentityFacts(context.Background())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if loaded1[0].Payload == nil || loaded2[0].Payload == nil {
		t.Fatal("Payload is nil")
	}
	if _, ok := loaded1[0].Payload["mutated"]; ok {
		t.Fatal("Payload already has 'mutated' key — test precondition violated")
	}
	loaded1[0].Payload["mutated"] = true
	if _, ok := loaded2[0].Payload["mutated"]; ok {
		t.Fatal("Second call's payload was mutated (defensive copy failed)")
	}
}
