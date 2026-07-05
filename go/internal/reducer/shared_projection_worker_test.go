// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

func TestLatestIntentsByRepoAndPartitionDeduplicates(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC)
	intents := []SharedProjectionIntentRow{
		{IntentID: "old-1", RepositoryID: "repo-a", PartitionKey: "pk-1", CreatedAt: t0},
		{IntentID: "new-1", RepositoryID: "repo-a", PartitionKey: "pk-1", CreatedAt: t0.Add(time.Second)},
		{IntentID: "only-1", RepositoryID: "repo-b", PartitionKey: "pk-2", CreatedAt: t0},
	}

	latest, superseded := LatestIntentsByRepoAndPartition(intents)
	if len(latest) != 2 {
		t.Fatalf("latest len = %d, want 2", len(latest))
	}
	if latest[0].IntentID != "new-1" {
		t.Errorf("latest[0].IntentID = %q, want new-1", latest[0].IntentID)
	}
	if latest[1].IntentID != "only-1" {
		t.Errorf("latest[1].IntentID = %q, want only-1", latest[1].IntentID)
	}
	if len(superseded) != 1 || superseded[0] != "old-1" {
		t.Errorf("superseded = %v, want [old-1]", superseded)
	}
}

func TestLatestIntentsByRepoAndPartitionKeepsRefreshFirst(t *testing.T) {
	t.Parallel()

	// Regression for the #3451 refresh-fence wedge (companion to #3474): the
	// indexed candidate SQL hands rows in is_refresh_intent-DESC order so the
	// single repo refresh leads the batch, but the in-memory dedup MUST NOT bury
	// it behind older per-edge upsert rows. A refresh created AFTER its paired
	// edges (the live shape: edges enqueued at ingest, refresh emitted last) must
	// still sort first, or a fixed batch window selects only edges that defer
	// forever behind a refresh that is never selected.
	t0 := time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC)
	intents := []SharedProjectionIntentRow{
		{IntentID: "edge-old", RepositoryID: "repo-a", PartitionKey: "inheritance_edges:file:a.go", CreatedAt: t0},
		{IntentID: "edge-mid", RepositoryID: "repo-a", PartitionKey: "inheritance_edges:file:b.go", CreatedAt: t0.Add(time.Second)},
		{
			IntentID:     "refresh-late",
			RepositoryID: "repo-a",
			PartitionKey: "inheritance_edges:refresh:v1:whole:repo-a",
			CreatedAt:    t0.Add(time.Hour),
			Payload:      map[string]any{"intent_type": repoRefreshIntentType, "action": repoRefreshAction},
		},
	}

	latest, _ := LatestIntentsByRepoAndPartition(intents)
	if len(latest) != 3 {
		t.Fatalf("latest len = %d, want 3 (distinct partition keys, none superseded)", len(latest))
	}
	if latest[0].IntentID != "refresh-late" {
		t.Errorf("latest[0].IntentID = %q, want refresh-late (refresh must lead despite newest created_at)", latest[0].IntentID)
	}
}

func TestLatestIntentsByRepoAndPartitionEmpty(t *testing.T) {
	t.Parallel()

	latest, superseded := LatestIntentsByRepoAndPartition(nil)
	if latest != nil {
		t.Errorf("latest = %v, want nil", latest)
	}
	if superseded != nil {
		t.Errorf("superseded = %v, want nil", superseded)
	}
}

func TestLatestIntentsByRepoAndPartitionTripleSupersede(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC)
	intents := []SharedProjectionIntentRow{
		{IntentID: "v1", RepositoryID: "repo-a", PartitionKey: "pk-1", CreatedAt: t0},
		{IntentID: "v2", RepositoryID: "repo-a", PartitionKey: "pk-1", CreatedAt: t0.Add(time.Second)},
		{IntentID: "v3", RepositoryID: "repo-a", PartitionKey: "pk-1", CreatedAt: t0.Add(2 * time.Second)},
	}

	latest, superseded := LatestIntentsByRepoAndPartition(intents)
	if len(latest) != 1 || latest[0].IntentID != "v3" {
		t.Fatalf("latest = %v, want [v3]", latest)
	}
	if len(superseded) != 2 {
		t.Fatalf("superseded len = %d, want 2", len(superseded))
	}
}

