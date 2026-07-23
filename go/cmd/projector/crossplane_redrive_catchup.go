// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	// crossplaneRedriveCatchUpInterval bounds how often the catch-up loop
	// reclaims stale Crossplane redrive sweeps. Independent of
	// crossplaneRedriveDefaultLeaseTimeout (10 minutes): a shorter interval
	// than the lease just means most ticks find nothing reclaimable, which is
	// a cheap no-op query, not a correctness problem.
	crossplaneRedriveCatchUpInterval = 2 * time.Minute
	// crossplaneRedriveCatchUpBatchSize bounds how many stale claims one
	// catch-up pass reclaims, keeping each pass's own work bounded.
	crossplaneRedriveCatchUpBatchSize = 50
)

// crossplaneRedriveBatchSweeper is the narrow surface the catch-up loop
// needs. postgres.CrossplaneSatisfiedByRedriveSweeper implements it
// structurally; tests substitute a fake to exercise the loop/tick's error
// handling and ticker/cancellation behavior without a real Postgres.
type crossplaneRedriveBatchSweeper interface {
	SweepBatch(ctx context.Context, limit int) ([]postgres.CrossplaneRedriveSweepResult, error)
}

// runCrossplaneRedriveCatchUpLoop periodically reclaims and completes any
// Crossplane cross-scope SATISFIED_BY redrive sweep left 'queued' or with an
// expired claim lease (issue #5476 P1-a).
//
// The live post-Ack hook (projectorQueue.CrossplaneRedrive, wired in
// buildProjectorService) is a best-effort trigger: a transient DB error
// during its paged fan-out, or a crashed process mid-sweep, leaves
// crossplane_satisfied_by_redrive_state 'claimed' with an expiring lease, and
// NOTHING else revisits that row on its own -- the remaining un-swept target
// scopes would otherwise be stuck forever, reproducing the exact unbounded
// false-negative window #5476 exists to close, now triggered by crash/error
// instead of ingestion order. This loop is that recovery path, mirroring the
// durable-backfill periodic-maintenance shape used elsewhere in this repo
// (e.g. internal/coordinator's reap ticker): run until ctx is done, calling
// sweeper.SweepBatch on every tick.
//
// A per-tick error is logged and the loop continues -- an auxiliary recovery
// pass must never take down the primary projector service, whose job is
// running the main Service.Run loop, not this catch-up sweep.
func runCrossplaneRedriveCatchUpLoop(
	ctx context.Context,
	sweeper crossplaneRedriveBatchSweeper,
	logger *slog.Logger,
) {
	ticker := time.NewTicker(crossplaneRedriveCatchUpInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCrossplaneRedriveCatchUpTick(ctx, sweeper, logger)
		}
	}
}

func runCrossplaneRedriveCatchUpTick(
	ctx context.Context,
	sweeper crossplaneRedriveBatchSweeper,
	logger *slog.Logger,
) {
	results, err := sweeper.SweepBatch(ctx, crossplaneRedriveCatchUpBatchSize)
	if err != nil {
		if logger != nil {
			logger.ErrorContext(
				ctx, "crossplane redrive catch-up sweep failed",
				"error", err,
				"reclaimed_completed", len(results),
			)
		}
		return
	}
	if logger != nil && len(results) > 0 {
		logger.InfoContext(
			ctx, "crossplane redrive catch-up sweep completed",
			"claims_reclaimed", len(results),
		)
	}
}

// serviceRunner is the narrow surface runServiceAndJoinRedrive needs.
// app.Application (the concrete type main.go's `service` holds) satisfies it
// structurally; tests substitute a fake.
type serviceRunner interface {
	Run(context.Context) error
}

// runServiceAndJoinRedrive runs the primary projector service to completion,
// then unconditionally cancels ctx via stop BEFORE joining the redrive
// catch-up goroutine, and returns service's own error verbatim (issue #5476
// P0).
//
// A prior fix relied on deferred LIFO ordering (`defer stop()` registered
// before `defer redriveWG.Wait()`, so Wait() would run first, then stop())
// to guarantee the catch-up goroutine could always exit before the join.
// That reasoning holds ONLY on the signal-triggered shutdown path, where ctx
// is already canceled by the time service.Run returns. It FAILS on the
// other real return path: service.Run can return a non-nil error on a fatal
// Ack/Claim failure WITHOUT itself canceling ctx (projector.Service's
// internal runConcurrent cancels only its own locally-derived CHILD
// context, never the caller's signal-derived ctx), and this function's
// caller returns that error verbatim with no cancellation side effect. On
// that path the deferred Wait() would run first (LIFO), block forever on
// the goroutine's `case <-ctx.Done()` (which never fires, since nothing
// cancels ctx), and the deferred stop() -- queued behind Wait() in the same
// stack -- would never run either: a permanent hang, also skipping every
// later deferred cleanup (db.Close, pprof, telemetry shutdown) until
// SIGKILL.
//
// Calling stop() here, unconditionally, BEFORE Wait(), removes the
// dependency on defer ordering entirely: stop() is idempotent (safe to call
// even when ctx was already canceled by a signal), so both the
// signal-shutdown path and the fatal-error path converge on the same
// guaranteed-to-unblock sequence -- cancel, then join -- regardless of which
// one triggered the return.
func runServiceAndJoinRedrive(
	ctx context.Context,
	service serviceRunner,
	stop context.CancelFunc,
	redriveWG *sync.WaitGroup,
) error {
	err := service.Run(ctx)
	stop()
	redriveWG.Wait()
	return err
}
