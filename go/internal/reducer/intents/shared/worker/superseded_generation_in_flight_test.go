// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// TestSelectPartitionBatchKeepsBlockedRowWhileProducerIsInFlight covers the
// #7121 N1 window: the generation is superseded but its producer (for example
// workload_materialization) was already running when the successor activated.
// The reader reports only superseded generations with NO in-flight producer, so
// while the producer runs the lookup omits the generation and the blocked row
// must stay blocked. Once the producer publishes the phase row the row is ready
// and projects; once it ends without publishing the reader reports the
// generation and the row drains. Draining in the first pass would lose the edge
// permanently under a delta successor.
func TestSelectPartitionBatchKeepsBlockedRowWhileProducerIsInFlight(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	row := runsInRow("repo-a", "run-old", "gen-old", now)
	accepted := acceptedGenerationFixed("gen-old", true)
	newReader := func(superseded map[string]struct{}) *supersededReader {
		return &supersededReader{
			stubSharedIntentReader: stubSharedIntentReader{pending: []sharedintent.Row{row}},
			superseded:             superseded,
		}
	}

	// Pass 1: producer still in flight. The store omits the generation.
	inFlight := newReader(map[string]struct{}{})
	batch, err := selectRunsIn(inFlight, accepted, false, false)
	if err != nil {
		t.Fatalf("in-flight SelectPartitionBatch() error = %v", err)
	}
	if len(batch.StaleIDs) != 0 || batch.SupersededGenerationCount != 0 {
		t.Fatalf("in flight: StaleIDs = %v count = %d, want none", batch.StaleIDs, batch.SupersededGenerationCount)
	}
	if batch.BlockedCount != 1 || len(batch.LatestRows) != 0 {
		t.Fatalf("in flight: blocked = %d latest = %d, want 1/0", batch.BlockedCount, len(batch.LatestRows))
	}
	if len(inFlight.calls) != 1 || !slices.Equal(inFlight.calls[0], []string{"gen-old"}) {
		t.Fatalf("in flight: lookup calls = %v, want one call for gen-old", inFlight.calls)
	}

	// Pass 2: the producer published the phase row. The row is ready and must
	// project, and the lookup is not consulted for a batch with nothing blocked.
	published := newReader(map[string]struct{}{"gen-old": {}})
	batch, err = selectRunsIn(published, accepted, true, true)
	if err != nil {
		t.Fatalf("published SelectPartitionBatch() error = %v", err)
	}
	if len(batch.StaleIDs) != 0 || len(batch.LatestRows) != 1 || batch.BlockedCount != 0 {
		t.Fatalf("published: stale = %v latest = %d blocked = %d, want ready row kept",
			batch.StaleIDs, len(batch.LatestRows), batch.BlockedCount)
	}
	if len(published.calls) != 0 {
		t.Fatalf("published: lookup calls = %v, want none", published.calls)
	}

	// Pass 3: the producer ended without publishing. The store now reports the
	// generation, so the still-blocked row drains.
	orphaned := newReader(map[string]struct{}{"gen-old": {}})
	batch, err = selectRunsIn(orphaned, accepted, false, false)
	if err != nil {
		t.Fatalf("orphaned SelectPartitionBatch() error = %v", err)
	}
	if !slices.Equal(batch.StaleIDs, []string{row.IntentID}) || batch.SupersededGenerationCount != 1 {
		t.Fatalf("orphaned: StaleIDs = %v count = %d, want drained", batch.StaleIDs, batch.SupersededGenerationCount)
	}
}
