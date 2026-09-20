// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recovery

import (
	"context"
	"errors"
	"testing"
	"time"
)

// scriptedWriterShapeStore flips the reported applied version after a fixed
// number of reads, modeling a peer winner marking the upgrade applied while
// this runner loses every claim.
type scriptedWriterShapeStore struct {
	reads     int
	flipAfter int
	claims    int
}

func (f *scriptedWriterShapeStore) AppliedVersion(_ context.Context, _ string) (int, error) {
	f.reads++
	if f.reads > f.flipAfter {
		return 1, nil
	}
	return 0, nil
}

func (f *scriptedWriterShapeStore) ClaimVersion(_ context.Context, _ string, _ int, _ time.Duration) (bool, error) {
	f.claims++
	return false, nil
}

func (f *scriptedWriterShapeStore) MarkAppliedVersion(_ context.Context, _ string, _ int) error {
	return errors.New("scripted store never marks")
}

func (f *scriptedWriterShapeStore) ReleaseClaim(_ context.Context, _ string) error { return nil }

func testUpgradeRunner(t *testing.T, handler *Handler, shapes WriterShapeStore) *WriterShapeUpgradeRunner {
	t.Helper()
	return &WriterShapeUpgradeRunner{
		Handler:        handler,
		Shapes:         shapes,
		Key:            WriterShapeKey,
		CurrentVersion: 1,
		PollInterval:   time.Millisecond,
		Wait: func(ctx context.Context, _ time.Duration) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return nil
			}
		},
	}
}

// TestWriterShapeUpgradeRunnerSkipsWhenApplied proves the common path stays
// quiet: a binary already at the code's writer shape performs no claim and
// no refinalize, then the runner exits.
func TestWriterShapeUpgradeRunnerSkipsWhenApplied(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{}
	handler := mustNewHandler(t, store)
	shapes := &fakeWriterShapeStore{applied: 1, claimWon: true}
	runner := testUpgradeRunner(t, handler, shapes)

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if store.refinalizeFilter.AllScopes {
		t.Fatalf("refinalize ran on a current marker")
	}
	if shapes.claims != 0 {
		t.Fatalf("claims = %d, want 0 (no claim attempt on a current marker)", shapes.claims)
	}
}

// markingWriterShapeStore advances the reported applied version on mark,
// modeling the real marker table. The shared fake records marks without
// moving applied, which would leave a polling runner looping forever.
type markingWriterShapeStore struct {
	*fakeWriterShapeStore
}

func (f *markingWriterShapeStore) MarkAppliedVersion(ctx context.Context, key string, version int) error {
	if err := f.fakeWriterShapeStore.MarkAppliedVersion(ctx, key, version); err != nil {
		return err
	}
	f.applied = version
	return nil
}

// TestWriterShapeUpgradeRunnerRetiresOnceOnUpgrade proves the upgrade path:
// a stale marker plus a won claim runs one full all-scopes refinalize,
// advances the marker, and exits the runner so it never refinalizes twice.
func TestWriterShapeUpgradeRunnerRetiresOnceOnUpgrade(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{}
	handler := mustNewHandler(t, store)
	shapes := &markingWriterShapeStore{fakeWriterShapeStore: &fakeWriterShapeStore{applied: 0, claimWon: true}}
	runner := testUpgradeRunner(t, handler, shapes)

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if !store.refinalizeFilter.AllScopes {
		t.Fatalf("refinalize filter = %+v, want AllScopes", store.refinalizeFilter)
	}
	if len(shapes.marked) != 1 || shapes.marked[0] != 1 {
		t.Fatalf("marked = %v, want [1] (marker advances exactly once)", shapes.marked)
	}
}

// TestWriterShapeUpgradeRunnerLostClaimWaitsForWinner proves exactly-once
// semantics across concurrently starting binaries from the loser's side:
// the loser performs no refinalize and keeps polling until the winner's
// marker appears, then exits.
func TestWriterShapeUpgradeRunnerLostClaimWaitsForWinner(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{}
	handler := mustNewHandler(t, store)
	shapes := &scriptedWriterShapeStore{flipAfter: 2}
	runner := testUpgradeRunner(t, handler, shapes)

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if store.refinalizeFilter.AllScopes {
		t.Fatalf("refinalize ran on a lost claim")
	}
	if shapes.claims == 0 {
		t.Fatalf("claims = 0, want attempts while the marker was stale")
	}
}

// TestWriterShapeUpgradeRunnerRefinalizeErrorRetries proves a failed
// refinalize releases the claim and retries on a later tick instead of
// wedging the runner or the boot: the second attempt succeeds and marks.
func TestWriterShapeUpgradeRunnerRefinalizeErrorRetries(t *testing.T) {
	t.Parallel()

	inner := &fakeReplayStore{}
	store := &failOnceRefinalizeStore{ReplayStore: inner, err: errors.New("boom")}
	handler := mustNewHandler(t, store)
	innerShapes := &fakeWriterShapeStore{applied: 0, claimWon: true}
	shapes := &markingWriterShapeStore{fakeWriterShapeStore: innerShapes}
	runner := testUpgradeRunner(t, handler, shapes)

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if store.calls != 2 {
		t.Fatalf("refinalize calls = %d, want 2 (fail once, succeed on retry)", store.calls)
	}
	if len(innerShapes.marked) != 1 || innerShapes.marked[0] != 1 {
		t.Fatalf("marked = %v, want [1] after retry", innerShapes.marked)
	}
	if innerShapes.released == 0 {
		t.Fatalf("released = 0, want the failed attempt to release its claim")
	}
}

// failOnceRefinalizeStore fails the first refinalize call, then delegates,
// so the runner exercises the release-and-retry path.
type failOnceRefinalizeStore struct {
	ReplayStore
	calls int
	err   error
}

func (f *failOnceRefinalizeStore) RefinalizeScopeProjections(
	ctx context.Context,
	filter RefinalizeFilter,
	now time.Time,
) (RefinalizeResult, error) {
	f.calls++
	if f.calls == 1 {
		return RefinalizeResult{}, f.err
	}
	return f.ReplayStore.RefinalizeScopeProjections(ctx, filter, now)
}

// TestWriterShapeUpgradeRunnerContextCancelExits proves the runner never
// outlives the service: a cancelled context stops the poll loop promptly.
func TestWriterShapeUpgradeRunnerContextCancelExits(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{}
	handler := mustNewHandler(t, store)
	shapes := &scriptedWriterShapeStore{flipAfter: 1 << 30}
	runner := testUpgradeRunner(t, handler, shapes)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil on cancelled context", err)
	}
}

// TestWriterShapeUpgradeRunnerRejectsZeroValue proves the runner fails
// closed on a missing handler, store, key, or version instead of polling
// a no-op forever.
func TestWriterShapeUpgradeRunnerRejectsZeroValue(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{}
	handler := mustNewHandler(t, store)
	shapes := &fakeWriterShapeStore{applied: 0, claimWon: true}

	for name, mutate := range map[string]func(*WriterShapeUpgradeRunner){
		"nil handler": func(r *WriterShapeUpgradeRunner) { r.Handler = nil },
		"nil store":   func(r *WriterShapeUpgradeRunner) { r.Shapes = nil },
		"empty key":   func(r *WriterShapeUpgradeRunner) { r.Key = "" },
		"zero rev":    func(r *WriterShapeUpgradeRunner) { r.CurrentVersion = 0 },
	} {
		runner := testUpgradeRunner(t, handler, shapes)
		mutate(runner)
		if err := runner.Run(context.Background()); err == nil {
			t.Fatalf("%s: Run() = nil, want a configuration error", name)
		}
	}
}
