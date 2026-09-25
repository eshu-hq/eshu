// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// supersededReader wraps the legacy stub reader with the optional
// SupersededGenerationReader port so tests can drive the #7121 drain.
type supersededReader struct {
	stubSharedIntentReader
	superseded map[string]struct{}
	err        error
	calls      [][]string
}

func (s *supersededReader) SupersededGenerationIDs(
	_ context.Context,
	generationIDs []string,
) (map[string]struct{}, error) {
	s.calls = append(s.calls, slices.Clone(generationIDs))
	if s.err != nil {
		return nil, s.err
	}
	return s.superseded, nil
}

func runsInRow(repoID, sourceRunID, generationID string, created time.Time) sharedintent.Row {
	return sharedintent.Build(sharedintent.Input{
		ProjectionDomain: reducercontract.DomainRunsIn,
		PartitionKey:     "runs_in:" + repoID + ":" + sourceRunID,
		ScopeID:          "scope-" + repoID,
		AcceptanceUnitID: repoID,
		RepositoryID:     repoID,
		SourceRunID:      sourceRunID,
		GenerationID:     generationID,
		Payload:          map[string]any{"repo_id": repoID},
		CreatedAt:        created,
	})
}

func selectRunsIn(
	reader IntentReader,
	accepted sharedintent.AcceptedGenerationLookup,
	ready bool,
	found bool,
) (PartitionBatchResult, error) {
	return SelectPartitionBatch(
		context.Background(), reader, reducercontract.DomainRunsIn,
		0, 1, 10,
		accepted, nil,
		readinessLookupFixed(ready, found), nil, nil,
	)
}

// TestSelectPartitionBatchDrainsSupersededGenerationIntent is the #7121 RED: a
// pending runs_in intent whose generation is superseded and that has no
// workload_materialization phase row must drain as stale instead of blocking
// forever.
func TestSelectPartitionBatchDrainsSupersededGenerationIntent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	orphan := runsInRow("repo-a", "run-old", "gen-old", now)
	reader := &supersededReader{
		stubSharedIntentReader: stubSharedIntentReader{pending: []sharedintent.Row{orphan}},
		superseded:             map[string]struct{}{"gen-old": {}},
	}

	// The orphan's own source-run acceptance row names its own generation, so
	// the acceptance filter alone keeps it "active" (the observed defect).
	batch, err := selectRunsIn(reader, acceptedGenerationFixed("gen-old", true), false, false)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if !slices.Equal(batch.StaleIDs, []string{orphan.IntentID}) {
		t.Fatalf("StaleIDs = %v, want [%s]", batch.StaleIDs, orphan.IntentID)
	}
	if batch.SupersededGenerationCount != 1 {
		t.Fatalf("SupersededGenerationCount = %d, want 1", batch.SupersededGenerationCount)
	}
	if batch.BlockedCount != 0 || len(batch.LatestRows) != 0 {
		t.Fatalf("blocked=%d latest=%d, want 0/0", batch.BlockedCount, len(batch.LatestRows))
	}
}

// TestSelectPartitionBatchKeepsActiveGenerationBlockedOnMissingPhase proves the
// drain does not swallow a legitimate wait: an active generation's intent with
// a missing phase row still blocks.
func TestSelectPartitionBatchKeepsActiveGenerationBlockedOnMissingPhase(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	live := runsInRow("repo-b", "run-new", "gen-live", now)
	reader := &supersededReader{
		stubSharedIntentReader: stubSharedIntentReader{pending: []sharedintent.Row{live}},
		superseded:             map[string]struct{}{"gen-old": {}},
	}

	batch, err := selectRunsIn(reader, acceptedGenerationFixed("gen-live", true), false, false)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if len(batch.StaleIDs) != 0 || batch.SupersededGenerationCount != 0 {
		t.Fatalf("StaleIDs = %v count = %d, want none", batch.StaleIDs, batch.SupersededGenerationCount)
	}
	if batch.BlockedCount != 1 {
		t.Fatalf("BlockedCount = %d, want 1", batch.BlockedCount)
	}
}

// TestSelectPartitionBatchLeavesSuccessorGenerationUntouched proves that when a
// superseded generation and its successor both have pending rows in the same
// batch, only the superseded row drains and the successor row still flows
// through readiness.
func TestSelectPartitionBatchLeavesSuccessorGenerationUntouched(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	orphan := runsInRow("repo-c", "run-old", "gen-old", now)
	successor := runsInRow("repo-c", "run-new", "gen-new", now.Add(time.Minute))
	reader := &supersededReader{
		stubSharedIntentReader: stubSharedIntentReader{pending: []sharedintent.Row{orphan, successor}},
		superseded:             map[string]struct{}{"gen-old": {}},
	}
	accepted := func(key sharedintent.AcceptanceKey) (string, bool) {
		if key.SourceRunID == "run-old" {
			return "gen-old", true
		}
		return "gen-new", true
	}

	batch, err := selectRunsIn(reader, accepted, true, true)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if !slices.Equal(batch.StaleIDs, []string{orphan.IntentID}) {
		t.Fatalf("StaleIDs = %v, want only the superseded row", batch.StaleIDs)
	}
	if len(batch.LatestRows) != 1 || batch.LatestRows[0].IntentID != successor.IntentID {
		t.Fatalf("LatestRows = %v, want the successor row", batch.LatestRows)
	}
	if len(reader.calls) != 1 {
		t.Fatalf("superseded lookups = %d, want exactly 1 round trip", len(reader.calls))
	}
	if got := slices.Sorted(slices.Values(reader.calls[0])); !slices.Equal(got, []string{"gen-new", "gen-old"}) {
		t.Fatalf("lookup ids = %v, want the distinct batch generations", got)
	}
}

// TestSelectPartitionBatchWithoutSupersededReaderIsUnchanged pins that a reader
// that does not implement the port keeps the pre-#7121 behavior byte-identical.
func TestSelectPartitionBatchWithoutSupersededReaderIsUnchanged(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	orphan := runsInRow("repo-d", "run-old", "gen-old", now)
	reader := &stubSharedIntentReader{pending: []sharedintent.Row{orphan}}

	batch, err := selectRunsIn(reader, acceptedGenerationFixed("gen-old", true), false, false)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if len(batch.StaleIDs) != 0 || batch.BlockedCount != 1 {
		t.Fatalf("StaleIDs = %v blocked = %d, want none/1", batch.StaleIDs, batch.BlockedCount)
	}
}

// TestSelectPartitionBatchPropagatesSupersededLookupError proves a failed
// lookup fails the selection rather than silently dropping or keeping rows.
func TestSelectPartitionBatchPropagatesSupersededLookupError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	boom := errors.New("postgres unavailable")
	reader := &supersededReader{
		stubSharedIntentReader: stubSharedIntentReader{
			pending: []sharedintent.Row{runsInRow("repo-e", "run-old", "gen-old", now)},
		},
		err: boom,
	}

	_, err := selectRunsIn(reader, acceptedGenerationFixed("gen-old", true), false, false)
	if !errors.Is(err, boom) {
		t.Fatalf("SelectPartitionBatch() error = %v, want wrapped %v", err, boom)
	}
}
