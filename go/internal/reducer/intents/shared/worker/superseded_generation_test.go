// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
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

// readinessPublishedFor returns a readiness lookup that reports the phase row
// as published (ready) only for the listed generations, mirroring production
// where a phase row is keyed by generation id and never appears for a
// generation superseded before workload materialization ran.
func readinessPublishedFor(generationIDs ...string) gpphase.ReadinessLookup {
	published := make(map[string]struct{}, len(generationIDs))
	for _, id := range generationIDs {
		published[id] = struct{}{}
	}
	return func(key gpphase.PhaseKey, _ gpphase.Phase) (bool, bool) {
		_, ok := published[key.GenerationID]
		return ok, ok
	}
}

func acceptedByRun(generationsByRun map[string]string) sharedintent.AcceptedGenerationLookup {
	return func(key sharedintent.AcceptanceKey) (string, bool) {
		generationID, ok := generationsByRun[key.SourceRunID]
		return generationID, ok
	}
}

// TestSelectPartitionBatchKeepsReadyRowOnSupersededGeneration is the #7121
// review F1 RED: a superseded generation whose phase row DID publish still has
// ready intents. A delta successor (scope_generations.is_delta) carries only
// changed-file facts and never re-emits an untouched file's edge, so draining
// the ready row would lose that edge permanently. The row must project.
func TestSelectPartitionBatchKeepsReadyRowOnSupersededGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	ready := runsInRow("repo-f", "run-old", "gen-old", now)
	reader := &supersededReader{
		stubSharedIntentReader: stubSharedIntentReader{pending: []sharedintent.Row{ready}},
		superseded:             map[string]struct{}{"gen-old": {}},
	}

	batch, err := selectRunsIn(reader, acceptedGenerationFixed("gen-old", true), true, true)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if len(batch.LatestRows) != 1 || batch.LatestRows[0].IntentID != ready.IntentID {
		t.Fatalf("LatestRows = %v, want the ready row on the superseded generation", batch.LatestRows)
	}
	if len(batch.StaleIDs) != 0 || batch.SupersededGenerationCount != 0 {
		t.Fatalf("StaleIDs = %v count = %d, want none: a ready row must project", batch.StaleIDs, batch.SupersededGenerationCount)
	}
	if len(reader.calls) != 0 {
		t.Fatalf("superseded lookups = %d, want 0 when nothing is blocked", len(reader.calls))
	}
}

// TestSelectPartitionBatchDrainsOnlyBlockedRowsOfSupersededGeneration mixes a
// ready row and a blocked row on the same superseded generation with a ready
// successor row: only the blocked row drains, and the lookup covers only the
// blocked rows' generation.
func TestSelectPartitionBatchDrainsOnlyBlockedRowsOfSupersededGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	blockedOrphan := runsInRow("repo-g", "run-old-g", "gen-old-blocked", now)
	readyOnSuperseded := runsInRow("repo-h", "run-old-h", "gen-old-ready", now)
	successor := runsInRow("repo-i", "run-new-i", "gen-new", now)
	reader := &supersededReader{
		stubSharedIntentReader: stubSharedIntentReader{
			pending: []sharedintent.Row{blockedOrphan, readyOnSuperseded, successor},
		},
		superseded: map[string]struct{}{"gen-old-blocked": {}, "gen-old-ready": {}},
	}
	accepted := acceptedByRun(map[string]string{
		"run-old-g": "gen-old-blocked",
		"run-old-h": "gen-old-ready",
		"run-new-i": "gen-new",
	})

	batch, err := SelectPartitionBatch(
		context.Background(), reader, reducercontract.DomainRunsIn,
		0, 1, 10, accepted, nil,
		readinessPublishedFor("gen-old-ready", "gen-new"), nil, nil,
	)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if !slices.Equal(batch.StaleIDs, []string{blockedOrphan.IntentID}) || batch.SupersededGenerationCount != 1 {
		t.Fatalf("StaleIDs = %v count = %d, want only the blocked superseded row", batch.StaleIDs, batch.SupersededGenerationCount)
	}
	gotReady := []string{}
	for _, row := range batch.LatestRows {
		gotReady = append(gotReady, row.IntentID)
	}
	wantReady := []string{readyOnSuperseded.IntentID, successor.IntentID}
	slices.Sort(gotReady)
	slices.Sort(wantReady)
	if !slices.Equal(gotReady, wantReady) {
		t.Fatalf("LatestRows = %v, want the ready rows %v", gotReady, wantReady)
	}
	if batch.BlockedCount != 0 || len(batch.BlockedRows) != 0 {
		t.Fatalf("blocked = %d/%d, want 0: the drained row must leave BlockedRows", batch.BlockedCount, len(batch.BlockedRows))
	}
	if len(reader.calls) != 1 || !slices.Equal(reader.calls[0], []string{"gen-old-blocked"}) {
		t.Fatalf("lookups = %v, want one round trip over the blocked rows' generation only", reader.calls)
	}
}

