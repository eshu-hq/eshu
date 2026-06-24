// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// logRuntimeStage records human-readable stage timings that mirror the
// low-cardinality projector metrics. Dogfood runs rely on these logs to split
// source-local time between build, graph, content-store, and intent enqueue
// work without requiring an OTEL backend.
func (r Runtime) logRuntimeStage(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generationID string,
	stage string,
	start time.Time,
	attrs ...any,
) {
	if r.Logger == nil {
		return
	}

	scopeAttrs := telemetry.ScopeAttrs(scopeValue.ScopeID, generationID, scopeValue.SourceSystem)
	logAttrs := make([]any, 0, len(scopeAttrs)+len(attrs)+3)
	for _, attr := range scopeAttrs {
		logAttrs = append(logAttrs, attr)
	}
	logAttrs = append(
		logAttrs,
		slog.String("stage", stage),
		slog.Float64("duration_seconds", time.Since(start).Seconds()),
		telemetry.PhaseAttr(telemetry.PhaseProjection),
	)
	logAttrs = append(logAttrs, attrs...)

	r.Logger.InfoContext(ctx, "projector runtime stage completed", logAttrs...)
}
