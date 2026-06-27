// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

// stubPartitionCandidateReader implements SharedIntentReader plus the indexed
// and unhashed candidate reader interfaces. It records which selection path the
// runner used so tests can prove the indexed predicate is preferred over the
// in-memory domain scan.
type stubPartitionCandidateReader struct {
	hashed          []SharedProjectionIntentRow
	unhashed        []SharedProjectionIntentRow
	legacyResponder func(limit int) []SharedProjectionIntentRow
	completedIDs    []string
	indexedCalls    int
	legacyCalls     int
	unhashedCalls   int
}

func (s *stubPartitionCandidateReader) ListPendingDomainIntents(_ context.Context, _ string, limit int) ([]SharedProjectionIntentRow, error) {
	s.legacyCalls++
	if s.legacyResponder != nil {
		return s.legacyResponder(limit), nil
	}
	return nil, nil
}

func (s *stubPartitionCandidateReader) ListPendingDomainPartitionIntents(
	_ context.Context,
	_ string,
	partitionID, partitionCount, limit int,
) ([]SharedProjectionIntentRow, error) {
	s.indexedCalls++
	// Mimic the indexed Postgres predicate: only rows whose stable partition
	// hash belongs to the leased partition are returned, bounded by limit.
	matched := RowsForPartition(s.hashed, partitionID, partitionCount)
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (s *stubPartitionCandidateReader) ListPendingDomainUnhashedIntents(_ context.Context, _ string, limit int) ([]SharedProjectionIntentRow, error) {
	s.unhashedCalls++
	rows := s.unhashed
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return append([]SharedProjectionIntentRow(nil), rows...), nil
}

func (s *stubPartitionCandidateReader) MarkIntentsCompleted(_ context.Context, intentIDs []string, _ time.Time) error {
	s.completedIDs = append(s.completedIDs, intentIDs...)
	return nil
}

func TestSelectPartitionBatchUsesIndexedPartitionCandidatesWhenReaderSupportsIt(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, time.June, 17, 12, 0, 0, 0, time.UTC)
	partitionCount := 2
	target := 1
	reader := &stubPartitionCandidateReader{
		hashed: []SharedProjectionIntentRow{
			{
				IntentID:         "target-1",
				ProjectionDomain: DomainWorkloadDependency,
				PartitionKey:     partitionKeyForTestPartition(t, target, partitionCount, "indexed"),
				ScopeID:          "scope-target",
				AcceptanceUnitID: "repo-target",
				RepositoryID:     "repo-target",
				SourceRunID:      "run-1",
				GenerationID:     "gen-target",
				CreatedAt:        t0,
			},
		},
	}

	batch, err := SelectPartitionBatch(
		context.Background(), reader, DomainWorkloadDependency,
		target, partitionCount, 1,
		acceptedGenerationFixed("gen-target", true),
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if len(batch.LatestRows) != 1 || batch.LatestRows[0].IntentID != "target-1" {
		t.Fatalf("LatestRows = %v, want target row", batch.LatestRows)
	}
	if !batch.IndexedSelection {
		t.Fatal("IndexedSelection = false, want true for candidate-reader path")
	}
	if reader.indexedCalls == 0 {
		t.Fatal("indexed candidate reader was not used")
	}
	if reader.legacyCalls != 0 {
		t.Fatalf("legacy domain scan called %d times, want 0 on indexed path", reader.legacyCalls)
	}
}

func TestSelectPartitionBatchDoesNotHitScanCapWithIndexedSelection(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, time.June, 17, 12, 0, 0, 0, time.UTC)
	partitionCount := 2
	target := 1
	// The legacy responder would return a full window of partition-0 head rows,
	// which forces the in-memory scan to the cap and errors. The indexed path
	// must never call it, so the buried target-partition row is found cheaply.
	reader := &stubPartitionCandidateReader{
		hashed: []SharedProjectionIntentRow{
			{
				IntentID:         "buried-target",
				ProjectionDomain: DomainWorkloadDependency,
				PartitionKey:     partitionKeyForTestPartition(t, target, partitionCount, "buried"),
				ScopeID:          "scope-target",
				AcceptanceUnitID: "repo-target",
				RepositoryID:     "repo-target",
				SourceRunID:      "run-1",
				GenerationID:     "gen-target",
				CreatedAt:        t0,
			},
		},
		legacyResponder: func(limit int) []SharedProjectionIntentRow {
			rows := make([]SharedProjectionIntentRow, limit)
			for i := range rows {
				rows[i] = SharedProjectionIntentRow{
					IntentID:         "head",
					ProjectionDomain: DomainWorkloadDependency,
					PartitionKey:     partitionKeyForTestPartition(t, 0, partitionCount, "cap"),
					ScopeID:          "scope-head",
					AcceptanceUnitID: "repo-head",
					RepositoryID:     "repo-head",
					SourceRunID:      "run-1",
					GenerationID:     "gen-target",
				}
			}
			return rows
		},
	}

	batch, err := SelectPartitionBatch(
		context.Background(), reader, DomainWorkloadDependency,
		target, partitionCount, 1,
		acceptedGenerationFixed("gen-target", true),
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v, want nil (indexed selection avoids scan cap)", err)
	}
	if len(batch.LatestRows) != 1 || batch.LatestRows[0].IntentID != "buried-target" {
		t.Fatalf("LatestRows = %v, want buried target row", batch.LatestRows)
	}
	if reader.legacyCalls != 0 {
		t.Fatalf("legacy domain scan called %d times, want 0 on indexed path", reader.legacyCalls)
	}
}

