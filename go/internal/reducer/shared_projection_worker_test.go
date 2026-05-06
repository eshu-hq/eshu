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
		10, lookup, nil, nil, nil,
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
		0, 1, 10, lookup, nil, nil, nil,
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
				ProjectionDomain: DomainPlatformInfra,
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
				ProjectionDomain: DomainPlatformInfra,
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
				ProjectionDomain: DomainPlatformInfra,
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
				ProjectionDomain: DomainPlatformInfra,
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
				ProjectionDomain: DomainPlatformInfra,
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
		DomainPlatformInfra,
		targetPartition,
		partitionCount,
		1,
		acceptedGenerationFixed("gen-target", true),
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
					ProjectionDomain: DomainPlatformInfra,
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
		DomainPlatformInfra,
		1,
		2,
		1,
		acceptedGenerationFixed("gen-1", true),
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

func TestSelectPartitionBatchKeepsScanningForReadyRowsWhenEarlierUnitsAreReadinessBlocked(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 17, 13, 30, 0, 0, time.UTC)
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			{
				IntentID:         "blocked-1",
				ProjectionDomain: DomainSQLRelationships,
				PartitionKey:     "pk-a",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-blocked",
				RepositoryID:     "repo-blocked",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				CreatedAt:        now,
			},
			{
				IntentID:         "ready-1",
				ProjectionDomain: DomainSQLRelationships,
				PartitionKey:     "pk-b",
				ScopeID:          "scope-b",
				AcceptanceUnitID: "repo-ready",
				RepositoryID:     "repo-ready",
				SourceRunID:      "run-2",
				GenerationID:     "gen-2",
				CreatedAt:        now.Add(time.Second),
			},
		},
	}

	result, err := SelectPartitionBatch(
		context.Background(),
		reader,
		DomainSQLRelationships,
		0,
		1,
		10,
		func(key SharedProjectionAcceptanceKey) (string, bool) {
			switch key.AcceptanceUnitID {
			case "repo-blocked":
				return "gen-1", true
			case "repo-ready":
				return "gen-2", true
			default:
				return "", false
			}
		},
		nil,
		func(key GraphProjectionPhaseKey, phase GraphProjectionPhase) (bool, bool) {
			if phase != GraphProjectionPhaseSemanticNodesCommitted {
				t.Fatalf("phase = %q, want %q", phase, GraphProjectionPhaseSemanticNodesCommitted)
			}
			if key.AcceptanceUnitID == "repo-ready" {
				return true, true
			}
			return false, false
		},
		nil,
	)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if len(result.LatestRows) != 1 {
		t.Fatalf("len(LatestRows) = %d, want 1 ready row", len(result.LatestRows))
	}
	if got, want := result.BlockedCount, 1; got != want {
		t.Fatalf("BlockedCount = %d, want %d", got, want)
	}
	if len(result.BlockedRows) != 1 {
		t.Fatalf("BlockedRows len = %d, want 1", len(result.BlockedRows))
	}
	if got, want := result.LatestRows[0].IntentID, "ready-1"; got != want {
		t.Fatalf("LatestRows[0].IntentID = %q, want %q", got, want)
	}
}

func TestProcessPartitionOnceFullCycle(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 13, 14, 0, 0, 0, time.UTC)
	t0 := now.Add(-time.Minute)

	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			{
				IntentID:         "intent-1",
				ProjectionDomain: "platform_infra",
				PartitionKey:     "pk-a",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload:          map[string]any{"platform_id": "p1", "action": "upsert"},
				CreatedAt:        t0,
			},
		},
	}

	lease := &stubLeaseManager{claimResult: true}
	edges := &stubEdgeWriter{}

	lookup := acceptedGenerationFixed("gen-1", true)

	cfg := PartitionProcessorConfig{
		Domain:         "platform_infra",
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
		EvidenceSource: "finalization/workloads",
	}

	result, err := ProcessPartitionOnce(context.Background(), now, cfg, lease, reader, edges, lookup, nil, nil, nil)
	if err != nil {
		t.Fatalf("ProcessPartitionOnce error = %v", err)
	}
	if !result.LeaseAcquired {
		t.Fatal("LeaseAcquired = false, want true")
	}
	if result.ProcessedIntents != 1 {
		t.Errorf("ProcessedIntents = %d, want 1", result.ProcessedIntents)
	}
	if result.UpsertedRows != 1 {
		t.Errorf("UpsertedRows = %d, want 1", result.UpsertedRows)
	}
	if result.RetractedRows != 1 {
		t.Errorf("RetractedRows = %d, want 1", result.RetractedRows)
	}
	if !lease.released {
		t.Error("lease was not released")
	}
	if len(reader.completedIDs) != 1 {
		t.Errorf("completedIDs = %v, want [intent-1]", reader.completedIDs)
	}
	if len(edges.retractCalls) != 1 {
		t.Errorf("retractCalls = %d, want 1", len(edges.retractCalls))
	}
	if len(edges.writeCalls) != 1 {
		t.Errorf("writeCalls = %d, want 1", len(edges.writeCalls))
	}
	if got, want := result.MaxIntentWaitSeconds, 60.0; got != want {
		t.Errorf("MaxIntentWaitSeconds = %.3f, want %.3f", got, want)
	}
	if result.ProcessingDurationSeconds < 0 {
		t.Errorf("ProcessingDurationSeconds = %.3f, want non-negative", result.ProcessingDurationSeconds)
	}
}

