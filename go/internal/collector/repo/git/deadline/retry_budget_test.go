// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadline

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
)

// TestRunRetryingSweepBudgetFallsThrough is the per-sweep cap: once the sweep
// has spent its retries, a failure is final on the first attempt, exactly as
// before the retry existed, and the exhaustion is counted and logged.
func TestRunRetryingSweepBudgetFallsThrough(t *testing.T) {
	limits := fastLimits
	limits.RetrySweepBudget = 1
	h := newRetryHarness(t, limits)

	first := &scripted{outcome: []string{dnsStderr, "ok"}}
	if err := RunRetrying(h.ctx, limits, h.command(first, Fetch)); err != nil {
		t.Fatalf("first command error = %v, want recovery on the one budgeted retry", err)
	}
	for i := 0; i < 2; i++ {
		later := &scripted{outcome: []string{dnsStderr, "ok"}}
		err := RunRetrying(h.ctx, limits, h.command(later, Fetch))
		var failure *Failure
		if !errors.As(err, &failure) || failure.Class != ClassDNSResolve || failure.Attempts != 1 {
			t.Fatalf("later command %d error = %v (%+v), want today's single-attempt failure", i, err, failure)
		}
		if later.callCount() != 1 {
			t.Fatalf("later command %d ran %d attempts, want 1 once the budget is spent", i, later.callCount())
		}
	}
	wantSeries(t, h.series(t, exhaustedMetric), exhaustedMetric, map[string]int64{"scope=sweep": 2})
	wantSeries(t, h.series(t, retriesMetric), retriesMetric, map[string]int64{
		"failure_class=dns_resolve_failed,host=github.com,operation=fetch": 1,
	})
	var exhausted int
	for _, rec := range h.recordsWithMessage(t, "git command attempt failed") {
		if rec["retry_budget_exhausted"] == true {
			exhausted++
			if rec["will_retry"] != false {
				t.Errorf("exhausted WARN will_retry = %v, want false", rec["will_retry"])
			}
		}
	}
	if exhausted != 2 {
		t.Fatalf("WARN lines with retry_budget_exhausted = %d, want 2:\n%s", exhausted, h.logs)
	}
}