// TestSelectPartitionBatchKeepsPendingGenerationBlocked pins that a blocked row
// on a generation that is not superseded (pending or active: its phase row may
// still publish) stays blocked even though the lookup ran.
func TestSelectPartitionBatchKeepsPendingGenerationBlocked(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	pending := runsInRow("repo-j", "run-pending", "gen-pending", now)
	reader := &supersededReader{
		stubSharedIntentReader: stubSharedIntentReader{pending: []sharedintent.Row{pending}},
		superseded:             map[string]struct{}{},
	}

	batch, err := selectRunsIn(reader, acceptedGenerationFixed("gen-pending", true), false, false)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if batch.BlockedCount != 1 || len(batch.StaleIDs) != 0 {
		t.Fatalf("blocked = %d stale = %v, want 1/none", batch.BlockedCount, batch.StaleIDs)
	}
	if len(reader.calls) != 1 || !slices.Equal(reader.calls[0], []string{"gen-pending"}) {
		t.Fatalf("lookups = %v, want one round trip over the blocked generation", reader.calls)
	}
}

// TestSelectPartitionBatchSupersededLookupSkippedWhenNothingBlocked proves the
// port costs no round trip when every row is ready.
func TestSelectPartitionBatchSupersededLookupSkippedWhenNothingBlocked(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	reader := &supersededReader{
		stubSharedIntentReader: stubSharedIntentReader{
			pending: []sharedintent.Row{runsInRow("repo-k", "run-1", "gen-1", now)},
		},
		superseded: map[string]struct{}{"gen-1": {}},
	}

	batch, err := selectRunsIn(reader, acceptedGenerationFixed("gen-1", true), true, true)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if len(batch.LatestRows) != 1 || len(reader.calls) != 0 {
		t.Fatalf("latest = %d lookups = %d, want 1/0", len(batch.LatestRows), len(reader.calls))
	}
}

// TestSelectPartitionBatchReturnsWithoutWideningWhenSupersededRowsDrain is the
// #7121 review F4 case: a window made only of blocked rows on a superseded
// generation, with more pending rows behind it, counts the drained rows as
// progress and returns instead of widening the scan toward the cap.
func TestSelectPartitionBatchReturnsWithoutWideningWhenSupersededRowsDrain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	const batchLimit = 10
	pending := make([]sharedintent.Row, 0, 3*batchLimit)
	for i := 0; i < 3*batchLimit; i++ {
		repo := fmt.Sprintf("repo-orphan-%02d", i)
		pending = append(pending, runsInRow(repo, "run-old", "gen-old", now))
	}
	reader := &supersededReader{
		stubSharedIntentReader: stubSharedIntentReader{pending: pending},
		superseded:             map[string]struct{}{"gen-old": {}},
	}

	batch, err := SelectPartitionBatch(
		context.Background(), reader, reducercontract.DomainRunsIn,
		0, 1, batchLimit, acceptedGenerationFixed("gen-old", true), nil,
		readinessLookupFixed(false, false), nil, nil,
	)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if !slices.Equal(reader.limitRequests, []int{batchLimit * 2}) {
		t.Fatalf("list limits = %v, want one window read (no widening)", reader.limitRequests)
	}
	if len(batch.StaleIDs) != batchLimit*2 || batch.SupersededGenerationCount != batchLimit*2 {
		t.Fatalf("StaleIDs = %d count = %d, want the whole %d-row window drained",
			len(batch.StaleIDs), batch.SupersededGenerationCount, batchLimit*2)
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
