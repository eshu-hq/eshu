// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// drainHistoryCodeCallIntentStore combines the drain port with controllable
// completion history, so the F5 tests can hold history at skip-conditions
// while the cycle drains.
type drainHistoryCodeCallIntentStore struct {
	*historyAwareCodeCallIntentStore
	covered map[string]struct{}
	lookups int
}

func (f *drainHistoryCodeCallIntentStore) CoveredByEmittedFullSuccessorIDs(
	_ context.Context, _ string, generationIDs []string,
) (map[string]struct{}, error) {
	f.lookups++
	covered := make(map[string]struct{})
	for _, id := range generationIDs {
		if _, ok := f.covered[id]; ok {
			covered[id] = struct{}{}
		}
	}
	return covered, nil
}

// failOnceMarkCodeCallIntentStore fails the first MarkIntentsCompleted call
// (the crash window: work done, mark failed) and succeeds after, so the retry
// path is observable.
type failOnceMarkCodeCallIntentStore struct {
	*drainHistoryCodeCallIntentStore
	markCalls int
	markErr   error
}

func (f *failOnceMarkCodeCallIntentStore) MarkIntentsCompleted(
	ctx context.Context, intentIDs []string, completedAt time.Time,
) error {
	f.markCalls++
	if f.markCalls == 1 {
		return f.markErr
	}
	return f.drainHistoryCodeCallIntentStore.MarkIntentsCompleted(ctx, intentIDs, completedAt)
}

func acceptedGenOldOnly(key sharedintent.AcceptanceKey) (string, bool) {
	return "gen-old", key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1"
}

// TestCodeCallPureDrainForcesRetract is the #7736 F5 RED test: a pure-drain
// cycle (every active row covered by an emitted full successor, nothing kept)
// must run retract on the drained scope even when completion history sits at
// skip-conditions. A crash-window partial write (write ok, mark failed) ahead
// of the drain would otherwise linger: the drained rows complete without a
// write, and the successor's upsert-only cycle has no stale IDs to force its
// own retract.
func TestCodeCallPureDrainForcesRetract(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	row := coveredCodeCallRow("intent-1", "gen-old", now)
	history := &historyAwareCodeCallIntentStore{
		fakeCodeCallIntentStore: &fakeCodeCallIntentStore{
			pendingByDomain:     []sharedintent.Row{row},
			pendingByAcceptance: map[string][]sharedintent.Row{"scope-a|repo-a|run-1": {row}},
			leaseGranted:        true,
		},
		// Fresh unit: the history predicate would skip retract if it were
		// consulted. The drain marker bypasses it.
		hasCompleted: false,
	}
	reader := &drainHistoryCodeCallIntentStore{
		historyAwareCodeCallIntentStore: history,
		covered:                         map[string]struct{}{"gen-old": {}},
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := Runner{
		IntentReader: reader,
		LeaseManager: history.fakeCodeCallIntentStore,
		EdgeWriter:   writer,
		AcceptedGen:  acceptedGenOldOnly,
		Config:       RunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
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
	if len(reader.marked) != 1 || reader.marked[0] != "intent-1" {
		t.Fatalf("marked = %v, want [intent-1]", reader.marked)
	}
	if result.CoveredByFullSuccessorIntents != 1 {
		t.Fatalf("CoveredByFullSuccessorIntents = %d, want 1", result.CoveredByFullSuccessorIntents)
	}
}

// TestCodeCallPureStaleWithoutDrainSkipsRetract pins the marker precision:
// an all-stale cycle with no drainable rows keeps the old skip (no retract,
// just mark completed). Only drainable rows force the retract pass.
func TestCodeCallPureStaleWithoutDrainSkipsRetract(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	row := coveredCodeCallRow("intent-1", "gen-old", now)
	// Plain fake: no drain port, so nothing is drainable.
	reader := &fakeCodeCallIntentStore{
		pendingByDomain:     []sharedintent.Row{row},
		pendingByAcceptance: map[string][]sharedintent.Row{"scope-a|repo-a|run-1": {row}},
		leaseGranted:        true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := Runner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen: func(key sharedintent.AcceptanceKey) (string, bool) {
			return "gen-new", key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1"
		},
		Config: RunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	_, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if len(writer.retractCalls) != 0 {
		t.Fatalf("len(retractCalls) = %d, want 0: non-drain stale rows must not force retract", len(writer.retractCalls))
	}
	if len(writer.writeCalls) != 0 {
		t.Fatalf("len(writeCalls) = %d, want 0", len(writer.writeCalls))
	}
	if len(reader.marked) != 1 || reader.marked[0] != "intent-1" {
		t.Fatalf("marked = %v, want [intent-1]", reader.marked)
	}
}

// TestCodeCallDrainRetryAfterMarkFailureRetracts injects the F5 crash window:
// the first cycle's completion mark fails after the drain, and the retry must
// run the forced retract before completing.
func TestCodeCallDrainRetryAfterMarkFailureRetracts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	row := coveredCodeCallRow("intent-1", "gen-old", now)
	history := &historyAwareCodeCallIntentStore{
		fakeCodeCallIntentStore: &fakeCodeCallIntentStore{
			pendingByDomain:     []sharedintent.Row{row},
			pendingByAcceptance: map[string][]sharedintent.Row{"scope-a|repo-a|run-1": {row}},
			leaseGranted:        true,
		},
		hasCompleted: false,
	}
	reader := &failOnceMarkCodeCallIntentStore{
		drainHistoryCodeCallIntentStore: &drainHistoryCodeCallIntentStore{
			historyAwareCodeCallIntentStore: history,
			covered:                         map[string]struct{}{"gen-old": {}},
		},
		markErr: errors.New("injected mark-completed failure"),
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := Runner{
		IntentReader: reader,
		LeaseManager: history.fakeCodeCallIntentStore,
		EdgeWriter:   writer,
		AcceptedGen:  acceptedGenOldOnly,
		Config:       RunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	if _, err := runner.processOnce(context.Background(), now); err == nil {
		t.Fatalf("first processOnce() error = nil, want injected mark failure")
	}
	if _, err := runner.processOnce(context.Background(), now); err != nil {
		t.Fatalf("retry processOnce() error = %v", err)
	}
	if len(writer.retractCalls) == 0 {
		t.Fatalf("len(retractCalls) = 0, want >= 1: the drain retry must run the forced retract (#7736 F5)")
	}
	if len(reader.marked) != 1 || reader.marked[0] != "intent-1" {
		t.Fatalf("marked = %v, want [intent-1]", reader.marked)
	}
}