func TestFilterAuthoritativeIntentsMatchesGeneration(t *testing.T) {
	t.Parallel()

	intents := []SharedProjectionIntentRow{
		{IntentID: "active-1", ScopeID: "scope-a", AcceptanceUnitID: "unit-a", RepositoryID: "repo-a", SourceRunID: "run-1", GenerationID: "gen-1"},
		{IntentID: "stale-1", ScopeID: "scope-a", AcceptanceUnitID: "unit-a", RepositoryID: "repo-a", SourceRunID: "run-2", GenerationID: "gen-old"},
		{IntentID: "active-2", ScopeID: "scope-b", AcceptanceUnitID: "unit-b", RepositoryID: "repo-b", SourceRunID: "run-1", GenerationID: "gen-2"},
	}

	lookup := func(key SharedProjectionAcceptanceKey) (string, bool) {
		accepted := map[SharedProjectionAcceptanceKey]string{
			{ScopeID: "scope-a", AcceptanceUnitID: "unit-a", SourceRunID: "run-1"}: "gen-1",
			{ScopeID: "scope-a", AcceptanceUnitID: "unit-a", SourceRunID: "run-2"}: "gen-current",
			{ScopeID: "scope-b", AcceptanceUnitID: "unit-b", SourceRunID: "run-1"}: "gen-2",
		}
		gen, ok := accepted[key]
		return gen, ok
	}

	active, staleIDs := FilterAuthoritativeIntents(intents, lookup)
	if len(active) != 2 {
		t.Fatalf("active len = %d, want 2", len(active))
	}
	if active[0].IntentID != "active-1" {
		t.Errorf("active[0] = %q, want active-1", active[0].IntentID)
	}
	if active[1].IntentID != "active-2" {
		t.Errorf("active[1] = %q, want active-2", active[1].IntentID)
	}
	if len(staleIDs) != 1 || staleIDs[0] != "stale-1" {
		t.Errorf("staleIDs = %v, want [stale-1]", staleIDs)
	}
}

func TestFilterAuthoritativeIntentsSkipsUnknownRepos(t *testing.T) {
	t.Parallel()

	intents := []SharedProjectionIntentRow{
		{IntentID: "unknown-1", ScopeID: "scope-x", AcceptanceUnitID: "unit-x", RepositoryID: "repo-x", SourceRunID: "run-1", GenerationID: "gen-1"},
	}

	lookup := func(SharedProjectionAcceptanceKey) (string, bool) {
		return "", false
	}

	active, staleIDs := FilterAuthoritativeIntents(intents, lookup)
	if len(active) != 0 {
		t.Errorf("active = %v, want empty", active)
	}
	if len(staleIDs) != 0 {
		t.Errorf("staleIDs = %v, want empty", staleIDs)
	}
}

func TestFilterAuthoritativeIntentsUsesScopeAndAcceptanceUnit(t *testing.T) {
	t.Parallel()

	intents := []SharedProjectionIntentRow{
		{
			IntentID:         "active",
			ScopeID:          "scope-a",
			AcceptanceUnitID: "unit-a",
			RepositoryID:     "repo-shared",
			SourceRunID:      "run-1",
			GenerationID:     "gen-a",
		},
		{
			IntentID:         "stale",
			ScopeID:          "scope-b",
			AcceptanceUnitID: "unit-b",
			RepositoryID:     "repo-shared",
			SourceRunID:      "run-1",
			GenerationID:     "gen-a",
		},
	}

	lookup := func(key SharedProjectionAcceptanceKey) (string, bool) {
		if key.ScopeID == "scope-a" && key.AcceptanceUnitID == "unit-a" && key.SourceRunID == "run-1" {
			return "gen-a", true
		}
		if key.ScopeID == "scope-b" && key.AcceptanceUnitID == "unit-b" && key.SourceRunID == "run-1" {
			return "gen-b", true
		}
		return "", false
	}

	active, staleIDs := FilterAuthoritativeIntents(intents, lookup)
	if len(active) != 1 || active[0].IntentID != "active" {
		t.Fatalf("active = %v, want only active", active)
	}
	if len(staleIDs) != 1 || staleIDs[0] != "stale" {
		t.Fatalf("staleIDs = %v, want [stale]", staleIDs)
	}
}

func TestFilterUpsertRows(t *testing.T) {
	t.Parallel()

	rows := []SharedProjectionIntentRow{
		{IntentID: "upsert-1", Payload: map[string]any{"action": "upsert"}},
		{IntentID: "delete-1", Payload: map[string]any{"action": "delete"}},
		{IntentID: "implicit-1", Payload: map[string]any{"platform_id": "p1"}},
		{IntentID: "nil-payload"},
	}

	result := filterUpsertRows(rows)
	if len(result) != 3 {
		t.Fatalf("filterUpsertRows len = %d, want 3", len(result))
	}
	if result[0].IntentID != "upsert-1" {
		t.Errorf("result[0] = %q", result[0].IntentID)
	}
	if result[1].IntentID != "implicit-1" {
		t.Errorf("result[1] = %q", result[1].IntentID)
	}
	if result[2].IntentID != "nil-payload" {
		t.Errorf("result[2] = %q", result[2].IntentID)
	}
}

