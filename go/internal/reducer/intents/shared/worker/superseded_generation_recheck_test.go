// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"slices"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// publishingReader is a superseded-generation reader whose lookup publishes the
// phase row before it answers, modelling a producer that publishes and acks
// between the selection's readiness read and its superseded lookup (#7121 N3).
type publishingReader struct {
	supersededReader
	publish func()
}

func (p *publishingReader) SupersededGenerationIDs(
	ctx context.Context,
	generationIDs []string,
) (map[string]struct{}, error) {
	if p.publish != nil {
		p.publish()
	}
	return p.supersededReader.SupersededGenerationIDs(ctx, generationIDs)
}

// TestSelectPartitionBatchKeepsRowThatTurnedReadyAfterReadinessRead is the #7121
// N3 RED. A row is blocked when readiness is first read, the in-flight
// producer then publishes and acks, and the superseded lookup finds no producer
// left. Draining now would drop a READY row whose edge a delta successor never
// re-emits, so the worker must re-read readiness for the rows it would drain
// and keep the ones that became ready.
func TestSelectPartitionBatchKeepsRowThatTurnedReadyAfterReadinessRead(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	row := runsInRow("repo-n3", "run-old", "gen-old", now)
	published := false
	reader := &publishingReader{
		supersededReader: supersededReader{
			stubSharedIntentReader: stubSharedIntentReader{pending: []sharedintent.Row{row}},
			superseded:             map[string]struct{}{"gen-old": {}},
		},
		publish: func() { published = true },
	}
	lookup := func(gpphase.PhaseKey, gpphase.Phase) (bool, bool) { return published, published }

	batch, err := SelectPartitionBatch(
		context.Background(), reader, reducercontract.DomainRunsIn,
		0, 1, 10,
		acceptedGenerationFixed("gen-old", true), nil,
		lookup, nil, nil,
	)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if len(batch.LatestRows) != 1 || batch.LatestRows[0].IntentID != row.IntentID {
		t.Fatalf("LatestRows = %v, want the row that became ready to project", batch.LatestRows)
	}
	if len(batch.StaleIDs) != 0 || batch.SupersededGenerationCount != 0 {
		t.Fatalf("StaleIDs = %v count = %d, want none: the phase row published before the drain",
			batch.StaleIDs, batch.SupersededGenerationCount)
	}
	if batch.BlockedCount != 0 {
		t.Fatalf("BlockedCount = %d, want 0", batch.BlockedCount)
	}
}

// TestSelectPartitionBatchRecheckUsesFreshPrefetch proves the re-check reads
// readiness again through the prefetch (one extra bounded round trip over only
// the rows about to drain) instead of reusing the stale first resolution, and
// that a row still blocked after the re-check drains.
func TestSelectPartitionBatchRecheckUsesFreshPrefetch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	stays := runsInRow("repo-stays", "run-stays", "gen-stays", now)
	turns := runsInRow("repo-turns", "run-turns", "gen-turns", now)
	published := map[string]bool{}
	var prefetchKeys [][]gpphase.PhaseKey
	prefetch := func(_ context.Context, keys []gpphase.PhaseKey, _ gpphase.Phase) (gpphase.ReadinessLookup, error) {
		prefetchKeys = append(prefetchKeys, slices.Clone(keys))
		snapshot := make(map[string]bool, len(published))
		for id, ok := range published {
			snapshot[id] = ok
		}
		return func(key gpphase.PhaseKey, _ gpphase.Phase) (bool, bool) {
			ok := snapshot[key.GenerationID]
			return ok, ok
		}, nil
	}
	reader := &publishingReader{
		supersededReader: supersededReader{
			stubSharedIntentReader: stubSharedIntentReader{pending: []sharedintent.Row{stays, turns}},
			superseded:             map[string]struct{}{"gen-stays": {}, "gen-turns": {}},
		},
		publish: func() { published["gen-turns"] = true },
	}
	accepted := acceptedByRun(map[string]string{"run-stays": "gen-stays", "run-turns": "gen-turns"})

	batch, err := SelectPartitionBatch(
		context.Background(), reader, reducercontract.DomainRunsIn,
		0, 1, 10, accepted, nil, nil, prefetch, nil,
	)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if len(prefetchKeys) != 2 {
		t.Fatalf("readiness prefetches = %d, want 2 (initial read and one re-check)", len(prefetchKeys))
	}
	if len(prefetchKeys[0]) != 2 || len(prefetchKeys[1]) != 2 {
		t.Fatalf("prefetch key counts = %d/%d, want 2/2 (both rows drain-eligible)",
			len(prefetchKeys[0]), len(prefetchKeys[1]))
	}
	if len(batch.LatestRows) != 1 || batch.LatestRows[0].IntentID != turns.IntentID {
		t.Fatalf("LatestRows = %v, want only the row that turned ready", batch.LatestRows)
	}
	if !slices.Equal(batch.StaleIDs, []string{stays.IntentID}) || batch.SupersededGenerationCount != 1 {
		t.Fatalf("StaleIDs = %v count = %d, want only the still-blocked row",
			batch.StaleIDs, batch.SupersededGenerationCount)
	}
}

// TestSelectPartitionBatchSkipsRecheckWhenNothingDrains keeps the extra round
// trip conditional: with no superseded generation there is no re-check.
func TestSelectPartitionBatchSkipsRecheckWhenNothingDrains(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	live := runsInRow("repo-live", "run-live", "gen-live", now)
	prefetches := 0
	prefetch := func(_ context.Context, _ []gpphase.PhaseKey, _ gpphase.Phase) (gpphase.ReadinessLookup, error) {
		prefetches++
		return readinessLookupFixed(false, false), nil
	}
	reader := &supersededReader{stubSharedIntentReader: stubSharedIntentReader{pending: []sharedintent.Row{live}}}

	batch, err := SelectPartitionBatch(
		context.Background(), reader, reducercontract.DomainRunsIn,
		0, 1, 10, acceptedGenerationFixed("gen-live", true), nil, nil, prefetch, nil,
	)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if prefetches != 1 || batch.BlockedCount != 1 {
		t.Fatalf("prefetches = %d blocked = %d, want 1/1", prefetches, batch.BlockedCount)
	}
}
