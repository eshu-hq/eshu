// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

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

// claimProjectorWork claims one work item. A failure.ErrWorkClaimConflict means
// the storage layer already retried the claim on a Postgres deadlock or
// serialization failure and the statement rolled back, so nothing changed: the
// worker logs the conflict, waits claimConflictWait, and claims again, exactly
// like projector.Service (#7122). It neither ends the worker nor cancels
// siblings. Any other Claim error stays fatal, and a wait interrupted by
// context cancellation returns the context error so shutdown is not retried.
// The queue's eshu_dp_queue_claim_conflict_retries_total counter is emitted by
// the storage layer, which wiring.go gives the shared instruments.
func claimProjectorWork(
	ctx context.Context,
	workSource projector.ProjectorWorkSource,
	workerID int,
	logger *slog.Logger,
) (projector.ScopeGenerationWork, bool, error) {
	for {
		work, ok, err := workSource.Claim(ctx)
		if err == nil {
			return work, ok, nil
		}
		if !errors.Is(err, failure.ErrWorkClaimConflict) {
			return projector.ScopeGenerationWork{}, false, fmt.Errorf("bootstrap projector claim (worker %d): %w", workerID, err)
		}
		if logger != nil {
			logger.WarnContext(ctx, "bootstrap projector claim conflict; retrying after wait",
				slog.Int("worker_id", workerID),
				slog.String(telemetry.LogKeyFailureClass, claimConflictFailureClass),
				slog.String("error", err.Error()),
				telemetry.PhaseAttr(telemetry.PhaseProjection),
			)
		}
		timer := time.NewTimer(claimConflictWait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return projector.ScopeGenerationWork{}, false, fmt.Errorf("bootstrap projector claim (worker %d): %w", workerID, ctx.Err())
		case <-timer.C:
		}
	}
}
