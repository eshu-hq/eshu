// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"
)

// reportingEdgeWriter returns a scripted write report, standing in for a
// canonical writer that rejected some rows.
type reportingEdgeWriter struct {
	stubEdgeWriter
	report SharedProjectionWriteReport
	err    error
}

func (w *reportingEdgeWriter) WriteEdges(
	ctx context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	evidenceSource string,
) (SharedProjectionWriteReport, error) {
	_, _ = w.stubEdgeWriter.WriteEdges(ctx, domain, rows, evidenceSource)
	return w.report, w.err
}

// recordingUnroutableWriter captures what the worker persisted.
type recordingUnroutableWriter struct {
	batches [][]SharedProjectionUnroutableRow
	err     error
}

func (w *recordingUnroutableWriter) WriteUnroutableIntents(
	_ context.Context,
	rows []SharedProjectionUnroutableRow,
) error {
	w.batches = append(w.batches, rows)
	return w.err
}

// TestProcessPartitionOnceQuarantinesUnroutableRowsBeforeCompleting is the
// #5984 contract after the owner chose quarantine-and-complete over
// fail-the-intent.
//
// The case it pins is the one neither main nor the first cut of this branch
// fixed: a MIXED batch. Some rows route, so WriteEdges reports no error, and
// the worker completes every latest row in the batch — including the ones that
// produced no edge. Completed intents are never reopened by the durable upsert,
// so without a durable record those rows are lost with nothing saying so.
//
// The order matters as much as the record: persisting must happen BEFORE
// MarkIntentsCompleted, because after completion nothing else will ever say
// the row produced nothing.
func TestProcessPartitionOnceQuarantinesUnroutableRowsBeforeCompleting(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 14, 0, 0, 0, time.UTC)
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			{
				IntentID:         "intent-routable",
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
	quarantine := &recordingUnroutableWriter{}
	edges := &reportingEdgeWriter{
		report: SharedProjectionWriteReport{
			UnroutableRows: []SharedProjectionUnroutableRow{{
				IntentID:         "intent-unroutable",
				ProjectionDomain: "platform_infra",
				ScopeID:          "scope-a",
				GenerationID:     "gen-1",
				Reason:           UnroutableReasonMissingRequiredField,
				DecidedAt:        now,
			}},
		},
	}

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
		nil, nil, nil, nil, nil, nil, quarantine,
	)
	if err != nil {
		t.Fatalf("ProcessPartitionOnce() error = %v, want nil: a mixed batch is not a failure", err)
	}

	if len(quarantine.batches) != 1 {
		t.Fatalf("quarantine writes = %d, want 1", len(quarantine.batches))
	}
	if got, want := len(quarantine.batches[0]), 1; got != want {
		t.Fatalf("quarantined rows = %d, want %d", got, want)
	}
	if got, want := quarantine.batches[0][0].IntentID, "intent-unroutable"; got != want {
		t.Errorf("quarantined intent = %q, want %q", got, want)
	}
	if len(reader.completedIDs) == 0 {
		t.Fatal("no intents completed: the partition must keep draining, not stall")
	}
}

// TestProcessPartitionOnceFailsTheCycleWhenQuarantineWriteFails pins the
// inversion of the existing best-effort quarantine pattern.
//
// persistQuarantinedFacts deliberately never fails its intent, because a
// quarantined fact sits beside a work item that still exists. This is the
// opposite: the intent is about to be completed, after which the durable row is
// the ONLY record that it produced no edge. If the write fails and we complete
// anyway, the silent loss is back in a narrower window.
//
// Failing the cycle is safe because the whole batch re-runs: the retract and
// write are idempotent, and the quarantine upsert is keyed on the intent id.
func TestProcessPartitionOnceFailsTheCycleWhenQuarantineWriteFails(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 14, 0, 0, 0, time.UTC)
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{{
			IntentID:         "intent-routable",
			ProjectionDomain: "platform_infra",
			PartitionKey:     "pk-a",
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-a",
			RepositoryID:     "repo-a",
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Payload:          map[string]any{"platform_id": "p1", "action": "upsert"},
			CreatedAt:        now.Add(-time.Minute),
		}},
	}
	lease := &stubLeaseManager{claimResult: true}
	errQuarantine := errors.New("quarantine store unavailable")
	quarantine := &recordingUnroutableWriter{err: errQuarantine}
	edges := &reportingEdgeWriter{
		report: SharedProjectionWriteReport{
			UnroutableRows: []SharedProjectionUnroutableRow{{
				IntentID: "intent-unroutable",
				Reason:   UnroutableReasonMissingRequiredField,
			}},
		},
	}

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
		nil, nil, nil, nil, nil, nil, quarantine,
	)
	if !errors.Is(err, errQuarantine) {
		t.Fatalf("ProcessPartitionOnce() error = %v, want it to wrap the quarantine failure", err)
	}
	if len(reader.completedIDs) != 0 {
		t.Fatalf(
			"completedIDs = %v, want none: completing after a failed quarantine write restores the silent loss",
			reader.completedIDs,
		)
	}
}

// TestProcessPartitionOnceWithNoUnroutableRowsSkipsTheQuarantineWrite keeps the
// ordinary path free of a pointless store round trip per cycle.
func TestProcessPartitionOnceWithNoUnroutableRowsSkipsTheQuarantineWrite(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 14, 0, 0, 0, time.UTC)
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{{
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
		}},
	}
	lease := &stubLeaseManager{claimResult: true}
	quarantine := &recordingUnroutableWriter{}

	cfg := PartitionProcessorConfig{
		Domain:         "platform_infra",
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
		EvidenceSource: "finalization/workloads",
	}

	if _, err := ProcessPartitionOnce(
		context.Background(), now, cfg, lease, reader, &reportingEdgeWriter{},
		acceptedGenerationFixed("gen-1", true),
		nil, nil, nil, nil, nil, nil, quarantine,
	); err != nil {
		t.Fatalf("ProcessPartitionOnce() error = %v", err)
	}
	if len(quarantine.batches) != 0 {
		t.Fatalf("quarantine writes = %d, want 0 when nothing was rejected", len(quarantine.batches))
	}
}
