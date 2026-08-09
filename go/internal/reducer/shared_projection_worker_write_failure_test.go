// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"
)

// errWriteEdges is a GENERIC canonical-edge-write failure — a backend error, a
// timeout, a dropped connection.
//
// Deliberately not the #5984 unroutable case: that one does not error at all.
// An unroutable row is rejected from its persisted payload, so no retry can
// ever succeed, and the final design reports those rows through
// SharedProjectionWriteReport and completes the intent rather than failing.
// This test guards the neighbouring path — a write that failed for a reason
// that COULD succeed on retry must not complete the intent.
var errWriteEdges = errors.New("graph write failed")

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

// TestProcessPartitionOnceDoesNotCompleteIntentsWhenWriteEdgesFails proves the
// worker acts on a write error rather than completing the intent anyway.
//
// It matters because completion is permanent: intent IDs are deterministic and
// the durable upsert never reopens a completed row, so an intent completed
// after a failed write records missing edges as done forever.
//
// The error here is a genuine, retryable write failure (see errWriteEdges) --
// NOT the #5984 unroutable case, which reports its rows and completes. Both
// behaviours have to hold at once: retryable failures must block completion,
// and permanently-unroutable rows must not, or the partition stalls on work
// that can never succeed.
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