func TestSelectPartitionBatchMergesUnhashedFallbackForIndexedReader(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, time.June, 17, 12, 0, 0, 0, time.UTC)
	partitionCount := 2
	target := 1
	reader := &stubPartitionCandidateReader{
		hashed: []SharedProjectionIntentRow{
			{
				IntentID:         "hashed-a",
				ProjectionDomain: DomainWorkloadDependency,
				PartitionKey:     partitionKeyForTestPartition(t, target, partitionCount, "hashed-a"),
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-target",
				CreatedAt:        t0.Add(time.Second),
			},
		},
		unhashed: []SharedProjectionIntentRow{
			{
				IntentID:         "legacy-b",
				ProjectionDomain: DomainWorkloadDependency,
				PartitionKey:     partitionKeyForTestPartition(t, target, partitionCount, "legacy-b"),
				ScopeID:          "scope-b",
				AcceptanceUnitID: "repo-b",
				RepositoryID:     "repo-b",
				SourceRunID:      "run-1",
				GenerationID:     "gen-target",
				CreatedAt:        t0,
			},
		},
	}

	batch, err := SelectPartitionBatch(
		context.Background(), reader, DomainWorkloadDependency,
		target, partitionCount, 10,
		acceptedGenerationFixed("gen-target", true),
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if len(batch.LatestRows) != 2 {
		t.Fatalf("LatestRows = %v, want hashed + unhashed legacy row", batch.LatestRows)
	}
	// created_at ordering: the legacy row (t0) precedes the hashed row (t0+1s).
	if batch.LatestRows[0].IntentID != "legacy-b" || batch.LatestRows[1].IntentID != "hashed-a" {
		t.Fatalf("LatestRows order = %v, want [legacy-b hashed-a]", batch.LatestRows)
	}
	if batch.UnhashedFallbackRows != 1 {
		t.Fatalf("UnhashedFallbackRows = %d, want 1", batch.UnhashedFallbackRows)
	}
	if reader.unhashedCalls == 0 {
		t.Fatal("unhashed candidate reader was not consulted")
	}
}

func TestAppendUnhashedSharedCandidatesCountsRowsSurvivingTruncation(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, time.June, 17, 12, 0, 0, 0, time.UTC)
	partitionCount := 2
	target := 1
	// The hashed row is the earliest, so after truncating the merged set to the
	// limit, one of the two unhashed rows is dropped. The reported matched count
	// must reflect only the unhashed row that survives, not the pre-truncation
	// total, so the unhashed_fallback_rows signal does not overstate drain.
	hashed := []SharedProjectionIntentRow{
		{
			IntentID:     "hashed-a",
			PartitionKey: partitionKeyForTestPartition(t, target, partitionCount, "hashed-a"),
			CreatedAt:    t0,
		},
	}
	reader := &stubPartitionCandidateReader{
		unhashed: []SharedProjectionIntentRow{
			{
				IntentID:     "legacy-b",
				PartitionKey: partitionKeyForTestPartition(t, target, partitionCount, "legacy-b"),
				CreatedAt:    t0.Add(time.Second),
			},
			{
				IntentID:     "legacy-c",
				PartitionKey: partitionKeyForTestPartition(t, target, partitionCount, "legacy-c"),
				CreatedAt:    t0.Add(2 * time.Second),
			},
		},
	}

	merged, matched, atLimit, err := appendUnhashedSharedCandidates(
		context.Background(), reader, hashed, DomainWorkloadDependency, target, partitionCount, 2,
	)
	if err != nil {
		t.Fatalf("appendUnhashedSharedCandidates() error = %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("merged len = %d, want 2 (truncated to limit)", len(merged))
	}
	if merged[0].IntentID != "hashed-a" || merged[1].IntentID != "legacy-b" {
		t.Fatalf("merged = %v, want [hashed-a legacy-b]", merged)
	}
	if matched != 1 {
		t.Fatalf("matched = %d, want 1 (only legacy-b survives truncation)", matched)
	}
	if !atLimit {
		t.Fatal("atLimit = false, want true (unhashed query filled its window)")
	}
}

