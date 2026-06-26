// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

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
