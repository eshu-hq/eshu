// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// This file holds the operator signals of the delta-baseline and
// reconciliation decisions in selection_baseline.go: the bounded counters and
// the WARN/INFO logs that explain each decision.

// logBaselineLookupFailed logs a failed baseline lookup.
func logBaselineLookupFailed(ctx context.Context, logger *slog.Logger, repoPath string, err error) {
	if logger == nil {
		return
	}
	logger.WarnContext(ctx, "git_delta_baseline_lookup_failed", log.RepoPath(repoPath), log.Err(err))
}

// logGraphDirtyLookupFailed logs a failed #7389 uncovered-writer lookup; the
// scope keeps its normal path and is decided again next cycle.
func logGraphDirtyLookupFailed(ctx context.Context, logger *slog.Logger, scopeID string, err error) {
	if logger == nil {
		return
	}
	logger.WarnContext(ctx, "git_delta_baseline_graph_dirty_lookup_failed", log.ScopeID(scopeID), log.Err(err))
}

// logGraphDirty logs the git_delta_baseline_graph_dirty WARN: the generations
// that wrote the graph without activating, and the reconcile decision they
// produced (graph_dirty, or the throttle reason that holds it off).
// Generation ids and timestamps ride only on the log, never a metric label.
func logGraphDirty(ctx context.Context, logger *slog.Logger, scopeID string, writers []scope.UncoveredProjectionWriter, reason string) {
	if logger == nil {
		return
	}
	ids := make([]string, 0, len(writers))
	statuses := make([]string, 0, len(writers))
	classes := make([]string, 0, len(writers))
	started := make([]string, 0, len(writers))
	for _, writer := range writers {
		ids = append(ids, writer.GenerationID)
		statuses = append(statuses, string(writer.Status))
		classes = append(classes, writer.FailureClass)
		started = append(started, writer.ProjectionWriteStartedAt.Format(time.RFC3339Nano))
	}
	logger.WarnContext(ctx, "git_delta_baseline_graph_dirty",
		log.ScopeID(scopeID),
		slog.Any("generation_ids", ids),
		slog.Any("generation_statuses", statuses),
		slog.Any("failure_classes", classes),
		slog.Any("projection_write_started_at", started),
		slog.String("reason", reason),
	)
}

// recordFallback emits the bounded delta-baseline fallback counter so operators
// can watch the rate at which git syncs skip the delta path and re-observe a
// full snapshot. reason is one of the deltaFallback* constants. The metric is best-effort: a nil Instruments is a
// no-op so the sync still runs in instrument-free contexts and tests.
func (b gitDeltaBaseline) recordFallback(ctx context.Context, reason string) {
	if b.Instruments == nil || b.Instruments.DeltaBaselineFallbacks == nil {
		return
	}
	b.Instruments.DeltaBaselineFallbacks.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionSkipReason, reason),
	))
}

// recordReconciliation emits the bounded reconciliation counter, labeled by the
// decision reason, and the git_reconcile_forced log so operators can see how
// often the periodic sweep forces a full re-observation, why, and for which
// scope. scope_id rides only on the log: it is unbounded and never a metric
// label. The log is WARN when the forced full replaces an expired in-flight
// full, retries an unprojected one, or replaces a stale #7978 re-projection
// request, since a repeat of any means projection is not keeping up.
// Best-effort: a nil Instruments or logger is a no-op.
func (b gitDeltaBaseline) recordReconciliation(ctx context.Context, logger *slog.Logger, decision reconcileDecision) {
	if b.Instruments != nil && b.Instruments.ReconciliationFullSnapshots != nil {
		b.Instruments.ReconciliationFullSnapshots.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionReason, decision.Reason),
		))
	}
	if logger == nil {
		return
	}
	level := slog.LevelInfo
	if decision.Reason == reconcileReasonInFlightExpired || decision.Reason == reconcileReasonRetryAfterUnprojected ||
		decision.Reason == scope.ReconcileReasonRequestStale {
		level = slog.LevelWarn
	}
	attrs := []slog.Attr{
		log.ScopeID(decision.ScopeID),
		slog.String("reason", decision.Reason),
	}
	if decision.State.HasProjectedFull {
		attrs = append(attrs, slog.Time("last_projected_full_at", decision.State.LastProjectedFullAt))
	}
	if decision.State.HasLatestFull {
		attrs = append(attrs,
			slog.Time("latest_full_at", decision.State.LatestFullAt),
			slog.String("latest_full_status", string(decision.State.LatestFullStatus)),
		)
	}
	if !decision.ReindexRequestedAt.IsZero() {
		attrs = append(attrs, slog.Time("reindex_requested_at", decision.ReindexRequestedAt))
	}
	attrs = appendReprojectionRequestAttrs(attrs, decision.State)
	logger.LogAttrs(ctx, level, "git_reconcile_forced", attrs...)
}

// recordReconcileSuppressed emits the bounded suppression counter when the
// sweep holds a due scope off because a full generation is still in flight
// (reconcile_in_flight) or recently failed to project (reconcile_retry_backoff).
// A sustained in-flight rate with no forced reconciles means projection is not
// keeping up. Best-effort: a nil Instruments is a no-op.
func (b gitDeltaBaseline) recordReconcileSuppressed(ctx context.Context, reason string) {
	if b.Instruments == nil || b.Instruments.ReconciliationSuppressed == nil {
		return
	}
	b.Instruments.ReconciliationSuppressed.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionReason, reason),
	))
}

// appendReprojectionRequestAttrs adds the #7978 re-projection request, Ack,
// and dead-letter times the decision read, when set, so a WARN for
// reconcile_request_stale shows why the request went stale.
func appendReprojectionRequestAttrs(attrs []slog.Attr, state scope.FullReconcileState) []slog.Attr {
	for _, stamp := range []struct {
		key string
		at  time.Time
	}{
		{"reconcile_requested_at", state.ReconcileRequestedAt},
		{"reconcile_projected_at", state.ReconcileProjectedAt},
		{"reconcile_dead_lettered_at", state.ReconcileDeadLetteredAt},
	} {
		if !stamp.at.IsZero() {
			attrs = append(attrs, slog.Time(stamp.key, stamp.at))
		}
	}
	return attrs
}
