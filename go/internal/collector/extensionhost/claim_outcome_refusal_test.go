// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

var testCommitRefusal = collector.CommitRefusal{Reason: "destructive_successor_refused", Rule: "collector_ratio"}

// TestObserveClaimedGenerationRefusedWritesTheStashedOutcomeOnce proves a
// refusal consumes the stash exactly as a commit does, reaches the recorder as
// a refusal, and never as a commit (#10037).
func TestObserveClaimedGenerationRefusedWritesTheStashedOutcomeOnce(t *testing.T) {
	t.Parallel()

	recorder := &recordingOutcomeRecorder{}
	item := testWorkItem()
	source := mustNewSourceWithOutcomeRecorder(t, &recordingRunner{result: completeResult(item, testSDKFact(item))}, nil, recorder)
	if _, ok, err := source.NextClaimed(context.Background(), item); err != nil || !ok {
		t.Fatalf("NextClaimed() ok = %v, err = %v", ok, err)
	}

	for range 2 {
		if err := source.ObserveClaimedGenerationRefused(context.Background(), item, testCommitRefusal); err != nil {
			t.Fatalf("ObserveClaimedGenerationRefused() error = %v, want nil", err)
		}
	}

	if got := len(recorder.refused); got != 1 {
		t.Fatalf("refused calls = %d, want 1: the stash is consumed once", got)
	}
	if got := recorder.refused[0]; got.Item.WorkItemID != item.WorkItemID {
		t.Fatalf("refused outcome item = %q, want %q", got.Item.WorkItemID, item.WorkItemID)
	}
	if got := recorder.refusals[0]; got != testCommitRefusal {
		t.Fatalf("refusal = %+v, want %+v", got, testCommitRefusal)
	}
	if got := len(recorder.committed); got != 0 {
		t.Fatalf("committed calls = %d, want 0 on a refusal", got)
	}
	if got := source.pendingOutcomes.len(); got != 0 {
		t.Fatalf("pending outcomes = %d, want 0", got)
	}
	// A commit hook after the refusal has nothing left to write.
	if err := source.ObserveClaimedGenerationCommitted(context.Background(), item); err != nil {
		t.Fatalf("ObserveClaimedGenerationCommitted() error = %v", err)
	}
	if got := len(recorder.committed); got != 0 {
		t.Fatalf("committed calls after a refusal = %d, want 0", got)
	}
}

// TestObserveClaimedGenerationRefusedSkipsWithoutAMatchingOutcome covers the
// no-op paths: no recorder, no stash, and a stash for another generation, which
// stays in place for its own claim.
func TestObserveClaimedGenerationRefusedSkipsWithoutAMatchingOutcome(t *testing.T) {
	t.Parallel()

	item := testWorkItem()

	t.Run("no recorder", func(t *testing.T) {
		t.Parallel()
		source := mustNewSourceWithOutcomeRecorder(t, &recordingRunner{result: completeResult(item, testSDKFact(item))}, nil, nil)
		if err := source.ObserveClaimedGenerationRefused(context.Background(), item, testCommitRefusal); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
	})
	t.Run("nothing stashed", func(t *testing.T) {
		t.Parallel()
		recorder := &recordingOutcomeRecorder{}
		source := mustNewSourceWithOutcomeRecorder(t, &recordingRunner{result: completeResult(item, testSDKFact(item))}, nil, recorder)
		if err := source.ObserveClaimedGenerationRefused(context.Background(), item, testCommitRefusal); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if got := len(recorder.refused); got != 0 {
			t.Fatalf("refused calls = %d, want 0", got)
		}
	})
	t.Run("another generation", func(t *testing.T) {
		t.Parallel()
		recorder := &recordingOutcomeRecorder{}
		source := mustNewSourceWithOutcomeRecorder(t, &recordingRunner{result: completeResult(item, testSDKFact(item))}, nil, recorder)
		if _, ok, err := source.NextClaimed(context.Background(), item); err != nil || !ok {
			t.Fatalf("NextClaimed() ok = %v, err = %v", ok, err)
		}
		other := workflow.WorkItem{WorkItemID: item.WorkItemID, GenerationID: item.GenerationID + "-other"}
		if err := source.ObserveClaimedGenerationRefused(context.Background(), other, testCommitRefusal); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if got := len(recorder.refused); got != 0 {
			t.Fatalf("refused calls = %d, want 0", got)
		}
		if got := source.pendingOutcomes.len(); got != 1 {
			t.Fatalf("pending outcomes = %d, want the other generation's entry kept", got)
		}
	})
}

// TestObserveClaimedGenerationRefusedReturnsTheRecorderError proves the error
// reaches ClaimedService, which counts it and completes the claim.
func TestObserveClaimedGenerationRefusedReturnsTheRecorderError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("store down")
	recorder := &recordingOutcomeRecorder{refusedErr: wantErr}
	item := testWorkItem()
	source := mustNewSourceWithOutcomeRecorder(t, &recordingRunner{result: completeResult(item, testSDKFact(item))}, nil, recorder)
	if _, ok, err := source.NextClaimed(context.Background(), item); err != nil || !ok {
		t.Fatalf("NextClaimed() ok = %v, err = %v", ok, err)
	}
	if err := source.ObserveClaimedGenerationRefused(context.Background(), item, testCommitRefusal); !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}
