// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// logCommitStage emits one low-cardinality timing record for the durable
// ingestion transaction. These records intentionally sit at transaction
// boundaries so dogfood runs can distinguish slow Postgres inserts from queue
// enqueue, relationship evidence, or commit latency.
func (s IngestionStore) logCommitStage(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	stage string,
	start time.Time,
	attrs ...any,
) {
	if s.Logger == nil {
		return
	}

	scopeAttrs := telemetry.ScopeAttrs(scopeValue.ScopeID, generation.GenerationID, scopeValue.SourceSystem)
	logAttrs := make([]any, 0, len(scopeAttrs)+len(attrs)+3)
	for _, attr := range scopeAttrs {
		logAttrs = append(logAttrs, attr)
	}
	logAttrs = append(
		logAttrs,
		slog.String("stage", stage),
		slog.Float64("duration_seconds", time.Since(start).Seconds()),
		telemetry.PhaseAttr(telemetry.PhaseEmission),
	)
	logAttrs = append(logAttrs, attrs...)

	s.Logger.InfoContext(ctx, "ingestion commit stage completed", logAttrs...)
}

func (s IngestionStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}

	return time.Now().UTC()
}
