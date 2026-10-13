// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadline

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"slices"
	"testing"
	"time"
)

// TestRunRetryingRecoversOnSecondAttempt proves the loop: a resolver failure is
// classified, the loop backs off once for the default 3 s, and the second
// attempt succeeds.
func TestRunRetryingRecoversOnSecondAttempt(t *testing.T) {
	h := newRetryHarness(t, fastLimits)
	run := &scripted{outcome: []string{dnsStderr, "ok"}}

	if err := RunRetrying(h.ctx, fastLimits, h.command(run, Fetch)); err != nil {
		t.Fatalf("RunRetrying() error = %v, want nil after the second attempt", err)
	}
	if run.callCount() != 2 {
		t.Fatalf("attempts = %d, want 2", run.callCount())
	}
	if got := h.sleeps(); len(got) != 1 || got[0] != DefaultRetryBackoff {
		t.Fatalf("backoff sleeps = %v, want one of %s", got, DefaultRetryBackoff)
	}
}

// TestRunRetryingNeverRetriesNonRetryable proves auth and the other closed
// non-retryable classes run exactly once and cost no backoff.
func TestRunRetryingNeverRetriesNonRetryable(t *testing.T) {
	h := newRetryHarness(t, fastLimits)
	run := &scripted{outcome: []string{authStderr, "ok"}}

	err := RunRetrying(h.ctx, fastLimits, h.command(run, Fetch))
	var failure *Failure
	if !errors.As(err, &failure) || failure.Class != ClassAuth || failure.Attempts != 1 || failure.Retryable {
		t.Fatalf("RunRetrying() error = %v (%+v), want an auth_failed Failure after 1 attempt", err, failure)
	}
	if run.callCount() != 1 || len(h.sleeps()) != 0 {
		t.Fatalf("attempts = %d, sleeps = %v, want 1 attempt and no sleep", run.callCount(), h.sleeps())
	}
	if got := h.series(t, retriesMetric); len(got) != 0 {
		t.Fatalf("%s = %v, want none", retriesMetric, got)
	}
}

// TestRunRetryingExhaustsAttemptsWithTheSameClass proves the attempts bound of
// one operation: the retry fails with the same class, the error says how many
// attempts ran, and the error text is the process error unchanged.
func TestRunRetryingExhaustsAttemptsWithTheSameClass(t *testing.T) {
	h := newRetryHarness(t, fastLimits)
	run := &scripted{outcome: []string{dnsStderr}}

	err := RunRetrying(h.ctx, fastLimits, h.command(run, Resolve))
	var failure *Failure
	if !errors.As(err, &failure) || failure.Class != ClassDNSResolve || failure.Attempts != 2 || !failure.Retryable {
		t.Fatalf("RunRetrying() error = %v (%+v), want a dns_resolve_failed Failure after 2 attempts", err, failure)
	}
	if err.Error() != "exit status 128" {
		t.Fatalf("error text = %q, want the process error unchanged", err)
	}
	if run.callCount() != 2 {
		t.Fatalf("attempts = %d, want 2", run.callCount())
	}
}

// TestRunRetryingAttemptsOneTurnsRetryOff proves ESHU_GIT_RETRY_ATTEMPTS=1 is
// today's behaviour: one attempt, no backoff, no retry counter.
func TestRunRetryingAttemptsOneTurnsRetryOff(t *testing.T) {
	limits := fastLimits
	limits.RetryAttempts = 1
	h := newRetryHarness(t, limits)
	run := &scripted{outcome: []string{dnsStderr, "ok"}}

	if err := RunRetrying(h.ctx, limits, h.command(run, Fetch)); err == nil {
		t.Fatal("RunRetrying() = nil, want the first failure")
	}
	if run.callCount() != 1 || len(h.sleeps()) != 0 {
		t.Fatalf("attempts = %d, sleeps = %v, want one attempt and no sleep", run.callCount(), h.sleeps())
	}
}