func TestSharedPartitionCandidatesReportsAtLimitFromCandidateWindows(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, time.June, 17, 12, 0, 0, 0, time.UTC)
	partitionCount := 2
	target := 1
	reader := &stubPartitionCandidateReader{
		unhashed: []SharedProjectionIntentRow{
			{IntentID: "u1", PartitionKey: partitionKeyForTestPartition(t, target, partitionCount, "u1"), CreatedAt: t0},
			{IntentID: "u2", PartitionKey: partitionKeyForTestPartition(t, target, partitionCount, "u2"), CreatedAt: t0.Add(time.Second)},
		},
	}

	// limit == available unhashed rows: the window is full, so more legacy rows
	// may remain in the database and atLimit must be true.
	_, _, atLimitFull, indexed, err := sharedPartitionCandidates(
		context.Background(), reader, DomainWorkloadDependency, target, partitionCount, 2,
	)
	if err != nil {
		t.Fatalf("sharedPartitionCandidates() error = %v", err)
	}
	if !indexed {
		t.Fatal("indexed = false, want true for candidate reader")
	}
	if !atLimitFull {
		t.Fatal("atLimit = false, want true when the unhashed window is full")
	}

	// limit larger than available: the window is not full, atLimit is false.
	_, _, atLimitPartial, _, err := sharedPartitionCandidates(
		context.Background(), reader, DomainWorkloadDependency, target, partitionCount, 10,
	)
	if err != nil {
		t.Fatalf("sharedPartitionCandidates() error = %v", err)
	}
	if atLimitPartial {
		t.Fatal("atLimit = true, want false when the unhashed window is not full")
	}
}

// TestAppendUnhashedSharedCandidatesRefreshFirstAcrossMerge is a regression
// test for #3474: when the DB has BOTH hashed upsert rows AND an unhashed
// legacy refresh intent, the merged result must place the refresh row first
// even though the refresh intent has a later created_at timestamp than the
// upsert rows.
//
// Before the fix the sort used (created_at ASC, intent_id ASC), so the older
// upsert edges always ranked before the refresh intent.  After the fix the
// sort uses (is_refresh_intent DESC, created_at ASC, intent_id ASC), mirroring
// the ORDER BY in listPendingDomainPartitionIntentsSQL.
func TestAppendUnhashedSharedCandidatesRefreshFirstAcrossMerge(t *testing.T) {
	t.Parallel()

	edgeTime := time.Date(2026, 6, 21, 1, 43, 1, 0, time.UTC)
	refreshTime := edgeTime.Add(5 * time.Minute)

	partitionCount := 2
	target := 1

	// Three upsert edges with older timestamps go into the hashed lane.
	hashed := []SharedProjectionIntentRow{
		{
			IntentID:     "edge-1",
			PartitionKey: partitionKeyForTestPartition(t, target, partitionCount, "edge-1"),
			CreatedAt:    edgeTime,
			Payload:      map[string]any{"action": "upsert"},
		},
		{
			IntentID:     "edge-2",
			PartitionKey: partitionKeyForTestPartition(t, target, partitionCount, "edge-2"),
			CreatedAt:    edgeTime.Add(time.Second),
			Payload:      map[string]any{"action": "upsert"},
		},
	}

	// The refresh intent has a later timestamp and lives in the unhashed lane.
	reader := &stubPartitionCandidateReader{
		unhashed: []SharedProjectionIntentRow{
			{
				IntentID:     "refresh-later",
				PartitionKey: partitionKeyForTestPartition(t, target, partitionCount, "refresh-later"),
				CreatedAt:    refreshTime,
				Payload:      map[string]any{"action": repoRefreshAction, "intent_type": repoRefreshIntentType},
			},
		},
	}

	merged, matched, _, err := appendUnhashedSharedCandidates(
		context.Background(), reader, hashed, DomainWorkloadDependency, target, partitionCount, 10,
	)
	if err != nil {
		t.Fatalf("appendUnhashedSharedCandidates() error = %v", err)
	}
	if len(merged) != 3 {
		t.Fatalf("merged len = %d, want 3", len(merged))
	}
	// Refresh row must sort first despite having a later created_at (#3474).
	if merged[0].IntentID != "refresh-later" {
		t.Fatalf("merged[0].IntentID = %q, want refresh-later (refresh-first ordering broken across hashed+unhashed merge)", merged[0].IntentID)
	}
	if matched != 1 {
		t.Fatalf("matched = %d, want 1", matched)
	}
}

func TestSelectPartitionBatchKeepsLegacyScanWhenReaderUnsupported(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, time.June, 17, 12, 0, 0, 0, time.UTC)
	partitionCount := 2
	target := 1
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			{
				IntentID:         "legacy-target",
				ProjectionDomain: DomainWorkloadDependency,
				PartitionKey:     partitionKeyForTestPartition(t, target, partitionCount, "legacy"),
				ScopeID:          "scope-target",
				AcceptanceUnitID: "repo-target",
				RepositoryID:     "repo-target",
				SourceRunID:      "run-1",
				GenerationID:     "gen-target",
				CreatedAt:        t0,
			},
		},
	}

	batch, err := SelectPartitionBatch(
		context.Background(), reader, DomainWorkloadDependency,
		target, partitionCount, 1,
		acceptedGenerationFixed("gen-target", true),
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if len(batch.LatestRows) != 1 || batch.LatestRows[0].IntentID != "legacy-target" {
		t.Fatalf("LatestRows = %v, want legacy target row", batch.LatestRows)
	}
	if batch.IndexedSelection {
		t.Fatal("IndexedSelection = true, want false for reader without candidate interface")
	}
	if len(reader.limitRequests) == 0 {
		t.Fatal("legacy domain scan was not used for unsupported reader")
	}
}
