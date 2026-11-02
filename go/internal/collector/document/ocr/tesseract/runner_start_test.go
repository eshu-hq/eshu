// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// TestRunClassifiesStartErrorFromEndedCallerContextAsCanceled drives run with
// a context that ended after Recognize's pre-exec check but before cmd.Start.
// exec.Cmd.Start returns the context error in that window; the caller giving
// up must count as canceled, not as an engine failure.
func TestRunClassifiesStartErrorFromEndedCallerContextAsCanceled(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	run, err := engine.run(ctx, fakePNG(t, 20, 10, "ok"))
	assertOutcome(t, err, OutcomeCanceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) = false: %v", err)
	}
	if run.started {
		t.Fatal("run reports started for a process that never started")
	}
}

// TestRunClassifiesStartErrorFromPassedCallerDeadlineAsCanceled keeps the
// running-case rule: an ended caller context wins, whatever its cause.
func TestRunClassifiesStartErrorFromPassedCallerDeadlineAsCanceled(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := engine.run(ctx, fakePNG(t, 20, 10, "ok"))
	assertOutcome(t, err, OutcomeCanceled)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(err, context.DeadlineExceeded) = false: %v", err)
	}
}

// TestRunKeepsRealStartFailureAsExecFailed proves a start error with a live
// caller context is still an engine failure.
func TestRunKeepsRealStartFailureAsExecFailed(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)
	engine.cfg.BinaryPath = filepath.Join(t.TempDir(), "missing-tesseract")

	_, err := engine.run(context.Background(), fakePNG(t, 20, 10, "ok"))
	assertOutcome(t, err, OutcomeExecFailed)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("real start failure wraps a context error: %v", err)
	}
}

// TestRunClassifiesStartErrorFromPassedRunDeadlineAsTimeout covers the run
// deadline passing before Start while the caller context is still live.
func TestRunClassifiesStartErrorFromPassedRunDeadlineAsTimeout(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)
	engine.cfg.Timeout = time.Nanosecond

	_, err := engine.run(context.Background(), fakePNG(t, 20, 10, "ok"))
	assertOutcome(t, err, OutcomeTimeout)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(err, context.DeadlineExceeded) = false: %v", err)
	}
}