func TestProcessPartitionOnceReportsReadinessBlockedWait(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 17, 14, 0, 0, 0, time.UTC)
	t0 := now.Add(-5 * time.Minute)
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			{
				IntentID:         "blocked-1",
				ProjectionDomain: DomainSQLRelationships,
				PartitionKey:     "pk-a",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				CreatedAt:        t0,
			},
		},
	}
	lease := &stubLeaseManager{claimResult: true}
	edges := &stubEdgeWriter{}

	cfg := PartitionProcessorConfig{
		Domain:         DomainSQLRelationships,
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
	}

	result, err := ProcessPartitionOnce(
		context.Background(),
		now,
		cfg,
		lease,
		reader,
		edges,
		acceptedGenerationFixed("gen-1", true),
		nil,
		readinessLookupFixed(false, false),
		nil,
	)
	if err != nil {
		t.Fatalf("ProcessPartitionOnce() error = %v", err)
	}
	if got, want := result.BlockedReadiness, 1; got != want {
		t.Fatalf("BlockedReadiness = %d, want %d", got, want)
	}
	if got, want := result.ProcessedIntents, 0; got != want {
		t.Fatalf("ProcessedIntents = %d, want %d", got, want)
	}
	if got, want := result.MaxBlockedIntentWaitSeconds, 300.0; got != want {
		t.Fatalf("MaxBlockedIntentWaitSeconds = %.3f, want %.3f", got, want)
	}
	if len(reader.completedIDs) != 0 {
		t.Fatalf("completedIDs = %v, want empty while readiness blocked", reader.completedIDs)
	}
}

func TestProcessPartitionOnceLeaseNotAcquired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 13, 14, 0, 0, 0, time.UTC)
	lease := &stubLeaseManager{claimResult: false}
	reader := &stubSharedIntentReader{}
	edges := &stubEdgeWriter{}
	lookup := acceptedGenerationFixed("", false)

	cfg := PartitionProcessorConfig{
		Domain:         "platform_infra",
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
	}

	result, err := ProcessPartitionOnce(context.Background(), now, cfg, lease, reader, edges, lookup, nil, nil, nil)
	if err != nil {
		t.Fatalf("ProcessPartitionOnce error = %v", err)
	}
	if result.LeaseAcquired {
		t.Error("LeaseAcquired = true, want false")
	}
	if result.ProcessedIntents != 0 {
		t.Errorf("ProcessedIntents = %d, want 0", result.ProcessedIntents)
	}
}

func TestProcessPartitionOnceEmptyBatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 13, 14, 0, 0, 0, time.UTC)
	lease := &stubLeaseManager{claimResult: true}
	reader := &stubSharedIntentReader{pending: nil}
	edges := &stubEdgeWriter{}
	lookup := acceptedGenerationFixed("gen-1", true)

	cfg := PartitionProcessorConfig{
		Domain:         "platform_infra",
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
	}

	result, err := ProcessPartitionOnce(context.Background(), now, cfg, lease, reader, edges, lookup, nil, nil, nil)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if !result.LeaseAcquired {
		t.Error("LeaseAcquired = false")
	}
	if result.ProcessedIntents != 0 {
		t.Errorf("ProcessedIntents = %d", result.ProcessedIntents)
	}
	if !lease.released {
		t.Error("lease was not released on empty batch")
	}
}

func TestProcessPartitionOnceFiltersDeleteAction(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 13, 14, 0, 0, 0, time.UTC)
	t0 := now.Add(-time.Minute)

	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			{
				IntentID:         "upsert-1",
				ProjectionDomain: "platform_infra",
				PartitionKey:     "pk-a",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload:          map[string]any{"action": "upsert"},
				CreatedAt:        t0,
			},
			{
				IntentID:         "delete-1",
				ProjectionDomain: "platform_infra",
				PartitionKey:     "pk-b",
				ScopeID:          "scope-b",
				AcceptanceUnitID: "repo-b",
				RepositoryID:     "repo-b",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload:          map[string]any{"action": "delete"},
				CreatedAt:        t0,
			},
		},
	}

	lease := &stubLeaseManager{claimResult: true}
	edges := &stubEdgeWriter{}
	lookup := acceptedGenerationFixed("gen-1", true)

	cfg := PartitionProcessorConfig{
		Domain:         "platform_infra",
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
		EvidenceSource: "finalization/workloads",
	}

	result, err := ProcessPartitionOnce(context.Background(), now, cfg, lease, reader, edges, lookup, nil, nil, nil)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if result.UpsertedRows != 1 {
		t.Errorf("UpsertedRows = %d, want 1 (delete should be filtered)", result.UpsertedRows)
	}
	if result.RetractedRows != 2 {
		t.Errorf("RetractedRows = %d, want 2 (both get retracted)", result.RetractedRows)
	}
}

