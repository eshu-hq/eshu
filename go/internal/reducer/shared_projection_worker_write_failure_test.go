// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"
)

// errWriteEdges is the failure a canonical edge writer reports when it could
// not write the batch — for #5984, a batch whose every row was unroutable.
var errWriteEdges = errors.New("all rows were unroutable")

type failingWriteEdgeWriter struct {
	stubEdgeWriter
	err error
}

func (f *failingWriteEdgeWriter) WriteEdges(
	ctx context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	evidenceSource string,
) (SharedProjectionWriteReport, error) {
	_, _ = f.stubEdgeWriter.WriteEdges(ctx, domain, rows, evidenceSource)
	return SharedProjectionWriteReport{}, f.err
}

// TestProcessPartitionOnceDoesNotCompleteIntentsWhenWriteEdgesFails is the
// other half of the #5984 contract. EdgeWriter.WriteEdges now reports an error
// when a non-empty batch produced no write at all; this proves the worker acts
// on that error rather than completing the intent anyway.
//
// It matters because completion is permanent: intent IDs are deterministic and
// the durable upsert never reopens a completed row, so an intent completed
// after a failed write records missing edges as done forever.
func TestProcessPartitionOnceDoesNotCompleteIntentsWhenWriteEdgesFails(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 13, 14, 0, 0, 0, time.UTC)
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
				CreatedAt:        now.Add(-time.Minute),
			},
		},
	}
	lease := &stubLeaseManager{claimResult: true}
	edges := &failingWriteEdgeWriter{err: errWriteEdges}

	cfg := PartitionProcessorConfig{
		Domain:         "platform_infra",
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
		EvidenceSource: "finalization/workloads",
	}

	_, err := ProcessPartitionOnce(
		context.Background(), now, cfg, lease, reader, edges,
		acceptedGenerationFixed("gen-1", true),
		nil, nil, nil, nil, nil, nil,
		nil,
	)
	if err == nil {
		t.Fatal("ProcessPartitionOnce() error = nil, want the write failure to propagate")
	}
	if !errors.Is(err, errWriteEdges) {
		t.Fatalf("ProcessPartitionOnce() error = %v, want it to wrap the write failure", err)
	}
	if len(reader.completedIDs) != 0 {
		t.Fatalf("completedIDs = %v, want none: a failed write must not complete the intent", reader.completedIDs)
	}
	if len(edges.writeCalls) != 1 {
		t.Fatalf("writeCalls = %d, want 1", len(edges.writeCalls))
	}
}
