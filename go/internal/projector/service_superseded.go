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
