// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric/noop"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// probeQueryRow returns a single-row fake response for the epoch probe:
// count, COALESCE(max(observed_at), ...), active_fingerprint.
func probeQueryRow(count int, maxObservedAt time.Time, fingerprint int64) queueFakeRows {
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
	cache, err := newIdentityEpochCache(noop.NewMeterProvider().Meter("test"), maxBytes)
	if err != nil {
		panic("newIdentityEpochCache in test: " + err.Error())
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
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0),
			{rows: [][]any{factRow}},
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0),
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0),
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
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0),
			{rows: [][]any{factRow}},
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0),
			probeQueryRow(2, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), 0),
			{rows: [][]any{factRow, factRow2}},
			probeQueryRow(2, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), 0),
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

	responses := []queueFakeRows{
		probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0),
		{rows: [][]any{factRow}},
		probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0),
	}
	for range 31 {
		responses = append(responses, probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0))
	}

	db := &fakeExecQueryer{
		queryResponses: responses,
	}

	store := newFactStoreWithCache(db, 0)

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

	var loadQueries int
	for _, q := range db.queries {
		if strings.Contains(q.query, "LIMIT") {
			loadQueries++
		}
	}
	if loadQueries != 1 {
		t.Fatalf("load page queries = %d, want 1 (singleflight)", loadQueries)
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
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0),
			{rows: [][]any{factRow}},
			probeQueryRow(2, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), 0),
			probeQueryRow(2, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), 0),
			{rows: [][]any{factRow, factRow}},
			probeQueryRow(2, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), 0),
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
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0),
			{rows: [][]any{factRow}},
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0),
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0),
			{rows: [][]any{factRow}},
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0),
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
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0),
			{rows: [][]any{factRow}},
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0),
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0),
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
