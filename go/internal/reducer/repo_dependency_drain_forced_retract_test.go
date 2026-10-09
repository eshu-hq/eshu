// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"
)

// failOnceMarkRepoDependencyIntentStore fails the first MarkIntentsCompleted
// call (the crash window: work done, mark failed) and succeeds after, so the
// retry path is observable.
type failOnceMarkRepoDependencyIntentStore struct {
	*coveredRepoDependencyIntentStore
	markCalls int
	markErr   error
}

func (f *failOnceMarkRepoDependencyIntentStore) MarkIntentsCompleted(
	ctx context.Context, intentIDs []string, completedAt time.Time,
) error {
	f.markCalls++
	if f.markCalls == 1 {
		return f.markErr
	}
	return f.coveredRepoDependencyIntentStore.MarkIntentsCompleted(ctx, intentIDs, completedAt)
}

// TestRepoDependencyPureDrainForcesRetract is the #7736 F5 RED test for the
// repo_dependency lane: a pure-drain cycle (every active row covered by an
// emitted full successor, nothing kept) must run retract on the drained scope.
// The lane is otherwise upsert-only (repoDependencyNeedsRetract has no history
// arm), so without the forced drain retract a crash-window partial write ahead
// of the drain lingers: the drained rows complete without a write, and the
// successor's cycle has no stale IDs and no delete actions to force its own
// retract.
func TestRepoDependencyPureDrainForcesRetract(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	repoID := "repository:r_repo_a"
	row := coveredRepoDependencyRow("covered-1", "gen-old", now)
	reader := &coveredRepoDependencyIntentStore{
		fakeRepoDependencyIntentStore: &fakeRepoDependencyIntentStore{
			pendingByDomain:         []SharedProjectionIntentRow{row},
			pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{repoID: {row}},
			leaseGranted:            true,
		},
		covered: map[string]struct{}{"gen-old": {}},
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := RepoDependencyProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen:  acceptedGenerationFixed("gen-old", true),
	}

	result, _, _, err := runner.processAcceptanceUnit(
		context.Background(), now, repoID, reader, PartitionProcessResult{}, now,
	)
	if err != nil {
		t.Fatalf("processAcceptanceUnit() error = %v", err)
	}
	if reader.lookups != 1 {
		t.Fatalf("drain lookups = %d, want exactly 1", reader.lookups)
	}
	if len(writer.retractCalls) == 0 {
		t.Fatalf("len(retractCalls) = 0, want >= 1: a pure-drain cycle must retract the drained scope (#7736 F5)")
	}
	if len(writer.writeCalls) != 0 {
		t.Fatalf("len(writeCalls) = %d, want 0: drained rows still never write", len(writer.writeCalls))
	}
	if len(reader.marked) != 1 || reader.marked[0] != "covered-1" {
		t.Fatalf("marked = %v, want [covered-1]", reader.marked)
	}
	if result.CoveredByFullSuccessorIntents != 1 {
		t.Fatalf("CoveredByFullSuccessorIntents = %d, want 1", result.CoveredByFullSuccessorIntents)
	}
}

// TestRepoDependencyPureStaleWithoutDrainSkipsRetract pins the marker
// precision: an all-stale cycle with no drainable rows keeps the old skip (no
// retract, just mark completed). Only drainable rows force the retract pass.
func TestRepoDependencyPureStaleWithoutDrainSkipsRetract(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	repoID := "repository:r_repo_a"
	row := coveredRepoDependencyRow("stale-1", "gen-old", now)
	// Plain fake: no drain port, so nothing is drainable.
	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain:         []SharedProjectionIntentRow{row},
		pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{repoID: {row}},
		leaseGranted:            true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := RepoDependencyProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen:  acceptedGenerationFixed("gen-new", true),
	}

	_, _, _, err := runner.processAcceptanceUnit(
		context.Background(), now, repoID, reader, PartitionProcessResult{}, now,
	)
	if err != nil {
		t.Fatalf("processAcceptanceUnit() error = %v", err)
	}
	if len(writer.retractCalls) != 0 {
		t.Fatalf("len(retractCalls) = %d, want 0: non-drain stale rows must not force retract", len(writer.retractCalls))
	}
	if len(writer.writeCalls) != 0 {
		t.Fatalf("len(writeCalls) = %d, want 0", len(writer.writeCalls))
	}
	if len(reader.marked) != 1 || reader.marked[0] != "stale-1" {
		t.Fatalf("marked = %v, want [stale-1]", reader.marked)
	}
}

// TestRepoDependencyDrainRetryAfterMarkFailureRetracts injects the F5 crash
// window: the first cycle's completion mark fails after the drain, and the
// retry must run the forced retract before completing.
func TestRepoDependencyDrainRetryAfterMarkFailureRetracts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	repoID := "repository:r_repo_a"
	row := coveredRepoDependencyRow("covered-1", "gen-old", now)
	inner := &coveredRepoDependencyIntentStore{
		fakeRepoDependencyIntentStore: &fakeRepoDependencyIntentStore{
			pendingByDomain:         []SharedProjectionIntentRow{row},
			pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{repoID: {row}},
			leaseGranted:            true,
		},
		covered: map[string]struct{}{"gen-old": {}},
	}
	reader := &failOnceMarkRepoDependencyIntentStore{
		coveredRepoDependencyIntentStore: inner,
		markErr:                          errors.New("injected mark-completed failure"),
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := RepoDependencyProjectionRunner{
		IntentReader: reader,
		LeaseManager: inner,
		EdgeWriter:   writer,
		AcceptedGen:  acceptedGenerationFixed("gen-old", true),
	}

	if _, _, _, err := runner.processAcceptanceUnit(
		context.Background(), now, repoID, reader, PartitionProcessResult{}, now,
	); err == nil {
		t.Fatalf("first processAcceptanceUnit() error = nil, want injected mark failure")
	}
	if _, _, _, err := runner.processAcceptanceUnit(
		context.Background(), now, repoID, reader, PartitionProcessResult{}, now,
	); err != nil {
		t.Fatalf("retry processAcceptanceUnit() error = %v", err)
	}
	if len(writer.retractCalls) == 0 {
		t.Fatalf("len(retractCalls) = 0, want >= 1: the drain retry must run the forced retract (#7736 F5)")
	}
	if len(inner.marked) != 1 || inner.marked[0] != "covered-1" {
		t.Fatalf("marked = %v, want [covered-1]", inner.marked)
	}
}