func TestSelectPartitionBatchReturnsAcceptedBatch(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC)
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			{IntentID: "i1", ProjectionDomain: "platform_infra", PartitionKey: "pk-a", ScopeID: "scope-a", AcceptanceUnitID: "repo-a", RepositoryID: "repo-a", SourceRunID: "run-1", GenerationID: "gen-1", CreatedAt: t0},
			{IntentID: "i2", ProjectionDomain: "platform_infra", PartitionKey: "pk-b", ScopeID: "scope-b", AcceptanceUnitID: "repo-b", RepositoryID: "repo-b", SourceRunID: "run-1", GenerationID: "gen-1", CreatedAt: t0},
		},
	}

	lookup := acceptedGenerationFixed("gen-1", true)

	batch, err := SelectPartitionBatch(
		context.Background(), reader, "platform_infra",
		0, 1, // partition 0 of 1 → all rows match
		10, lookup, nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("SelectPartitionBatch error = %v", err)
	}
	if len(batch.LatestRows) != 2 {
		t.Fatalf("LatestRows len = %d, want 2", len(batch.LatestRows))
	}
}

func TestSelectPartitionBatchFiltersStale(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC)
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			{IntentID: "active-1", ProjectionDomain: "platform_infra", PartitionKey: "pk-a", ScopeID: "scope-a", AcceptanceUnitID: "repo-a", RepositoryID: "repo-a", SourceRunID: "run-1", GenerationID: "gen-current", CreatedAt: t0},
			{IntentID: "stale-1", ProjectionDomain: "platform_infra", PartitionKey: "pk-b", ScopeID: "scope-a", AcceptanceUnitID: "repo-a", RepositoryID: "repo-a", SourceRunID: "run-1", GenerationID: "gen-old", CreatedAt: t0},
		},
	}

	lookup := acceptedGenerationFixed("gen-current", true)

	batch, err := SelectPartitionBatch(
		context.Background(), reader, "platform_infra",
		0, 1, 10, lookup, nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("SelectPartitionBatch error = %v", err)
	}
	if len(batch.LatestRows) != 1 {
		t.Fatalf("LatestRows len = %d, want 1", len(batch.LatestRows))
	}
	if batch.LatestRows[0].IntentID != "active-1" {
		t.Errorf("LatestRows[0].IntentID = %q, want active-1", batch.LatestRows[0].IntentID)
	}
	if len(batch.StaleIDs) != 1 || batch.StaleIDs[0] != "stale-1" {
		t.Errorf("StaleIDs = %v, want [stale-1]", batch.StaleIDs)
	}
}

func TestSelectPartitionBatchExpandsWindowWhenPartitionWorkIsBeyondHeadSlice(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)
	partitionCount := 2
	targetPartition := 1
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			{
				IntentID:         "other-partition-1",
				ProjectionDomain: DomainWorkloadDependency,
				PartitionKey:     partitionKeyForTestPartition(t, 0, partitionCount, "head-a"),
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				CreatedAt:        t0,
			},
			{
				IntentID:         "other-partition-2",
				ProjectionDomain: DomainWorkloadDependency,
				PartitionKey:     partitionKeyForTestPartition(t, 0, partitionCount, "head-b"),
				ScopeID:          "scope-b",
				AcceptanceUnitID: "repo-b",
				RepositoryID:     "repo-b",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				CreatedAt:        t0.Add(time.Second),
			},
			{
				IntentID:         "other-partition-3",
				ProjectionDomain: DomainWorkloadDependency,
				PartitionKey:     partitionKeyForTestPartition(t, 0, partitionCount, "head-c"),
				ScopeID:          "scope-c",
				AcceptanceUnitID: "repo-c",
				RepositoryID:     "repo-c",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				CreatedAt:        t0.Add(2 * time.Second),
			},
			{
				IntentID:         "other-partition-4",
				ProjectionDomain: DomainWorkloadDependency,
				PartitionKey:     partitionKeyForTestPartition(t, 0, partitionCount, "head-d"),
				ScopeID:          "scope-d",
				AcceptanceUnitID: "repo-d",
				RepositoryID:     "repo-d",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				CreatedAt:        t0.Add(3 * time.Second),
			},
			{
				IntentID:         "target-partition-1",
				ProjectionDomain: DomainWorkloadDependency,
				PartitionKey:     partitionKeyForTestPartition(t, targetPartition, partitionCount, "tail"),
				ScopeID:          "scope-target",
				AcceptanceUnitID: "repo-target",
				RepositoryID:     "repo-target",
				SourceRunID:      "run-1",
				GenerationID:     "gen-target",
				CreatedAt:        t0.Add(4 * time.Second),
			},
		},
	}

	batch, err := SelectPartitionBatch(
		context.Background(),
		reader,
		DomainWorkloadDependency,
		targetPartition,
		partitionCount,
		1,
		acceptedGenerationFixed("gen-target", true),
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if len(batch.LatestRows) != 1 || batch.LatestRows[0].IntentID != "target-partition-1" {
		t.Fatalf("LatestRows = %v, want target partition row", batch.LatestRows)
	}
	if len(reader.limitRequests) < 2 {
		t.Fatalf("limitRequests = %v, want widened scan window", reader.limitRequests)
	}
}

