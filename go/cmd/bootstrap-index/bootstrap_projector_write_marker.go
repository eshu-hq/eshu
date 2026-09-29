// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// markBootstrapProjectionWriteStarted runs the shared #7389 write-start marker
// after the bootstrap projector loaded facts and before its first graph or
// content write, and reports whether it handled the item. heartbeatCtx is the
// item's heartbeat context; the heartbeat's own stop error is checked first,
// so a supersede or claim loss it saw wins. Superseded work is recorded as
// superseded and a lost claim is dropped, both with fact count 0 since nothing
// was projected; any other failure is routed to the queue's Fail path through
// the same isolation as a projection failure, which also handles shutdown.
func markBootstrapProjectionWriteStarted(
	itemCtx context.Context,
	heartbeatCtx context.Context,
	marker projector.ProjectionWriteMarker,
	workSink projector.ProjectorWorkSink,
	work projector.ScopeGenerationWork,
	workerID int,
	itemStart time.Time,
	stopHeartbeat bootstrapProjectorHeartbeatStop,
	span trace.Span,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (bool, error) {
	err := projector.MarkProjectionWriteStarted(heartbeatCtx, marker, work,
		projector.WriteMarkerDeferredLogger(itemCtx, logger, work, workerID))
	if err == nil {
		return false, nil
	}
	if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
		err = errors.Join(err, heartbeatErr)
	}
	switch {
	case dropLostBootstrapClaim(itemCtx, work, workerID, err, "write_marker", span, logger):
		return true, nil
	case errors.Is(err, failure.ErrWorkSuperseded):
		recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "superseded", 0, nil, span, instruments, logger)
		return true, nil
	default:
		return true, isolateBootstrapProjectorFailure(itemCtx, workSink, work, workerID, err, span, logger, func() {
			recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "failed", 0, err, span, instruments, logger)
		})
	}
}
