// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// fakeCrossplaneRedriveBatchSweeper implements crossplaneRedriveBatchSweeper
// without a real Postgres, so the catch-up loop/tick's own control flow
// (call it, swallow its error, return promptly on cancellation) can be
// proven as a fast unit test (issue #5476 P2-a).
type fakeCrossplaneRedriveBatchSweeper struct {
	calls   int
	results []postgres.CrossplaneRedriveSweepResult
	err     error
}

func (f *fakeCrossplaneRedriveBatchSweeper) SweepBatch(context.Context, int) ([]postgres.CrossplaneRedriveSweepResult, error) {
	f.calls++
	return f.results, f.err
}

func TestRunCrossplaneRedriveCatchUpTickCallsSweepBatch(t *testing.T) {
	fake := &fakeCrossplaneRedriveBatchSweeper{
		results: []postgres.CrossplaneRedriveSweepResult{{Attempted: true, TargetsEnqueued: 2}},
	}
	runCrossplaneRedriveCatchUpTick(context.Background(), fake, slog.Default())
	if fake.calls != 1 {
		t.Fatalf("expected SweepBatch to be called exactly once, got %d", fake.calls)
	}
}

func TestRunCrossplaneRedriveCatchUpTickSwallowsSweepBatchError(t *testing.T) {
	fake := &fakeCrossplaneRedriveBatchSweeper{err: errors.New("injected batch failure")}

	// Must not panic and must not require a non-nil logger; the whole point
	// is that a failed catch-up pass is logged and the caller (the ticker
	// loop) simply continues to the next tick, never propagating the error.
	runCrossplaneRedriveCatchUpTick(context.Background(), fake, slog.Default())
	if fake.calls != 1 {
		t.Fatalf("expected SweepBatch to be called exactly once despite the error, got %d", fake.calls)
	}

	// A nil logger must also be safe (defensive: no known production caller
	// passes nil, but the tick must not assume a non-nil logger).
	fakeNilLogger := &fakeCrossplaneRedriveBatchSweeper{err: errors.New("injected batch failure")}
	runCrossplaneRedriveCatchUpTick(context.Background(), fakeNilLogger, nil)
	if fakeNilLogger.calls != 1 {
		t.Fatalf("expected SweepBatch to be called exactly once with a nil logger, got %d", fakeNilLogger.calls)
	}
}

// TestRunCrossplaneRedriveCatchUpLoopReturnsPromptlyOnContextCancellation
// proves the loop's ctx.Done() case wins immediately over the (2-minute)
// ticker interval -- an already-canceled context must make the loop return
// well within this test's bounded timeout, having never called SweepBatch.
func TestRunCrossplaneRedriveCatchUpLoopReturnsPromptlyOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fake := &fakeCrossplaneRedriveBatchSweeper{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runCrossplaneRedriveCatchUpLoop(ctx, fake, slog.Default())
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected runCrossplaneRedriveCatchUpLoop to return promptly on an already-canceled context")
	}
	if fake.calls != 0 {
		t.Fatalf("expected SweepBatch to never be called when ctx is canceled before the first tick, got %d calls", fake.calls)
	}
}