// TestRunRetryingHungUpNeedsATransportLine proves the "remote end hung up"
// line alone is not retried and a paired one is.
func TestRunRetryingHungUpNeedsATransportLine(t *testing.T) {
	h := newRetryHarness(t, fastLimits)
	alone := &scripted{outcome: []string{"fatal: the remote end hung up unexpectedly", "ok"}}
	if err := RunRetrying(h.ctx, fastLimits, h.command(alone, Fetch)); err == nil || alone.callCount() != 1 {
		t.Fatalf("hung up alone: err = %v, attempts = %d, want a failure after 1 attempt", err, alone.callCount())
	}
	paired := &scripted{outcome: []string{hungUp + " fatal: the remote end hung up unexpectedly", "ok"}}
	if err := RunRetrying(h.ctx, fastLimits, h.command(paired, Fetch)); err != nil || paired.callCount() != 2 {
		t.Fatalf("hung up paired: err = %v, attempts = %d, want recovery on attempt 2", err, paired.callCount())
	}
}

// TestRunRetryingDeadlineKillIsRetried proves a real deadline kill is one
// attempt of class deadline_exceeded with a fresh deadline for the next.
func TestRunRetryingDeadlineKillIsRetried(t *testing.T) {
	limits := Limits{Fetch: 150 * time.Millisecond}
	h := newRetryHarness(t, limits)
	var calls int
	var stderr bytes.Buffer
	command := h.command(&scripted{outcome: []string{"ok"}}, Fetch)
	command.Stderr = func() string { return stderr.String() }
	command.Build = func(ctx context.Context) *exec.Cmd {
		calls++
		if calls == 1 {
			return exec.CommandContext(ctx, "sleep", "5")
		}
		return exec.CommandContext(ctx, "sh", "-c", "exit 0")
	}
	if err := RunRetrying(h.ctx, limits, command); err != nil || calls != 2 {
		t.Fatalf("RunRetrying() = %v after %d attempts, want recovery on attempt 2", err, calls)
	}
	wantSeries(t, h.series(t, attemptsMetric), attemptsMetric, map[string]int64{
		"failure_class=deadline_exceeded,host=github.com,operation=fetch,outcome=retried": 1,
		"failure_class=none,host=github.com,operation=fetch,outcome=success":              1,
	})
	// The kill itself is still counted once, by the existing counter.
	if got := operationCounts(t, h.reader); got[Fetch] != 1 {
		t.Fatalf("%s = %v, want {fetch:1}", counterName, got)
	}
}

// TestRunRetryingBeforeRetryRunsBetweenAttempts proves the cleanup hook runs
// once, between the failed attempt and the next one, and that a failed cleanup
// ends the retry with the original failure.
func TestRunRetryingBeforeRetryRunsBetweenAttempts(t *testing.T) {
	h := newRetryHarness(t, fastLimits)
	run := &scripted{outcome: []string{dnsStderr, "ok"}}
	var order []string
	command := h.command(run, Clone)
	inner := command.Build
	command.Build = func(ctx context.Context) *exec.Cmd {
		order = append(order, "attempt")
		return inner(ctx)
	}
	command.BeforeRetry = func() error { order = append(order, "cleanup"); return nil }
	if err := RunRetrying(h.ctx, fastLimits, command); err != nil {
		t.Fatalf("RunRetrying() error = %v", err)
	}
	if want := []string{"attempt", "cleanup", "attempt"}; !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}

	run = &scripted{outcome: []string{dnsStderr, "ok"}}
	command = h.command(run, Clone)
	command.BeforeRetry = func() error { return errors.New("cannot remove partial clone") }
	err := RunRetrying(h.ctx, fastLimits, command)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Class != ClassDNSResolve || failure.Attempts != 1 || run.callCount() != 1 {
		t.Fatalf("failed cleanup: err = %v, attempts = %d, want the original failure after 1 attempt", err, run.callCount())
	}
}

// TestRunRetryingShutdownDuringBackoffStops proves a parent cancel during the
// backoff ends the loop with no further attempt and no retry counted.
func TestRunRetryingShutdownDuringBackoffStops(t *testing.T) {
	h := newRetryHarness(t, fastLimits)
	ctx, cancel := context.WithCancel(h.ctx)
	run := &scripted{outcome: []string{dnsStderr, "ok"}}
	command := h.command(run, Fetch)
	command.Sleep = func(c context.Context, _ time.Duration) error { cancel(); return c.Err() }

	err := RunRetrying(ctx, fastLimits, command)
	if err == nil || run.callCount() != 1 {
		t.Fatalf("RunRetrying() = %v after %d attempts, want a failure after 1", err, run.callCount())
	}
	if got := h.series(t, retriesMetric); len(got) != 0 {
		t.Fatalf("%s = %v after a shutdown, want none", retriesMetric, got)
	}
}
