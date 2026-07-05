// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

func TestSelectPartitionBatchKeepsScanningForReadyRowsWhenEarlierUnitsAreReadinessBlocked(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 17, 13, 30, 0, 0, time.UTC)
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			{
				IntentID:         "blocked-1",
				ProjectionDomain: DomainDocumentationEdges,
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
				ProjectionDomain: DomainDocumentationEdges,
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
		DomainDocumentationEdges,
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

	result, err := ProcessPartitionOnce(context.Background(), now, cfg, lease, reader, edges, lookup, nil, nil, nil, nil, nil, nil)
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
		nil,
		nil,
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

	result, err := ProcessPartitionOnce(context.Background(), now, cfg, lease, reader, edges, lookup, nil, nil, nil, nil, nil, nil)
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

	result, err := ProcessPartitionOnce(context.Background(), now, cfg, lease, reader, edges, lookup, nil, nil, nil, nil, nil, nil)
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

	result, err := ProcessPartitionOnce(context.Background(), now, cfg, lease, reader, edges, lookup, nil, nil, nil, nil, nil, nil)
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

	result, err := ProcessPartitionOnce(context.Background(), now, cfg, lease, reader, edges, lookup, nil, nil, nil, nil, nil, nil)
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