// TestSweepBudgetHoldsUnderConcurrentOperations is the pool-safety proof: many
// goroutines run commands through RunRetrying on one shared sweep budget. Every
// command fails once and would recover if retried, so exactly the budget's
// worth recover, the rest fail on their first attempt, and the retries started
// never exceed the cap. Run under -race this also covers the budget counter and
// the shared signals.
func TestSweepBudgetHoldsUnderConcurrentOperations(t *testing.T) {
	const workers, budget = 64, 7
	limits := fastLimits
	limits.RetrySweepBudget = budget
	h := newRetryHarness(t, limits)

	var recovered, failedFirst, attempts atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run := &scripted{outcome: []string{dnsStderr, "ok"}}
			<-start
			err := RunRetrying(h.ctx, limits, h.command(run, Fetch))
			attempts.Add(int64(run.callCount()))
			var failure *Failure
			switch {
			case err == nil:
				recovered.Add(1)
			case errors.As(err, &failure) && failure.Attempts == 1:
				failedFirst.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if recovered.Load() != budget || failedFirst.Load() != workers-budget {
		t.Fatalf("recovered = %d, failed on first attempt = %d; want %d and %d", recovered.Load(), failedFirst.Load(), budget, workers-budget)
	}
	if got := attempts.Load(); got != workers+budget {
		t.Fatalf("attempts across workers = %d, want %d: retries started must equal the budget, never more", got, workers+budget)
	}
	wantSeries(t, h.series(t, exhaustedMetric), exhaustedMetric, map[string]int64{"scope=sweep": workers - budget})
	wantSeries(t, h.series(t, retriesMetric), retriesMetric, map[string]int64{
		"failure_class=dns_resolve_failed,host=github.com,operation=fetch": budget,
	})
	if got := len(h.sleeps()); got != budget {
		t.Fatalf("backoff sleeps = %d, want %d: the wait happens inside each retrying operation", got, budget)
	}
}

// TestRunRetryingRefundsTheBudgetWhenCleanupFails proves a retry that was
// drawn but never started (BeforeRetry failed) gives its budget back: with a
// budget of one, a failed cleanup must not strand the next command without a
// retry.
func TestRunRetryingRefundsTheBudgetWhenCleanupFails(t *testing.T) {
	limits := fastLimits
	limits.RetrySweepBudget = 1
	h := newRetryHarness(t, limits)

	broken := &scripted{outcome: []string{dnsStderr, "ok"}}
	command := h.command(broken, Clone)
	command.BeforeRetry = func() error { return errors.New("cannot remove partial clone") }
	if err := RunRetrying(h.ctx, limits, command); err == nil || broken.callCount() != 1 {
		t.Fatalf("failed cleanup: err = %v, attempts = %d, want the original failure after 1 attempt", err, broken.callCount())
	}

	next := &scripted{outcome: []string{dnsStderr, "ok"}}
	if err := RunRetrying(h.ctx, limits, h.command(next, Fetch)); err != nil || next.callCount() != 2 {
		t.Fatalf("next command: err = %v, attempts = %d; want its retry, the refunded budget", err, next.callCount())
	}
	if got := h.series(t, exhaustedMetric); len(got) != 0 {
		t.Fatalf("%s = %v, want none: the budget was refunded", exhaustedMetric, got)
	}
}

// TestSweepTakeNeverExceedsTheBudget races many goroutines for the last retry
// of a budget, round after round: exactly one wins each round, never two. A
// check-then-decrement counter overdraws here; the compare-and-swap loop does
// not. A refund returns exactly one.
func TestSweepTakeNeverExceedsTheBudget(t *testing.T) {
	const rounds, workers = 3000, 8
	for round := 0; round < rounds; round++ {
		sweep := &sweepState{}
		sweep.remaining.Store(1)
		var won atomic.Int64
		var ready, done sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < workers; i++ {
			ready.Add(1)
			done.Add(1)
			go func() {
				defer done.Done()
				ready.Done()
				<-start
				if sweep.take() {
					won.Add(1)
				}
			}()
		}
		ready.Wait()
		close(start)
		done.Wait()
		if won.Load() != 1 || sweep.remaining.Load() != 0 {
			t.Fatalf("round %d: draws won = %d, remaining = %d; want 1 and 0", round, won.Load(), sweep.remaining.Load())
		}
	}
	sweep := &sweepState{}
	sweep.refund()
	if !sweep.take() || sweep.take() {
		t.Fatal("after one refund exactly one draw must succeed")
	}
}

// TestRunRetryingWithoutASweepIsUncapped proves a call outside a sweep still
// retries by attempts alone; only the sweep caps the fleet total.
func TestRunRetryingWithoutASweepIsUncapped(t *testing.T) {
	run := &scripted{outcome: []string{dnsStderr, "ok"}}
	h := newRetryHarness(t, fastLimits)
	if err := RunRetrying(context.Background(), fastLimits, h.command(run, Fetch)); err != nil {
		t.Fatalf("RunRetrying() error = %v, want nil", err)
	}
	if run.callCount() != 2 {
		t.Fatalf("attempts = %d, want 2", run.callCount())
	}
}

type recordingRefresh struct {
	mu    sync.Mutex
	hosts []string
	wait  chan struct{}
}

func (r *recordingRefresh) RequestRefresh(host string) <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hosts = append(r.hosts, host)
	return r.wait
}

// TestRunRetryingRequestsAddressRefreshOnResolveOrConnect proves the hook for
// the later per-host address cache: resolve and connect classes ask for a
// refresh of the host, other classes do not, and the request never blocks.
func TestRunRetryingRequestsAddressRefreshOnResolveOrConnect(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   []string
	}{
		{"resolve", dnsStderr, []string{"github.com"}},
		{"connect", "fatal: unable to access 'https://github.com/o/r.git/': Failed to connect to github.com port 443", []string{"github.com"}},
		{"reset", hungUp, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newRetryHarness(t, fastLimits)
			wait := make(chan struct{})
			close(wait)
			refresh := &recordingRefresh{wait: wait}
			ctx := WithRefreshRequester(h.ctx, refresh)
			run := &scripted{outcome: []string{tc.stderr, "ok"}}
			if err := RunRetrying(ctx, fastLimits, h.command(run, Fetch)); err != nil {
				t.Fatalf("RunRetrying() error = %v", err)
			}
			if !slices.Equal(refresh.hosts, tc.want) {
				t.Fatalf("refresh requests = %v, want %v", refresh.hosts, tc.want)
			}
		})
	}
}
