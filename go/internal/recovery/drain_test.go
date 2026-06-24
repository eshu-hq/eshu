// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recovery

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestHandlerDrainBacklogReplaysOnlyDrainableClasses is the core regression for
// issue #3560's backlog drain: an operator draining the dead-letter backlog with
// no explicit failure class must replay only the safe, drainable transient
// bucket (retry_exhausted) and must NEVER touch the manual-review/terminal
// buckets (projection_bug, resource_exhausted) that #3557 marked unsafe. A blind
// drain of poison would re-fail immediately or re-exhaust a constrained resource.
func TestHandlerDrainBacklogReplaysOnlyDrainableClasses(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{
		drainDepth: 376,
		replayResult: ReplayResult{
			Stage:       StageProjector,
			Replayed:    224,
			WorkItemIDs: []string{"item-1", "item-2"},
		},
	}
	handler := mustNewHandler(t, store)

	result, err := handler.DrainBacklog(context.Background(), DrainFilter{
		Stage: StageProjector,
		Limit: 500,
	})
	if err != nil {
		t.Fatalf("DrainBacklog() error = %v, want nil", err)
	}
	if result.Replayed != 224 {
		t.Fatalf("DrainBacklog() Replayed = %d, want 224", result.Replayed)
	}
	if result.BacklogDepthBefore != 376 {
		t.Fatalf("DrainBacklog() BacklogDepthBefore = %d, want 376", result.BacklogDepthBefore)
	}

	// The drain must hand the store the manual-review classes as a hard
	// exclusion so an unscoped drain can never replay poison.
	excluded := map[string]bool{}
	for _, class := range store.replayFilter.ExcludeFailureClasses {
		excluded[class] = true
	}
	if len(excluded) == 0 {
		t.Fatal("DrainBacklog() passed no ExcludeFailureClasses; poison buckets are unguarded")
	}
	for _, manualReview := range manualReviewClassesForTest() {
		if !excluded[manualReview] {
			t.Fatalf("DrainBacklog() did not exclude manual-review class %q; blind drain would replay poison", manualReview)
		}
	}
}

// TestHandlerDrainBacklogDefaultsToSafeBucket proves that, absent an explicit
// failure class, the drain targets the retry_exhausted bucket — the safe
// transient pile the issue calls out — rather than every dead-letter row.
func TestHandlerDrainBacklogDefaultsToSafeBucket(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{}
	handler := mustNewHandler(t, store)

	if _, err := handler.DrainBacklog(context.Background(), DrainFilter{Stage: StageReducer}); err != nil {
		t.Fatalf("DrainBacklog() error = %v, want nil", err)
	}
	if store.replayFilter.FailureClass != string(DrainableClassRetryExhausted) {
		t.Fatalf("DrainBacklog() default FailureClass = %q, want %q",
			store.replayFilter.FailureClass, DrainableClassRetryExhausted)
	}
}

// TestHandlerDrainBacklogRefusesUnsafeExplicitClass proves an operator cannot
// target a manual-review class through the drain path: requesting projection_bug
// is refused rather than silently draining poison.
func TestHandlerDrainBacklogRefusesUnsafeExplicitClass(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{}
	handler := mustNewHandler(t, store)

	_, err := handler.DrainBacklog(context.Background(), DrainFilter{
		Stage:        StageProjector,
		FailureClass: "projection_bug",
	})
	if err == nil {
		t.Fatal("DrainBacklog() with projection_bug error = nil, want refusal")
	}
	if store.drainDepthCalled || store.replayCalled {
		t.Fatal("DrainBacklog() touched the store for an unsafe class; it must refuse before any read or write")
	}
}

// TestHandlerDrainBacklogRejectsInvalidStage proves the drain validates its
// filter before reading depth or replaying.
func TestHandlerDrainBacklogRejectsInvalidStage(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{}
	handler := mustNewHandler(t, store)

	if _, err := handler.DrainBacklog(context.Background(), DrainFilter{}); err == nil {
		t.Fatal("DrainBacklog() with empty stage error = nil, want non-nil")
	}
}

// TestHandlerDrainBacklogPropagatesReplayError proves a store replay failure is
// surfaced, not swallowed, so an operator never reads a false "drained" result.
func TestHandlerDrainBacklogPropagatesReplayError(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("database unavailable")
	store := &fakeReplayStore{replayErr: storeErr}
	handler := mustNewHandler(t, store)

	_, err := handler.DrainBacklog(context.Background(), DrainFilter{Stage: StageProjector})
	if !errors.Is(err, storeErr) {
		t.Fatalf("DrainBacklog() error = %v, want %v", err, storeErr)
	}
}

func manualReviewClassesForTest() []string {
	return []string{"projection_bug", "resource_exhausted"}
}

var _ = time.Now
