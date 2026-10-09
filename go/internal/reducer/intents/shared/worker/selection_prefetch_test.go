// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

func selectionTestRow(domain, scope, unit, gen string, i int) sharedintent.Row {
	return sharedintent.Row{
		IntentID:         fmt.Sprintf("intent-%s-%d", scope, i),
		ProjectionDomain: domain,
		PartitionKey:     fmt.Sprintf("part-%s-%d", scope, i),
		ScopeID:          scope,
		AcceptanceUnitID: unit,
		RepositoryID:     unit,
		SourceRunID:      "run-1",
		GenerationID:     gen,
		Payload:          map[string]any{"action": "upsert"},
		CreatedAt:        time.Now().UTC(),
	}
}

// stubSupersededReader is a stubSharedIntentReader that opts into the
// #7121 drain by reporting every queried generation as superseded.
type stubSupersededReader struct {
	stubSharedIntentReader
}

func (s *stubSupersededReader) SupersededGenerationIDs(_ context.Context, generationIDs []string) (map[string]struct{}, error) {
	superseded := make(map[string]struct{}, len(generationIDs))
	for _, id := range generationIDs {
		superseded[id] = struct{}{}
	}
	return superseded, nil
}

// TestSelectPartitionBatchCachesReadinessAcrossRounds is the #7724 1B RED
// test: across widen rounds each distinct readiness key is queried exactly
// once. It fails without the cross-round cache (round two re-queries round
// one's keys) and passes with it.
func TestSelectPartitionBatchCachesReadinessAcrossRounds(t *testing.T) {
	t.Parallel()

	const domain = reducercontract.DomainSQLRelationships
	var pending []sharedintent.Row
	for i := 0; i < 50; i++ {
		pending = append(pending, selectionTestRow(domain, fmt.Sprintf("scope-%d", i), fmt.Sprintf("unit-%d", i), "gen-1", i))
	}
	reader := &stubSharedIntentReader{
		limitResponder: func(limit int) []sharedintent.Row {
			if limit > len(pending) {
				limit = len(pending)
			}
			return append([]sharedintent.Row(nil), pending[:limit]...)
		},
	}

	acceptAll := func(_ context.Context, intents []sharedintent.Row) (sharedintent.AcceptedGenerationLookup, error) {
		return func(key sharedintent.AcceptanceKey) (string, bool) { return "gen-1", true }, nil
	}
	var readinessCalls [][]gpphase.PhaseKey
	neverReady := func(_ context.Context, keys []gpphase.PhaseKey, _ gpphase.Phase) (gpphase.ReadinessLookup, error) {
		readinessCalls = append(readinessCalls, keys)
		return func(gpphase.PhaseKey, gpphase.Phase) (bool, bool) { return false, false }, nil
	}

	batch, err := SelectPartitionBatch(context.Background(), reader, domain, 0, 1, 10,
		acceptedGenerationFixed("gen-1", true), acceptAll, nil, neverReady, nil)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if batch.SelectionRounds != 3 {
		t.Fatalf("SelectionRounds = %d, want 3 (scan 20, 40, then 50<80 seenAll)", batch.SelectionRounds)
	}
	if len(readinessCalls) != 3 {
		t.Fatalf("readiness prefetch called %d times, want 3 (once per round)", len(readinessCalls))
	}

	seen := make(map[gpphase.PhaseKey]int)
	for round, keys := range readinessCalls {
		for _, key := range keys {
			if prev, dup := seen[key]; dup {
				t.Fatalf("readiness key %+v queried in round %d and round %d, want exactly once (cross-round cache)", key, prev, round)
			}
			seen[key] = round
		}
	}
	if len(seen) != 50 {
		t.Fatalf("distinct readiness keys queried = %d, want 50", len(seen))
	}
	if batch.BlockedCount != 50 {
		t.Fatalf("BlockedCount = %d, want 50", batch.BlockedCount)
	}
}

