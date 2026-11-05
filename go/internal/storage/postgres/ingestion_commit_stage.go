// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

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

// recordMintedGeneration records the durable commit of a new scope generation
// (#10020): the commit_transaction stage log with the outcome fields, a span
// event on the caller's span, and one increment of
// eshu_dp_ingestion_generation_outcomes_total. The skip side lives in
// backfill.Store.RecordSkippedRefresh, so an operator reads minted versus
// skipped per collector kind from one counter. The counter carries no scope
// id; the stage log and span event name the scope.
func (s IngestionStore) recordMintedGeneration(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	commitStart time.Time,
) {
	s.logCommitStage(ctx, scopeValue, generation, "commit_transaction", commitStart,
		slog.String(telemetry.LogKeyCollectorKind, string(scopeValue.CollectorKind)),
		slog.String("refresh_outcome", telemetry.GenerationOutcomeMinted),
	)
	trace.SpanFromContext(ctx).AddEvent("ingestion.generation_minted", trace.WithAttributes(
		attribute.String(telemetry.LogKeyScopeID, scopeValue.ScopeID),
		attribute.String(telemetry.LogKeyCollectorKind, string(scopeValue.CollectorKind)),
		attribute.String(telemetry.LogKeyGenerationID, generation.GenerationID),
	))
	s.Instruments.RecordGenerationOutcome(ctx, string(scopeValue.CollectorKind),
		telemetry.GenerationOutcomeMinted, telemetry.GenerationReasonNone)
}
