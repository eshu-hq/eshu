// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadline

import (
	"context"
	"errors"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestRecoveredLineCarriesTheBackoffItWaited proves the INFO "recovered on
// attempt N" line reports the backoff the failed attempt waited, not zero: an
// operator reads it to see what the recovery cost.
func TestRecoveredLineCarriesTheBackoffItWaited(t *testing.T) {
	h := newRetryHarness(t, fastLimits)
	run := &scripted{outcome: []string{dnsStderr, "ok"}}
	command := h.command(run, Fetch)
	command.Jitter = func(base time.Duration) time.Duration { return base }

	limits := fastLimits
	limits.RetryBackoff = 3 * time.Second
	if err := RunRetrying(h.ctx, limits, command); err != nil {
		t.Fatalf("RunRetrying() error = %v, want recovery on attempt 2", err)
	}
	recovered := h.recordsWithMessage(t, "git command recovered on attempt 2")
	if len(recovered) != 1 {
		t.Fatalf("recovery lines = %v, want one", recovered)
	}
	if got := recovered[0]["backoff_seconds"]; got != float64(3) {
		t.Fatalf("recovered line backoff_seconds = %v, want 3: the wait before the successful attempt", got)
	}
}

// neverRefresh is a RefreshRequester whose lookup never finishes.
type neverRefresh struct{ wait chan struct{} }

func (n neverRefresh) RequestRefresh(string) <-chan struct{} { return n.wait }

// TestShutdownDuringTheRefreshWaitStartsNoRetry proves a parent cancel that
// lands while the retry waits for the address refresh ends the loop on the
// shutdown path: no retry is counted, no span event is written, the command
// does not run again, and the error is not a command failure.
func TestShutdownDuringTheRefreshWaitStartsNoRetry(t *testing.T) {
	h := newRetryHarness(t, fastLimits)
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := provider.Tracer("deadline-test").Start(h.ctx, "sync")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx = WithRefreshRequester(ctx, neverRefresh{wait: make(chan struct{})})

	run := &scripted{outcome: []string{dnsStderr, "ok"}}
	command := h.command(run, Fetch)
	// The backoff returns at once and the shutdown lands as it ends, so the
	// loop reaches the refresh wait with a context that is already done.
	command.Sleep = func(context.Context, time.Duration) error { cancel(); return nil }

	err := RunRetrying(ctx, fastLimits, command)
	span.End()

	var failure *Failure
	if err == nil || errors.As(err, &failure) {
		t.Fatalf("RunRetrying() error = %v, want the shutdown path, not a *Failure", err)
	}
	if run.callCount() != 1 {
		t.Fatalf("attempts = %d, want 1: a cancelled retry must not run the command again", run.callCount())
	}
	if got := h.series(t, retriesMetric); len(got) != 0 {
		t.Fatalf("%s = %v, want none: the retry never started", retriesMetric, got)
	}
	for _, ended := range recorder.Ended() {
		for _, event := range ended.Events() {
			if event.Name == retryEventName {
				t.Fatalf("span event %q written for a retry that never started", retryEventName)
			}
		}
	}
}
