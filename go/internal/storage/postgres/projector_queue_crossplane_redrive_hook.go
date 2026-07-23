// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// CrossplaneRedriveSweeper is the narrow surface ProjectorQueue.Ack calls to
// re-drive cross-scope Claim SATISFIED_BY correlations (issue #5476).
// CrossplaneSatisfiedByRedriveSweeper implements it.
type CrossplaneRedriveSweeper interface {
	Sweep(ctx context.Context, xrdScopeID, xrdGenerationID string) (CrossplaneRedriveSweepResult, error)
}

// runCrossplaneRedriveHook calls the wired CrossplaneRedriveSweeper AFTER
// Ack's own transaction has committed -- deliberately outside that
// transaction and on its own error path, mirroring the rejected design's
// review finding (issue #5476): the sweep's target-discovery fan-out is
// unbounded work relative to Ack's own fixed five-statement generation
// activation, so running it inline would turn every activation into a
// variable-length transaction holding Ack's row locks for however long the
// cross-scope sweep takes, including the failure mode where a stale lease
// commits under lock contention.
//
// A hook failure is deliberately never returned to the caller: by the time
// this runs, the generation is already correctly activated (Ack committed),
// and that real, valid work must not be reverted or reported as failed
// because a best-effort cross-scope re-drive attempt errored. The sweep
// itself is durably resumable (CrossplaneRedriveStateStore's claim/lease),
// so a failed attempt here is recovered by a later live trigger or the
// startup/periodic catch-up sweep, not by retrying Ack.
func (q ProjectorQueue) runCrossplaneRedriveHook(ctx context.Context, work projector.ScopeGenerationWork) {
	if q.CrossplaneRedrive == nil {
		return
	}
	if _, err := q.CrossplaneRedrive.Sweep(ctx, work.Scope.ScopeID, work.Generation.GenerationID); err != nil {
		slog.ErrorContext(
			ctx, "crossplane cross-scope satisfied-by redrive sweep failed",
			"scope_id", work.Scope.ScopeID,
			"generation_id", work.Generation.GenerationID,
			"error", err,
		)
		if q.Instruments != nil && q.Instruments.CrossplaneRedriveSweeps != nil {
			q.Instruments.CrossplaneRedriveSweeps.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrOutcome("sweep_error"),
			))
		}
	}
}