func TestProcessPartitionOnceCodeCallsDomain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 13, 14, 0, 0, 0, time.UTC)
	t0 := now.Add(-time.Minute)

	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			{
				IntentID:         "intent-1",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "entity:function:caller",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload: map[string]any{
					"repo_id":          "repo-a",
					"caller_entity_id": "entity:function:caller",
					"callee_entity_id": "entity:function:callee",
					"action":           "upsert",
				},
				CreatedAt: t0,
			},
		},
	}

	lease := &stubLeaseManager{claimResult: true}
	edges := &stubEdgeWriter{}
	lookup := acceptedGenerationFixed("gen-1", true)

	cfg := PartitionProcessorConfig{
		Domain:         DomainCodeCalls,
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
		EvidenceSource: "parser/code-calls",
	}

	result, err := ProcessPartitionOnce(context.Background(), now, cfg, lease, reader, edges, lookup, nil, nil, nil)
	if err != nil {
		t.Fatalf("ProcessPartitionOnce() error = %v", err)
	}
	if result.UpsertedRows != 1 {
		t.Fatalf("UpsertedRows = %d, want 1", result.UpsertedRows)
	}
	if result.RetractedRows != 1 {
		t.Fatalf("RetractedRows = %d, want 1", result.RetractedRows)
	}
	if got := len(edges.writeCalls); got != 1 {
		t.Fatalf("writeCalls = %d, want 1", got)
	}
	if got := len(edges.retractCalls); got != 1 {
		t.Fatalf("retractCalls = %d, want 1", got)
	}
}

// --- Test stubs ---

type stubSharedIntentReader struct {
	pending        []SharedProjectionIntentRow
	completedIDs   []string
	limitRequests  []int
	limitResponder func(limit int) []SharedProjectionIntentRow
}

func (s *stubSharedIntentReader) ListPendingDomainIntents(_ context.Context, _ string, limit int) ([]SharedProjectionIntentRow, error) {
	s.limitRequests = append(s.limitRequests, limit)
	if s.limitResponder != nil {
		return s.limitResponder(limit), nil
	}
	if limit > 0 && len(s.pending) > limit {
		return append([]SharedProjectionIntentRow(nil), s.pending[:limit]...), nil
	}
	return append([]SharedProjectionIntentRow(nil), s.pending...), nil
}

func (s *stubSharedIntentReader) MarkIntentsCompleted(_ context.Context, intentIDs []string, _ time.Time) error {
	s.completedIDs = append(s.completedIDs, intentIDs...)
	return nil
}

type stubLeaseManager struct {
	claimResult bool
	released    bool
}

func (s *stubLeaseManager) ClaimPartitionLease(_ context.Context, _ string, _, _ int, _ string, _ time.Duration) (bool, error) {
	return s.claimResult, nil
}

func (s *stubLeaseManager) ReleasePartitionLease(_ context.Context, _ string, _, _ int, _ string) error {
	s.released = true
	return nil
}

type stubEdgeWriter struct {
	retractCalls [][]SharedProjectionIntentRow
	writeCalls   [][]SharedProjectionIntentRow
}

func (s *stubEdgeWriter) RetractEdges(_ context.Context, _ string, rows []SharedProjectionIntentRow, _ string) error {
	s.retractCalls = append(s.retractCalls, rows)
	return nil
}

func (s *stubEdgeWriter) WriteEdges(_ context.Context, _ string, rows []SharedProjectionIntentRow, _ string) error {
	s.writeCalls = append(s.writeCalls, rows)
	return nil
}

func partitionKeyForTestPartition(t *testing.T, wantPartition, partitionCount int, prefix string) string {
	t.Helper()

	for i := 0; i < 10_000; i++ {
		key := prefix + "-" + time.Date(2026, time.April, 17, 0, 0, i%60, 0, time.UTC).Format("150405") + "-" + string(rune('a'+(i%26)))
		got, err := PartitionForKey(key, partitionCount)
		if err != nil {
			t.Fatalf("PartitionForKey(%q) error = %v", key, err)
		}
		if got == wantPartition {
			return key
		}
	}
	t.Fatalf("could not find partition key for partition %d of %d", wantPartition, partitionCount)
	return ""
}