// TestSelectPartitionBatchRefreshesAcceptanceEveryRound proves acceptance
// is re-queried fresh every widen round (#7724 1B: never cached). An
// acceptance committed between rounds is seen by the same selection: rows
// stale under round one's answer project once round two reports the new
// generation.
func TestSelectPartitionBatchRefreshesAcceptanceEveryRound(t *testing.T) {
	t.Parallel()

	const domain = reducercontract.DomainWorkloadDependency
	var pending []sharedintent.Row
	for i := 0; i < 30; i++ {
		pending = append(pending, selectionTestRow(domain, fmt.Sprintf("scope-%d", i), fmt.Sprintf("unit-%d", i), "gen-2", i))
	}
	reader := &stubSharedIntentReader{
		limitResponder: func(limit int) []sharedintent.Row {
			if limit > len(pending) {
				limit = len(pending)
			}
			return append([]sharedintent.Row(nil), pending[:limit]...)
		},
	}

	acceptanceCalls := 0
	changingAcceptance := func(_ context.Context, _ []sharedintent.Row) (sharedintent.AcceptedGenerationLookup, error) {
		acceptanceCalls++
		generation := "gen-1"
		if acceptanceCalls >= 2 {
			generation = "gen-2"
		}
		return func(sharedintent.AcceptanceKey) (string, bool) { return generation, true }, nil
	}

	batch, err := SelectPartitionBatch(context.Background(), reader, domain, 0, 1, 10,
		acceptedGenerationFixed("gen-2", true), changingAcceptance, nil, nil, nil)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if acceptanceCalls != 2 {
		t.Fatalf("acceptance prefetch called %d times, want 2 (fresh every round)", acceptanceCalls)
	}
	if len(batch.LatestRows) != 10 {
		t.Fatalf("LatestRows = %d, want 10 (round-two acceptance gen-2 must win)", len(batch.LatestRows))
	}
	if batch.StaleCount != 0 {
		t.Fatalf("StaleCount = %d, want 0 (no row is stale under the fresh answer)", batch.StaleCount)
	}
}

// TestSelectPartitionBatchDrainRereadBypassesCache proves the #7121
// readiness re-read bypasses the cross-round cache: a producer that
// publishes between the first readiness read and the superseded lookup
// keeps its rows (they project) instead of draining as superseded.
func TestSelectPartitionBatchDrainRereadBypassesCache(t *testing.T) {
	t.Parallel()

	const domain = reducercontract.DomainSQLRelationships
	var pending []sharedintent.Row
	for i := 0; i < 5; i++ {
		pending = append(pending, selectionTestRow(domain, "scope-a", "unit-a", "gen-1", i))
	}
	reader := &stubSupersededReader{stubSharedIntentReader: stubSharedIntentReader{pending: pending}}

	acceptAll := func(_ context.Context, _ []sharedintent.Row) (sharedintent.AcceptedGenerationLookup, error) {
		return func(sharedintent.AcceptanceKey) (string, bool) { return "gen-1", true }, nil
	}
	readinessCalls := 0
	publishBetweenReads := func(_ context.Context, _ []gpphase.PhaseKey, _ gpphase.Phase) (gpphase.ReadinessLookup, error) {
		readinessCalls++
		if readinessCalls == 1 {
			return func(gpphase.PhaseKey, gpphase.Phase) (bool, bool) { return false, false }, nil
		}
		return func(gpphase.PhaseKey, gpphase.Phase) (bool, bool) { return true, true }, nil
	}

	batch, err := SelectPartitionBatch(context.Background(), reader, domain, 0, 1, 100,
		acceptedGenerationFixed("gen-1", true), acceptAll, nil, publishBetweenReads, nil)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if readinessCalls < 2 {
		t.Fatalf("readiness prefetch called %d times, want at least 2 (main gate + drain re-read)", readinessCalls)
	}
	if len(batch.LatestRows) != 5 {
		t.Fatalf("LatestRows = %d, want 5 (published rows project, not drain)", len(batch.LatestRows))
	}
	if batch.StaleCount != 0 {
		t.Fatalf("StaleCount = %d, want 0 (fresh re-read must prevent the superseded drain)", batch.StaleCount)
	}
	if batch.BlockedCount != 0 {
		t.Fatalf("BlockedCount = %d, want 0", batch.BlockedCount)
	}
}