func TestSelectPartitionBatchErrorsWhenScanCapIsReached(t *testing.T) {
	t.Parallel()

	reader := &stubSharedIntentReader{
		limitResponder: func(limit int) []SharedProjectionIntentRow {
			rows := make([]SharedProjectionIntentRow, limit)
			for i := range rows {
				rows[i] = SharedProjectionIntentRow{
					IntentID:         "head-intent",
					ProjectionDomain: DomainWorkloadDependency,
					PartitionKey:     partitionKeyForTestPartition(t, 0, 2, "cap"),
					ScopeID:          "scope-a",
					AcceptanceUnitID: "repo-a",
					RepositoryID:     "repo-a",
					SourceRunID:      "run-1",
					GenerationID:     "gen-1",
				}
			}
			return rows
		},
	}

	_, err := SelectPartitionBatch(
		context.Background(),
		reader,
		DomainWorkloadDependency,
		1,
		2,
		1,
		acceptedGenerationFixed("gen-1", true),
		nil,
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("SelectPartitionBatch() error = nil, want non-nil")
	}
	if got, want := reader.limitRequests[len(reader.limitRequests)-1], maxSharedSelectionScanLimit; got != want {
		t.Fatalf("final scan limit = %d, want cap %d", got, want)
	}
}

func TestSelectPartitionBatchSkipsSQLAndInheritanceRowsUntilSemanticNodesCommitted(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 17, 13, 0, 0, 0, time.UTC)
	domains := []string{DomainSQLRelationships, DomainInheritanceEdges}

	for _, domain := range domains {
		domain := domain
		t.Run(domain, func(t *testing.T) {
			t.Parallel()

			reader := &stubSharedIntentReader{
				pending: []SharedProjectionIntentRow{
					{
						IntentID:         "blocked-1",
						ProjectionDomain: domain,
						PartitionKey:     "pk-a",
						ScopeID:          "scope-a",
						AcceptanceUnitID: "repo-a",
						RepositoryID:     "repo-a",
						SourceRunID:      "run-1",
						GenerationID:     "gen-1",
						CreatedAt:        now,
					},
				},
			}

			result, err := SelectPartitionBatch(
				context.Background(),
				reader,
				domain,
				0,
				1,
				100,
				acceptedGenerationFixed("gen-1", true),
				nil,
				readinessLookupFixed(false, false),
				nil,
				nil,
			)
			if err != nil {
				t.Fatalf("SelectPartitionBatch() error = %v", err)
			}
			if len(result.LatestRows) != 0 {
				t.Fatalf("len(LatestRows) = %d, want 0 until semantic readiness exists", len(result.LatestRows))
			}
			if len(result.StaleIDs) != 0 {
				t.Fatalf("StaleIDs = %v, want empty", result.StaleIDs)
			}
			if len(result.SupersededIDs) != 0 {
				t.Fatalf("SupersededIDs = %v, want empty", result.SupersededIDs)
			}
			if got, want := result.BlockedCount, 1; got != want {
				t.Fatalf("BlockedCount = %d, want %d", got, want)
			}
			if len(result.BlockedRows) != 1 {
				t.Fatalf("BlockedRows len = %d, want 1", len(result.BlockedRows))
			}
			if got, want := result.BlockedRows[0].IntentID, "blocked-1"; got != want {
				t.Fatalf("BlockedRows[0].IntentID = %q, want %q", got, want)
			}
		})
	}
}
