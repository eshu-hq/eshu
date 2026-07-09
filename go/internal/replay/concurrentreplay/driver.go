// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package concurrentreplay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Driver drives N concurrent workers that drain a shared Source and commit
// each collected generation through Committer (design doc 4102, issue #4395,
// parent epic #4389). It is the concurrent replacement for
// collector.Service's single poll-loop consumption of a collector.Source: the
// same Next-then-commit step, run by multiple goroutines against one shared,
// already-thread-safe Source instead of one goroutine against one delegate.
//
// Driver fails fast: the first error from Source.Next or
// Committer.CommitScopeGeneration stops all workers promptly and is returned
// from Run, rather than letting every worker run to its own completion. Driver
// does not reduce Workers in response to contention or errors — the
// configured concurrency is honored for the run's duration; the repository's
// Serialization-Is-Not-A-Fix rule prohibits shrinking a worker count as a
// stand-in for fixing a non-idempotent write path.
type Driver struct {
	// Source is the shared, thread-safe generation source workers drain from.
	// Required.
	Source *Source
	// Committer persists each collected generation Source hands out. Required.
	Committer collector.Committer
	// Workers is the number of concurrent goroutines draining Source. Values
	// <= 0 are treated as 1, which is a valid sequential run, not an error.
	Workers int
	// Logger is optional. When set, Run emits a start record, a drain or
	// error record when it returns, at Info/Error level respectively.
	Logger *slog.Logger
	// Instruments is optional and currently unused by Run. It is reserved so
	// a later slice can thread the existing eshu_dp_* instruments through the
	// driver without another change to Driver's public shape. Which existing
	// instrument(s) apply to a concurrent replay driver invocation — as
	// opposed to minting a new metric, which this slice must not do — is a
	// decision left to that later slice.
	Instruments *telemetry.Instruments
}

// Report summarizes one Driver.Run invocation.
type Report struct {
	// Workers is the worker count Run actually used: Driver.Workers, or 1 if
	// Driver.Workers was <= 0.
	Workers int
	// GenerationsCommitted is the number of generations Committer accepted
	// (returned a nil error for) before Run returned. When Run returns a
	// non-nil error, this may be less than the total generations Source could
	// have produced — Run does not wait for every worker to observe the
	// failure before returning.
	GenerationsCommitted int
	// Duration is the wall-clock time Run spent draining Source.
	Duration time.Duration
}

// Run drains d.Source with d.Workers concurrent goroutines, committing each
// collected generation through d.Committer. Run fails fast: the first error
// from Source.Next or Committer.CommitScopeGeneration cancels a context
// derived from ctx so the other workers stop draining promptly, and that
// error (wrapped with %w) is the one Run returns. Run blocks until every
// worker has returned, so the returned Report.GenerationsCommitted reflects
// whatever additional generations other workers committed concurrently with
// the failure — Run does not attempt to make that count deterministic across
// runs, only to return promptly and accurately count what did commit.
//
// Run returns an error without draining d.Source, and without spawning any
// worker, if d.Source or d.Committer is nil.
func (d *Driver) Run(ctx context.Context) (Report, error) {
	if d.Source == nil {
		return Report{}, errors.New("concurrentreplay: driver source is required")
	}
	if d.Committer == nil {
		return Report{}, errors.New("concurrentreplay: driver committer is required")
	}

	workers := d.Workers
	if workers <= 0 {
		workers = 1
	}

	if d.Logger != nil {
		d.Logger.InfoContext(ctx, "concurrentreplay driver starting", slog.Int("workers", workers))
	}

	start := time.Now()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		committed int64
		failOnce  sync.Once
		firstErr  error
	)
	failFast := func(err error) {
		if err == nil {
			return
		}
		failOnce.Do(func() {
			firstErr = err
			cancel()
		})
	}

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			d.drainOne(runCtx, &committed, failFast)
		}()
	}
	wg.Wait()

	report := Report{
		Workers:              workers,
		GenerationsCommitted: int(atomic.LoadInt64(&committed)),
		Duration:             time.Since(start),
	}

	if d.Logger != nil {
		if firstErr != nil {
			d.Logger.ErrorContext(ctx, "concurrentreplay driver stopped on error",
				slog.Int("workers", report.Workers),
				slog.Int("generations_committed", report.GenerationsCommitted),
				slog.String("error", firstErr.Error()),
			)
		} else {
			d.Logger.InfoContext(ctx, "concurrentreplay driver drained source",
				slog.Int("workers", report.Workers),
				slog.Int("generations_committed", report.GenerationsCommitted),
			)
		}
	}

	return report, firstErr
}

// drainOne runs one worker's loop: repeatedly call d.Source.Next, commit the
// result through d.Committer, and count each successful commit in committed,
// until the source is exhausted or either step fails. failFast is called with
// the first error this worker observes; it is a no-op for every call after
// the first, across all workers, so only one error is ever kept.
func (d *Driver) drainOne(ctx context.Context, committed *int64, failFast func(error)) {
	for {
		gen, ok, err := d.Source.Next(ctx)
		if err != nil {
			failFast(fmt.Errorf("concurrentreplay: driver source next: %w", err))
			return
		}
		if !ok {
			return
		}
		if err := d.Committer.CommitScopeGeneration(ctx, gen.Scope, gen.Generation, gen.Facts); err != nil {
			failFast(fmt.Errorf("concurrentreplay: driver commit generation %q: %w", gen.Generation.GenerationID, err))
			return
		}
		atomic.AddInt64(committed, 1)
	}
}