// TestSelectPartitionBatchReportsSelectionStats proves the selection
// reports widen rounds and prefetch keys/queries/rows/cache-hits (#7724
// telemetry: per-visit prefetch stats), and that acceptance never records
// cache hits.
func TestSelectPartitionBatchReportsSelectionStats(t *testing.T) {
	t.Parallel()

	const domain = reducercontract.DomainSQLRelationships
	var pending []sharedintent.Row
	for i := 0; i < 50; i++ {
		pending = append(pending, selectionTestRow(domain, fmt.Sprintf("scope-%d", i), fmt.Sprintf("unit-%d", i), "gen-1", i))
	}
	reader := &stubSharedIntentReader{
		limitResponder: func(limit int) []sharedintent.Row {
			if limit > len(pending) {
				limit = len(pending)
			}
			return append([]sharedintent.Row(nil), pending[:limit]...)
		},
	}

	recordingAcceptance := func(ctx context.Context, intents []sharedintent.Row) (sharedintent.AcceptedGenerationLookup, error) {
		seen := make(map[sharedintent.AcceptanceKey]struct{})
		for _, intent := range intents {
			if key, ok := intent.AcceptanceKey(); ok {
				seen[key] = struct{}{}
			}
		}
		sharedintent.RecordPrefetch(ctx, sharedintent.PrefetchKindAcceptance, len(seen), 1, len(seen), 0, time.Millisecond)
		return func(sharedintent.AcceptanceKey) (string, bool) { return "gen-1", true }, nil
	}
	recordingReadiness := func(ctx context.Context, keys []gpphase.PhaseKey, _ gpphase.Phase) (gpphase.ReadinessLookup, error) {
		sharedintent.RecordPrefetch(ctx, sharedintent.PrefetchKindReadiness, len(keys), 1, 0, 0, time.Millisecond)
		return func(gpphase.PhaseKey, gpphase.Phase) (bool, bool) { return false, false }, nil
	}

	batch, err := SelectPartitionBatch(context.Background(), reader, domain, 0, 1, 10,
		acceptedGenerationFixed("gen-1", true), recordingAcceptance, nil, recordingReadiness, nil)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if batch.SelectionRounds != 3 {
		t.Fatalf("SelectionRounds = %d, want 3", batch.SelectionRounds)
	}
	// Acceptance is re-queried fresh every round over the growing window:
	// 20 + 40 + 50 keys in 3 queries.
	if batch.PrefetchStats.Acceptance.Keys != 110 || batch.PrefetchStats.Acceptance.Queries != 3 {
		t.Fatalf("acceptance stats = %+v, want keys=110 queries=3", batch.PrefetchStats.Acceptance)
	}
	if batch.PrefetchStats.Acceptance.CacheHits != 0 {
		t.Fatalf("acceptance cache hits = %d, want 0 (never cached)", batch.PrefetchStats.Acceptance.CacheHits)
	}
	// Readiness queries only the per-round delta (20 + 20 + 10) while the
	// cache serves the overlap (0 + 20 + 40 hits).
	if batch.PrefetchStats.Readiness.Keys != 50 || batch.PrefetchStats.Readiness.Queries != 3 {
		t.Fatalf("readiness stats = %+v, want keys=50 queries=3", batch.PrefetchStats.Readiness)
	}
	if batch.PrefetchStats.Readiness.CacheHits != 60 {
		t.Fatalf("readiness cache hits = %d, want 60", batch.PrefetchStats.Readiness.CacheHits)
	}
}

// TestSelectPartitionBatchPrefetchErrorFailsSelection proves a batch error
// fails the selection instead of silently dropping or keeping rows.
func TestSelectPartitionBatchPrefetchErrorFailsSelection(t *testing.T) {
	t.Parallel()

	const domain = reducercontract.DomainSQLRelationships
	reader := &stubSharedIntentReader{
		pending: []sharedintent.Row{selectionTestRow(domain, "scope-a", "unit-a", "gen-1", 0)},
	}
	failing := func(_ context.Context, _ []sharedintent.Row) (sharedintent.AcceptedGenerationLookup, error) {
		return nil, errors.New("boom")
	}
	neverReady := func(_ context.Context, _ []gpphase.PhaseKey, _ gpphase.Phase) (gpphase.ReadinessLookup, error) {
		return func(gpphase.PhaseKey, gpphase.Phase) (bool, bool) { return false, false }, nil
	}

	_, err := SelectPartitionBatch(context.Background(), reader, domain, 0, 1, 100,
		acceptedGenerationFixed("gen-1", true), failing, nil, neverReady, nil)
	if err == nil || !strings.Contains(err.Error(), "prefetch accepted generations") {
		t.Fatalf("SelectPartitionBatch() error = %v, want prefetch failure", err)
	}
}
