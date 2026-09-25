// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// claimConflictWait is how long a worker waits after a transient claim conflict
// before it claims again. It matches the pipelined poll interval, so a conflict
// backs off exactly like an empty queue does.
const claimConflictWait = 500 * time.Millisecond

// claimConflictFailureClass is the failure_class log value for a claim that
// exhausted the storage layer's bounded conflict retries. It matches the value
// projector.Service logs so one query covers both runtimes.
const claimConflictFailureClass = "projector_claim_conflict"

// maxConsecutiveClaimConflicts bounds how many back-to-back conflicting Claim
// calls one worker tolerates before the run fails. Each Claim call already makes
// its own bounded attempts inside the storage layer (3 statements with jittered
// backoff), so 20 consecutive conflicts is 60 conflicting statements plus at
// least 10 s of claimConflictWait pauses (19 waits of 500 ms): far past any
// transient deadlock or serialization blip, so hitting it means a persistent
// fault that a one-shot run must surface instead of hanging on. It is a code
// constant, not an env var, because no operator needs to tune it.
const maxConsecutiveClaimConflicts = 20

// errClaimConflictsExhausted marks the fatal error claimProjectorWorkWith
// returns once maxConsecutiveClaimConflicts is spent.
var errClaimConflictsExhausted = errors.New("bootstrap projector claim conflicts exhausted")

// claimRetry is the conflict-retry policy claimProjectorWorkWith applies.
// Production always uses defaultClaimRetry; tests shrink it so the cap is
// reachable without waiting out real 500 ms pauses.
type claimRetry struct {
	// wait is the context-aware pause between a conflicting Claim and the next.
	wait time.Duration
	// maxConsecutiveConflicts is how many back-to-back conflicting Claim calls
	// end the run with errClaimConflictsExhausted.
	maxConsecutiveConflicts int
}

// defaultClaimRetry is the policy drainProjectorWorkItem runs with.
var defaultClaimRetry = claimRetry{wait: claimConflictWait, maxConsecutiveConflicts: maxConsecutiveClaimConflicts}

// claimProjectorWork claims one work item with defaultClaimRetry. See
// claimProjectorWorkWith for the retry contract.
func claimProjectorWork(
	ctx context.Context,
	workSource projector.ProjectorWorkSource,
	workerID int,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
) (projector.ScopeGenerationWork, bool, error) {
	return claimProjectorWorkWith(ctx, workSource, workerID, logger, instruments, defaultClaimRetry)
}

// claimProjectorWorkWith claims one work item. A failure.ErrWorkClaimConflict
// means the storage layer already retried the claim on a Postgres deadlock or
// serialization failure and the statement rolled back, so nothing changed: the
// worker logs the conflict, waits retry.wait, and claims again, like
// projector.Service (#7122). It neither ends the worker nor cancels siblings.
//
// The retry is bounded: after retry.maxConsecutiveConflicts back-to-back
// conflicting Claim calls it returns a fatal error wrapping both
// errClaimConflictsExhausted and the last conflict, because bootstrap-index is
// a one-shot run that must finish or exit non-zero, not hang on a persistent
// conflict. A successful or drained claim ends the call, so the count is
// always "consecutive" and starts fresh on the next item. Any other Claim error
// stays fatal, and a wait interrupted by context cancellation returns the
// context error so shutdown is not retried.
//
// Claim duration is recorded per Claim call, never across the wait, so
// eshu_dp_queue_claim_duration_seconds keeps meaning "queue claim latency". The
// eshu_dp_queue_claim_conflict_retries_total counter is emitted by the storage
// layer, which wiring.go gives the shared instruments.
func claimProjectorWorkWith(
	ctx context.Context,
	workSource projector.ProjectorWorkSource,
	workerID int,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
	retry claimRetry,
) (projector.ScopeGenerationWork, bool, error) {
	for conflicts := 1; ; conflicts++ {
		claimStart := time.Now()
		work, ok, err := workSource.Claim(ctx)
		if instruments != nil {
			instruments.QueueClaimDuration.Record(ctx, time.Since(claimStart).Seconds(), metric.WithAttributes(
				attribute.String("queue", "projector"),
			))
		}
		if err == nil {
			return work, ok, nil
		}
		if !errors.Is(err, failure.ErrWorkClaimConflict) {
			return projector.ScopeGenerationWork{}, false, fmt.Errorf("bootstrap projector claim (worker %d): %w", workerID, err)
		}
		if conflicts >= retry.maxConsecutiveConflicts {
			return projector.ScopeGenerationWork{}, false, fmt.Errorf(
				"bootstrap projector claim (worker %d): %w after %d consecutive claim conflicts: %w",
				workerID, errClaimConflictsExhausted, conflicts, err,
			)
		}
		if logger != nil {
			logger.WarnContext(ctx, "bootstrap projector claim conflict; retrying after wait",
				slog.Int("worker_id", workerID),
				slog.Int("consecutive_conflicts", conflicts),
				slog.String(telemetry.LogKeyFailureClass, claimConflictFailureClass),
				slog.String("error", err.Error()),
				telemetry.PhaseAttr(telemetry.PhaseProjection),
			)
		}
		timer := time.NewTimer(retry.wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return projector.ScopeGenerationWork{}, false, fmt.Errorf("bootstrap projector claim (worker %d): %w", workerID, ctx.Err())
		case <-timer.C:
		}
	}
}
