// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector //nolint:dirgate // Root owns superseded-work accounting; service/ owns only service-catalog reducer-intent routing.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

func (s Service) recordSupersededWork(
	ctx context.Context,
	work ScopeGenerationWork,
	start time.Time,
	factCount int,
	heartbeatErr error,
	workerID int,
) bool {
	if !errors.Is(heartbeatErr, ErrWorkSuperseded) {
		return false
	}
	if s.Logger == nil {
		return true
	}

	scopeAttrs := telemetry.ScopeAttrs(work.Scope.ScopeID, work.Generation.GenerationID, work.Scope.SourceSystem)
	logAttrs := make([]any, 0, len(scopeAttrs)+8)
	for _, attr := range scopeAttrs {
		logAttrs = append(logAttrs, attr)
	}
	logAttrs = append(
		logAttrs,
		log.Queue("projector"),
		log.Status("superseded"),
		slog.Int("fact_count", factCount),
		slog.Float64("duration_seconds", time.Since(start).Seconds()),
		log.WorkerID(fmt.Sprintf("%d", workerID)),
		telemetry.PhaseAttr(telemetry.PhaseProjection),
		telemetry.FailureClassAttr("projector_superseded_by_newer_generation"),
		log.Err(heartbeatErr),
	)
	s.Logger.InfoContext(context.WithoutCancel(ctx), "projector work superseded by newer generation", logAttrs...)
	return true
}

// recordClaimLostWork reports whether err means another attempt owns the work
// item. The stale attempt drops the item without stopping other workers; the
// current owner acks or fails it. Like superseded work, a lost claim is not a
// projection outcome, so it is logged but not counted in
// eshu_dp_projections_completed_total, whose success ratio feeds the SLO.
func (s Service) recordClaimLostWork(
	ctx context.Context,
	work ScopeGenerationWork,
	start time.Time,
	factCount int,
	err error,
	operation string,
	workerID int,
) bool {
	if !errors.Is(err, ErrWorkClaimLost) {
		return false
	}
	ctx = context.WithoutCancel(ctx)
	if s.Logger == nil {
		return true
	}
	scopeAttrs := telemetry.ScopeAttrs(work.Scope.ScopeID, work.Generation.GenerationID, work.Scope.SourceSystem)
	logAttrs := make([]any, 0, len(scopeAttrs)+9)
	for _, attr := range scopeAttrs {
		logAttrs = append(logAttrs, attr)
	}
	logAttrs = append(
		logAttrs,
		log.Queue("projector"),
		log.Status("claim_lost"),
		slog.String("operation", operation),
		slog.Int("attempt_count", work.AttemptCount),
		slog.Int("fact_count", factCount),
		slog.Float64("duration_seconds", time.Since(start).Seconds()),
		log.WorkerID(fmt.Sprintf("%d", workerID)),
		telemetry.PhaseAttr(telemetry.PhaseProjection),
		log.Err(err),
	)
	s.Logger.WarnContext(ctx, "projector work claim lost to another attempt", logAttrs...)
	return true
}

// AckWhenScopeFree acks work, retrying while the store reports
// ErrWorkAckDeferred because a same-scope ingestion commit holds the scope row.
// Each retry first renews the lease through heartbeater (when set), so the
// attempt keeps ownership while it waits; the store's lock timeout paces the
// loop. It returns ErrWorkSuperseded or ErrWorkClaimLost from that renewal, and
// returns the deferral error once ctx is done so shutdown does not wait on
// ingestion. Each Ack gets the projector Ack budget even after ctx is canceled.
func AckWhenScopeFree(
	ctx context.Context,
	sink ProjectorWorkSink,
	heartbeater ProjectorWorkHeartbeater,
	work ScopeGenerationWork,
	result Result,
	onDeferred func(retry int),
) error {
	for retry := 1; ; retry++ {
		ackCtx, cancel := projectorAckContext(ctx)
		err := sink.Ack(ackCtx, work, result)
		cancel()
		if !errors.Is(err, ErrWorkAckDeferred) {
			return err
		}
		if onDeferred != nil {
			onDeferred(retry)
		}
		if ctx.Err() != nil {
			return err
		}
		if heartbeater != nil {
			if heartbeatErr := heartbeater.Heartbeat(ctx, work); heartbeatErr != nil {
				return heartbeatErr
			}
		}
	}
}

// ackDeferredLogger logs the first Ack deferral and then every
// ackDeferredLogEvery retries, so a long ingestion commit stays visible
// without a WARN line per lock timeout.
func (s Service) ackDeferredLogger(ctx context.Context, work ScopeGenerationWork, workerID int) func(int) {
	return func(retry int) {
		if s.Logger == nil || (retry != 1 && retry%ackDeferredLogEvery != 0) {
			return
		}
		scopeAttrs := telemetry.ScopeAttrs(work.Scope.ScopeID, work.Generation.GenerationID, work.Scope.SourceSystem)
		logAttrs := make([]any, 0, len(scopeAttrs)+5)
		for _, attr := range scopeAttrs {
			logAttrs = append(logAttrs, attr)
		}
		logAttrs = append(logAttrs,
			log.Queue("projector"),
			slog.Int("ack_retry", retry),
			slog.Int("attempt_count", work.AttemptCount),
			log.WorkerID(fmt.Sprintf("%d", workerID)),
			telemetry.PhaseAttr(telemetry.PhaseProjection),
		)
		s.Logger.WarnContext(ctx, "projector ack waiting for busy scope", logAttrs...)
	}
}

// ackDeferredLogEvery spaces repeated deferral logs; with a 2 s store lock
// timeout this is roughly one line per minute of waiting.
const ackDeferredLogEvery = 30
