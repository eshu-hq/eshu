// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadline

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"os/exec"
	"sync/atomic"
	"time"
)

// DefaultRefreshWait is the longest a retry waits for a RefreshRequester's
// lookup after a resolve or connect failure. The later per-host address cache
// owns the real lookup deadline; this bound keeps the hook from stretching a
// retry if it ever returns a channel that never closes.
const DefaultRefreshWait = 5 * time.Second

// Command is one network git command the retry loop may run more than once.
type Command struct {
	// Operation is the closed deadline operation (Operations).
	Operation string
	// Host is the bounded provider host for metric labels and logs. It never
	// carries a path, credential, or full URL.
	Host string
	// Scope is the repository identity, for logs and the span event only.
	Scope string
	// Build makes a fresh process-group command from the attempt context. It
	// runs once per attempt, so it must reset any output buffer the caller
	// reads between attempts.
	Build func(context.Context) *exec.Cmd
	// Stderr returns the git error text of the latest attempt, with newlines
	// kept and credentials already redacted. Nil means no text.
	Stderr func() string
	// BeforeRetry, when set, runs between a failed attempt and its retry (a
	// clone removes its partial directory here). An error from it ends the
	// retry and the original failure is final.
	BeforeRetry func() error
	// Sleep waits out a backoff and returns the context error if the context
	// ends first. Nil means a context-aware timer. A test injects its clock
	// here, so no package-level state is shared between operations.
	Sleep func(ctx context.Context, d time.Duration) error
	// Jitter spreads the base backoff. Nil means jitterBackoff (75% to 125%).
	Jitter func(base time.Duration) time.Duration
}

// Failure is the final failure of a command run through RunRetrying. Its text
// is the underlying error's text, unchanged, and it unwraps to it, so a
// *Error deadline kill inside it is still found with errors.As.
type Failure struct {
	// Class is one of the Class constants.
	Class string
	// Attempts is how many times the command ran.
	Attempts int
	// Retryable reports whether Class is a retryable connectivity class.
	Retryable bool
	// Err is the last attempt's error.
	Err error
}

// Error returns the underlying error text unchanged.
func (f *Failure) Error() string { return f.Err.Error() }

// Unwrap returns the underlying error.
func (f *Failure) Unwrap() error { return f.Err }

// RefreshRequester is the hook for the later per-host address cache. After a
// resolve or connect failure the loop calls RequestRefresh with the provider
// host. The call must not block. The returned channel, when not nil, closes
// when the lookup finishes, and the retry waits for it up to DefaultRefreshWait.
// The default requester does nothing.
type RefreshRequester interface {
	RequestRefresh(host string) <-chan struct{}
}

type noRefresh struct{}

func (noRefresh) RequestRefresh(string) <-chan struct{} { return nil }

type (
	sweepKey   struct{}
	refreshKey struct{}
)

// sweepState is the retry budget and the logger of one sync cycle. The budget
// is an atomic counter, so any number of goroutines may draw from it at once
// and the total drawn never exceeds the cap; nothing here relies on the sync
// loop being serial.
type sweepState struct {
	remaining atomic.Int64
	logger    *slog.Logger
}

// take draws one retry. It reports false, drawing nothing, when none is left.
func (s *sweepState) take() bool {
	for {
		n := s.remaining.Load()
		if n <= 0 {
			return false
		}
		if s.remaining.CompareAndSwap(n, n-1) {
			return true
		}
	}
}

func (s *sweepState) refund() { s.remaining.Add(1) }

// WithSweep starts the retry budget of one sync cycle and carries the logger
// for per-attempt log lines. Every command run through RunRetrying with the
// returned context draws its retries from the one budget
// (Limits.RetrySweepBudget). A nil logger drops the log lines and keeps the
// metrics. A context with no sweep has no cap, only the per-command attempts.
func WithSweep(ctx context.Context, limits Limits, logger *slog.Logger) context.Context {
	state := &sweepState{logger: logger}
	state.remaining.Store(int64(limits.retrySweepBudget()))
	return context.WithValue(ctx, sweepKey{}, state)
}

// WithRefreshRequester carries the address-refresh hook to RunRetrying.
func WithRefreshRequester(ctx context.Context, requester RefreshRequester) context.Context {
	return context.WithValue(ctx, refreshKey{}, requester)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // the context error is the shutdown signal itself
	case <-timer.C:
		return nil
	}
}

