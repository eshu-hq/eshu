// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
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
	sweeper postgres.CrossplaneSatisfiedByRedriveSweeper,
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
	sweeper postgres.CrossplaneSatisfiedByRedriveSweeper,
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
