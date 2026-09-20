// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recovery

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeWriterShapeStore is an in-memory WriterShapeStore double.
type fakeWriterShapeStore struct {
	applied    int
	appliedErr error
	claimWon   bool
	claimErr   error
	claims     int
	marked     []int
	markErr    error
	released   int
	releaseErr error
}

func (f *fakeWriterShapeStore) AppliedVersion(_ context.Context, _ string) (int, error) {
	return f.applied, f.appliedErr
}

func (f *fakeWriterShapeStore) ClaimVersion(_ context.Context, _ string, _ int, _ time.Duration) (bool, error) {
	f.claims++
	return f.claimWon, f.claimErr
}

func (f *fakeWriterShapeStore) MarkAppliedVersion(_ context.Context, _ string, version int) error {
	if f.markErr != nil {
		return f.markErr
	}
	f.marked = append(f.marked, version)
	return nil
}

func (f *fakeWriterShapeStore) ReleaseClaim(_ context.Context, _ string) error {
	f.released++
	return f.releaseErr
}

// TestEnsureGraphWriterShapeSkipsCurrentVersion proves a binary already at
// the code's writer shape performs no refinalize: the common startup path
// must stay a cheap marker read.
func TestEnsureGraphWriterShapeSkipsCurrentVersion(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{}
	handler := mustNewHandler(t, store)
	shapes := &fakeWriterShapeStore{applied: 1, claimWon: true}

	retired, err := handler.EnsureGraphWriterShape(context.Background(), shapes, WriterShapeKey, 1)
	if err != nil {
		t.Fatalf("EnsureGraphWriterShape error = %v, want nil", err)
	}
	if retired {
		t.Fatalf("retired = true, want false (marker already at version 1)")
	}
	if store.refinalizeFilter.AllScopes {
		t.Fatalf("refinalize ran on a current marker")
	}
	if shapes.claims != 0 {
		t.Fatalf("claims = %d, want 0 (no claim attempt on a current marker)", shapes.claims)
	}
}

// TestEnsureGraphWriterShapeRefinalizesOnUpgrade proves the upgrade path:
// a stale marker plus a won claim runs a full all-scopes refinalize and
// advances the marker, so the next drain reprojects with the fixed
// writers (issue #6868).
func TestEnsureGraphWriterShapeRefinalizesOnUpgrade(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{
		refinalizeResult: RefinalizeResult{Enqueued: 7, ScopeIDs: []string{"s1"}},
	}
	handler := mustNewHandler(t, store)
	shapes := &fakeWriterShapeStore{applied: 0, claimWon: true}

	retired, err := handler.EnsureGraphWriterShape(context.Background(), shapes, WriterShapeKey, 1)
	if err != nil {
		t.Fatalf("EnsureGraphWriterShape error = %v, want nil", err)
	}
	if !retired {
		t.Fatalf("retired = false, want true (stale marker, claim won)")
	}
	if !store.refinalizeFilter.AllScopes {
		t.Fatalf("refinalize filter = %+v, want AllScopes", store.refinalizeFilter)
	}
	if len(store.refinalizeFilter.ScopeIDs) != 0 {
		t.Fatalf("refinalize scope_ids = %v, want empty with AllScopes", store.refinalizeFilter.ScopeIDs)
	}
	if len(shapes.marked) != 1 || shapes.marked[0] != 1 {
		t.Fatalf("marked = %v, want [1] (marker advances only after refinalize)", shapes.marked)
	}
	if shapes.released != 0 {
		t.Fatalf("released = %d, want 0 (no release on success)", shapes.released)
	}
}

// TestEnsureGraphWriterShapeLostClaimSkipsRefinalize proves exactly-once
// semantics across concurrently starting binaries: the loser of the atomic
// claim performs no refinalize and advances no marker.
func TestEnsureGraphWriterShapeLostClaimSkipsRefinalize(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{}
	handler := mustNewHandler(t, store)
	shapes := &fakeWriterShapeStore{applied: 0, claimWon: false}

	retired, err := handler.EnsureGraphWriterShape(context.Background(), shapes, WriterShapeKey, 1)
	if err != nil {
		t.Fatalf("EnsureGraphWriterShape error = %v, want nil", err)
	}
	if retired {
		t.Fatalf("retired = true, want false (claim lost)")
	}
	if store.refinalizeFilter.AllScopes {
		t.Fatalf("refinalize ran on a lost claim")
	}
	if len(shapes.marked) != 0 {
		t.Fatalf("marked = %v, want empty (loser advances nothing)", shapes.marked)
	}
}

// TestEnsureGraphWriterShapeRejectsNonPositiveVersion proves a zero or
// negative code version fails closed instead of refinalizing the world.
func TestEnsureGraphWriterShapeRejectsNonPositiveVersion(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{}
	handler := mustNewHandler(t, store)
	shapes := &fakeWriterShapeStore{applied: 0, claimWon: true}

	if _, err := handler.EnsureGraphWriterShape(context.Background(), shapes, WriterShapeKey, 0); err == nil {
		t.Fatalf("version 0 error = nil, want non-nil")
	}
	if store.refinalizeFilter.AllScopes {
		t.Fatalf("refinalize ran on an invalid version")
	}
}

// TestEnsureGraphWriterShapeFailedRefinalizeReleasesClaim proves a failed
// upgrade refinalize releases its claim without advancing the marker, so
// the next startup retries instead of waiting out the lease.
func TestEnsureGraphWriterShapeFailedRefinalizeReleasesClaim(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("refinalize store boom")
	store := &fakeReplayStore{refinalizeErr: storeErr}
	handler := mustNewHandler(t, store)
	shapes := &fakeWriterShapeStore{applied: 0, claimWon: true}

	retired, err := handler.EnsureGraphWriterShape(context.Background(), shapes, WriterShapeKey, 1)
	if err == nil {
		t.Fatalf("error = nil, want the store error")
	}
	if retired {
		t.Fatalf("retired = true on a failed refinalize")
	}
	if len(shapes.marked) != 0 {
		t.Fatalf("marked = %v, want empty (failed refinalize advances nothing)", shapes.marked)
	}
	if shapes.released != 1 {
		t.Fatalf("released = %d, want 1 (claim released for retry)", shapes.released)
	}
}