// jitterBackoff spreads a base wait over 75% to 125% of it so retries that
// failed together do not return together.
func jitterBackoff(base time.Duration) time.Duration {
	// #nosec G404 -- backoff jitter is not security-sensitive
	return time.Duration(float64(base) * (0.75 + 0.5*rand.Float64()))
}

// RunRetrying runs a network git command under its deadline and retries a
// retryable connectivity failure (#8091): attempt, classify, and if the class
// is retryable, the command has attempts left, and the sweep budget has a
// retry, wait the jittered backoff and attempt again with a fresh deadline.
// Any other failure, and a failure with no budget left, is final on that
// attempt exactly as before the retry existed. A cancel of the parent context
// ends the loop at once and is neither retried nor counted. The final failure
// is a *Failure; success returns nil.
func RunRetrying(ctx context.Context, limits Limits, c Command) error {
	attempts := limits.retryAttempts()
	sweep, _ := ctx.Value(sweepKey{}).(*sweepState)
	requester, _ := ctx.Value(refreshKey{}).(RefreshRequester)
	if requester == nil {
		requester = noRefresh{}
	}
	limit, _ := limits.For(c.Operation)
	sleep, spread := c.Sleep, c.Jitter
	if sleep == nil {
		sleep = sleepContext
	}
	if spread == nil {
		spread = jitterBackoff
	}
	// lastBackoff is the wait before the attempt now running, kept across
	// passes so the recovery line can report what the recovery cost.
	var lastBackoff time.Duration
	for attempt := 1; ; attempt++ {
		started := time.Now()
		err := Run(ctx, limits, c.Operation, c.Build)
		report := attemptReport{
			command: c, limit: limit, attempt: attempt, attempts: attempts,
			elapsed: time.Since(started), err: err, class: classNone,
		}
		if err == nil {
			report.outcome = outcomeSuccess
			report.backoff = lastBackoff
			recordAttempt(ctx, sweep, report)
			return nil
		}
		if ctx.Err() != nil {
			return err //nolint:wrapcheck // a shutdown is not a command failure
		}
		if c.Stderr != nil {
			report.stderr = c.Stderr()
		}
		var retryable bool
		report.class, retryable = Classify(err, report.stderr)
		if retryable && attempt < attempts {
			report.willRetry, report.budgetExhausted = takeRetry(sweep)
		}
		if report.willRetry && c.BeforeRetry != nil {
			if cleanupErr := c.BeforeRetry(); cleanupErr != nil {
				report.willRetry, report.cleanupErr = false, cleanupErr
				if sweep != nil {
					sweep.refund()
				}
			}
		}
		if !report.willRetry {
			report.outcome = outcomeFailed
			recordAttempt(ctx, sweep, report)
			return &Failure{Class: report.class, Attempts: attempt, Retryable: retryable, Err: err}
		}
		report.outcome = outcomeRetried
		report.backoff = spread(limits.retryBackoff())
		lastBackoff = report.backoff
		var refreshed <-chan struct{}
		if needsAddressRefresh(report.class) {
			refreshed = requester.RequestRefresh(c.Host)
		}
		recordAttempt(ctx, sweep, report)
		if sleepErr := sleep(ctx, report.backoff); sleepErr != nil {
			return err //nolint:wrapcheck // a shutdown during the backoff is not a command failure
		}
		if refreshed != nil && waitForRefresh(ctx, refreshed) {
			return err //nolint:wrapcheck // a shutdown during the refresh wait is not a command failure
		}
		recordRetryStarted(ctx, report)
	}
}

// takeRetry draws one retry from the sweep budget. A context with no sweep has
// no budget, so every retry is allowed.
func takeRetry(sweep *sweepState) (willRetry, exhausted bool) {
	if sweep == nil || sweep.take() {
		return true, false
	}
	return false, true
}

// waitForRefresh waits for the address refresh the requester started, up to
// DefaultRefreshWait. It reports true when the context ended first: the caller
// is shutting down and the retry must not start.
func waitForRefresh(ctx context.Context, refreshed <-chan struct{}) bool {
	timer := time.NewTimer(DefaultRefreshWait)
	defer timer.Stop()
	select {
	case <-refreshed:
		return false
	case <-ctx.Done():
		return true
	case <-timer.C:
		return false
	}
}
