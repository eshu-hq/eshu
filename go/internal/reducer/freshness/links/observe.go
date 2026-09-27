// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package links

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// linkOutcome is the closed outcome label of eshu_dp_changed_since_links_total.
type linkOutcome string

const (
	outcomeIdle   linkOutcome = "idle"
	outcomeLinked linkOutcome = "linked"
	outcomeBreak  linkOutcome = "chain_break"
	outcomeRetry  linkOutcome = "retry"
	outcomeError  linkOutcome = "error"
)

// linkOne runs one link transaction under the reducer.changed_since_link span
// and records its metrics and its log line. Scope and generation identifiers
// go to the span and the log, never to a metric label.
func (r *Runner) linkOne(ctx context.Context, scopeID string) linkOutcome {
	var span trace.Span
	if r.Tracer != nil {
		ctx, span = r.Tracer.Start(ctx, telemetry.SpanReducerChangedSinceLink,
			trace.WithAttributes(attribute.String("scope_id", scopeID)))
		defer span.End()
	}
	result, err := r.Linker.LinkNext(ctx, scopeID)
	if err != nil {
		return r.recordFailure(ctx, span, scopeID, err)
	}
	if result.Idle {
		return outcomeIdle
	}
	outcome := outcomeLinked
	if result.Break != "" {
		outcome = outcomeBreak
	}
	r.recordLink(ctx, span, result, outcome)
	return outcome
}

func (r *Runner) recordFailure(ctx context.Context, span trace.Span, scopeID string, err error) linkOutcome {
	if reducercontract.IsRetryable(err) {
		reason, _ := store.RetryReasonOf(err)
		r.count(ctx, store.LinkKindNone, outcomeRetry)
		if r.Instruments != nil {
			r.Instruments.ChangedSinceLinkRetries.Add(ctx, 1, metric.WithAttributes(
				attribute.String(telemetry.MetricDimensionReason, string(reason))))
		}
		if span != nil {
			span.SetAttributes(attribute.String("changed_since.retry_reason", string(reason)))
		}
		return outcomeRetry
	}
	r.count(ctx, store.LinkKindNone, outcomeError)
	if span != nil {
		span.RecordError(err)
	}
	r.logError(ctx, "changed-since link failed", err, slog.String("scope_id", scopeID))
	return outcomeError
}

func (r *Runner) recordLink(ctx context.Context, span trace.Span, result store.LinkResult, outcome linkOutcome) {
	r.count(ctx, result.Kind, outcome)
	if r.Instruments != nil {
		if result.Break != "" {
			r.Instruments.ChangedSinceChainBreaks.Add(ctx, 1, metric.WithAttributes(
				attribute.String(telemetry.MetricDimensionReason, string(result.Break))))
		}
		if outcome == outcomeLinked && result.Kind != store.LinkKindNone {
			kind := metric.WithAttributes(attribute.String(telemetry.MetricDimensionLinkKind, string(result.Kind)))
			r.Instruments.ChangedSinceLinkDuration.Record(ctx, result.Duration.Seconds(), kind)
			r.Instruments.ChangedSinceLinkDeltaRows.Record(ctx, result.DeltaRows, kind)
			r.Instruments.ChangedSinceLinkKeys.Record(ctx, result.Keys, kind)
		}
	}
	if span != nil {
		span.SetAttributes(
			attribute.String("generation_id", result.GenerationID),
			attribute.String("prior_generation_id", result.PriorGenerationID),
			attribute.Int64("changed_since.activation_seq", result.ActivationSeq),
			attribute.String("changed_since.link_kind", string(result.Kind)),
			attribute.String("changed_since.break_reason", string(result.Break)),
			attribute.Int64("changed_since.delta_rows", result.DeltaRows),
		)
	}
	if r.Logger != nil {
		r.Logger.InfoContext(ctx, "changed-since link",
			slog.String("scope_id", result.ScopeID),
			slog.String("generation_id", result.GenerationID),
			slog.String("prior_generation_id", result.PriorGenerationID),
			slog.Int64("activation_seq", result.ActivationSeq),
			slog.String("link_kind", string(result.Kind)),
			slog.String("break_reason", string(result.Break)),
			slog.Int64("delta_rows", result.DeltaRows),
			slog.Int64("keys", result.Keys),
			slog.Float64("duration_seconds", result.Duration.Seconds()),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}

func (r *Runner) count(ctx context.Context, kind store.LinkKind, outcome linkOutcome) {
	if r.Instruments == nil {
		return
	}
	r.Instruments.ChangedSinceLinks.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionLinkKind, string(kind)),
		attribute.String(telemetry.MetricDimensionOutcome, string(outcome)),
	))
}

func (r *Runner) recordGauges(ctx context.Context) {
	stats, err := r.Journal.Stats(ctx)
	if err != nil {
		r.logError(ctx, "changed-since ledger stats failed", err)
		return
	}
	if r.Instruments == nil {
		return
	}
	r.Instruments.ChangedSinceLinkBacklog.Record(ctx, stats.BacklogRows)
	r.Instruments.ChangedSinceLinkLag.Record(ctx, stats.LagSeconds)
	r.Instruments.ChangedSinceStateBytes.Record(ctx, stats.StateBytes)
	r.Instruments.ChangedSinceStateRows.Record(ctx, stats.StateRows)
}

func (r *Runner) logCycle(ctx context.Context, result CycleResult) {
	if r.Logger == nil {
		return
	}
	journaled := result.Journal.SweeperRows + result.Journal.BackfillRows
	if journaled == 0 && result.Linked+result.Breaks+result.Retries+result.Failures+result.Orphans == 0 {
		return
	}
	r.Logger.InfoContext(ctx, "changed-since link cycle completed",
		slog.Int64("journaled_sweeper", result.Journal.SweeperRows),
		slog.Int64("journaled_backfill", result.Journal.BackfillRows),
		slog.Int("linked", result.Linked),
		slog.Int("chain_breaks", result.Breaks),
		slog.Int("retries", result.Retries),
		slog.Int("failures", result.Failures),
		slog.Int("orphan_scopes_deleted", result.Orphans),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}

func (r *Runner) logError(ctx context.Context, msg string, err error, attrs ...any) {
	if r.Logger == nil {
		return
	}
	args := append([]any{
		log.Err(err),
		telemetry.FailureClassAttr("changed_since_link_error"),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	}, attrs...)
	r.Logger.ErrorContext(ctx, msg, args...)
}
