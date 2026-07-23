// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
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

// fakeServiceRunnerFunc adapts a plain func to serviceRunner for tests.
type fakeServiceRunnerFunc func(context.Context) error

func (f fakeServiceRunnerFunc) Run(ctx context.Context) error { return f(ctx) }

// TestRunServiceAndJoinRedriveReturnsPromptlyOnFatalServiceErrorWithoutCancelingCtx
// is the issue #5476 P0 regression: a fix that joined the redrive goroutine
// via a deferred WaitGroup.Wait() registered after a deferred stop() relied
// on LIFO ordering to guarantee stop() ran before Wait(). That only holds
// when ctx is ALREADY canceled by the time service.Run returns (the
// OS-signal shutdown path). It deadlocks the whole process on the OTHER
// real return path: service.Run returning a non-nil error on a fatal
// Ack/Claim failure WITHOUT itself canceling ctx. This test reproduces
// exactly that shape -- a fake Runner whose Run returns a non-nil error
// without touching ctx, plus a background goroutine that (like the real
// catch-up loop) only exits on ctx.Done() -- and asserts
// runServiceAndJoinRedrive still returns within a bounded time, proving the
// fix (stop() called unconditionally, before the join) actually closes the
// hang rather than just moving it.
func TestRunServiceAndJoinRedriveReturnsPromptlyOnFatalServiceErrorWithoutCancelingCtx(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())

	// Mirrors runCrossplaneRedriveCatchUpLoop's own shape: the only way this
	// goroutine exits is ctx.Done() firing.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
	}()

	fatalErr := errors.New("fatal claim/ack failure")
	// Run returns a non-nil error and deliberately does NOT cancel ctx --
	// reproducing projector.Service's real runConcurrent, which cancels only
	// its own locally-derived child context on a fatal error, never the
	// caller's ctx.
	fakeService := fakeServiceRunnerFunc(func(context.Context) error { return fatalErr })

	resultCh := make(chan error, 1)
	go func() {
		resultCh <- runServiceAndJoinRedrive(ctx, fakeService, stop, &wg)
	}()

	select {
	case err := <-resultCh:
		if !errors.Is(err, fatalErr) {
			t.Fatalf("expected the fatal error to be returned verbatim, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runServiceAndJoinRedrive hung: the fatal-error path must cancel ctx before joining the redrive goroutine (issue #5476 P0 regression)")
	}

	select {
	case <-ctx.Done():
	default:
		t.Fatal("expected ctx to be canceled by runServiceAndJoinRedrive's unconditional stop() call")
	}
}

// TestRunServiceAndJoinRedriveReturnsPromptlyOnNormalSignalShutdown proves
// the SAME function still behaves correctly on the pre-existing normal
// path: ctx already canceled (as signal.NotifyContext's relay would do) by
// the time Run observes it and returns. stop() is idempotent, so calling it
// again here is a safe no-op, and the join must still complete promptly.
func TestRunServiceAndJoinRedriveReturnsPromptlyOnNormalSignalShutdown(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
	}()

	// A real service under signal-driven shutdown observes ctx.Done() itself
	// and returns nil; simulate the "signal already fired" ordering by
	// canceling ctx from a separate goroutine while Run blocks on it.
	fakeService := fakeServiceRunnerFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})
	go stop()

	resultCh := make(chan error, 1)
	go func() {
		resultCh <- runServiceAndJoinRedrive(ctx, fakeService, stop, &wg)
	}()

	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatalf("expected a nil error on the normal signal-shutdown path, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected runServiceAndJoinRedrive to return promptly on the normal signal-shutdown path")
	}

	select {
	case <-ctx.Done():
	default:
		t.Fatal("expected ctx to remain canceled after the normal signal-shutdown path")
	}
}
