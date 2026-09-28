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
	outcomeIdle     linkOutcome = "idle"
	outcomeLinked   linkOutcome = "linked"
	outcomeBreak    linkOutcome = "break"
	outcomeFailed   linkOutcome = "failed"
	outcomePoisoned linkOutcome = "poisoned"
	// outcomeCanceled is a link cut short because the runner's own context
	// ended (reducer shutdown). It is not a failure: the transaction rolled
	// back, the cursor did not move, and the next cycle retries.
	outcomeCanceled linkOutcome = "canceled"
	// outcomeRetry is a non-counting miss. It is counted on
	// eshu_dp_changed_since_link_retries_total, not on links_total.
	outcomeRetry linkOutcome = "retry"
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
	// The runner's context ended (shutdown) while the link ran. The check is
	// on this context, not on the error: the link's own transaction deadline
	// and statement timeout end in context errors too, but leave this
	// context live, and they must still count. Recording now would also fail,
	// since RecordFailure needs this context.
	if ctx.Err() != nil {
		return r.recordCanceled(ctx, span, scopeID, err)
	}
	classify := r.Classify
	if classify == nil {
		classify = CountingFailure
	}
	if failure := classify(err); failure != nil {
		return r.recordCounting(ctx, span, failure)
	}
	if reducercontract.IsRetryable(err) {
		reason, _ := store.RetryReasonOf(err)
		if r.Instruments != nil {
			r.Instruments.ChangedSinceLinkRetries.Add(ctx, 1, metric.WithAttributes(
				attribute.String(telemetry.MetricDimensionReason, string(reason))))
		}
		if span != nil {
			span.SetAttributes(attribute.String("changed_since.retry_reason", string(reason)))
		}
		return outcomeRetry
	}
	// Uncounted: a failure before the link statement ran (begin, lock reads).
	r.count(ctx, store.LinkKindNone, outcomeFailed)
	if span != nil {
		span.RecordError(err)
	}
	r.logError(ctx, "changed-since link failed before its statement ran; not counted", err, slog.String("scope_id", scopeID))
	return outcomeFailed
}

// recordCanceled reports a link the runner's shutdown cut short, during the
// link or during the record of its failure: an INFO log and
// outcome=canceled, with no cursor record and no ERROR.
func (r *Runner) recordCanceled(ctx context.Context, span trace.Span, scopeID string, err error, attrs ...any) linkOutcome {
	r.count(ctx, store.LinkKindNone, outcomeCanceled)
	if span != nil {
		span.SetAttributes(attribute.Bool("changed_since.canceled", true))
	}
	if r.Logger != nil {
		r.Logger.InfoContext(ctx, "changed-since link canceled by shutdown; not counted, retried next cycle",
			append([]any{
				slog.String("scope_id", scopeID), slog.String("error", err.Error()),
				telemetry.PhaseAttr(telemetry.PhaseReduction),
			}, attrs...)...)
	}
	return outcomeCanceled
}

// recordCounting records a counting failure on the cursor (#7127 ruling 8.10)
// and reports it; at the attempt limit the activation is poisoned.
func (r *Runner) recordCounting(ctx context.Context, span trace.Span, failure *store.FailureError) linkOutcome {
	if span != nil {
		span.RecordError(failure)
		span.SetAttributes(attribute.String("changed_since.failure_class", string(failure.Class)))
	}
	if r.Instruments != nil {
		r.Instruments.ChangedSinceLinkFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionFailureClass, string(failure.Class))))
	}
	record, err := r.Linker.RecordFailure(ctx, failure, r.Config.MaxAttempts)
	if err != nil {
		if ctx.Err() != nil {
			// Shutdown cut the record transaction short: nothing was
			// recorded and the cursor did not move, so the next cycle
			// retries. The observed failure class is kept on the log line.
			return r.recordCanceled(ctx, span, failure.ScopeID, err,
				slog.String("link_failure_class", string(failure.Class)),
				slog.Int64("activation_seq", failure.ActivationSeq))
		}
		r.logError(ctx, "changed-since link failure could not be recorded", err,
			slog.String("scope_id", failure.ScopeID), slog.Int64("activation_seq", failure.ActivationSeq))
		r.count(ctx, store.LinkKindNone, outcomeFailed)
		return outcomeFailed
	}
	if !record.Poisoned {
		r.count(ctx, store.LinkKindNone, outcomeFailed)
		r.logError(ctx, "changed-since link failed", failure.Err,
			slog.String("scope_id", failure.ScopeID), slog.String("generation_id", failure.GenerationID),
			slog.Int64("activation_seq", failure.ActivationSeq), slog.String("link_failure_class", string(failure.Class)),
			slog.Int("attempts", record.Attempts), slog.Bool("counted", record.Counted),
			slog.Time("next_attempt_at", record.NextAttemptAt))
		return outcomeFailed
	}
	r.count(ctx, store.LinkKindNone, outcomePoisoned)
	if r.Instruments != nil {
		r.Instruments.ChangedSinceChainBreaks.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionReason, string(store.BreakLinkPoisoned))))
	}
	// One ERROR per poisoning, with the identifiers an operator needs; the
	// cursor row keeps the durable record.
	r.logError(ctx, "changed-since link poisoned: activation skipped as a chain break", failure.Err,
		slog.String("scope_id", failure.ScopeID), slog.String("generation_id", failure.GenerationID),
		slog.Int64("activation_seq", failure.ActivationSeq), slog.String("link_failure_class", string(failure.Class)),
		slog.Int("attempts", record.Attempts), slog.String("break_reason", string(store.BreakLinkPoisoned)))
	return outcomePoisoned
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
	if ctx.Err() != nil {
		// Shutting down: the stats read would fail on the done context.
		return
	}
	stats, err := r.Journal.Stats(ctx)
	if err != nil {
		r.logReadError(ctx, "changed-since ledger stats failed", err)
		return
	}
	if r.Instruments == nil {
		return
	}
	r.Instruments.ChangedSinceLinkBacklog.Record(ctx, stats.BacklogRows)
	r.Instruments.ChangedSinceLinkLag.Record(ctx, stats.LagSeconds)
	r.Instruments.ChangedSinceStateBytes.Record(ctx, stats.StateBytes)
	r.Instruments.ChangedSinceStateRows.Record(ctx, stats.StateRows)
	r.Instruments.ChangedSinceDeltasBytes.Record(ctx, stats.DeltaBytes)
	r.Instruments.ChangedSinceDeltasRows.Record(ctx, stats.DeltaRows)
	r.Instruments.ChangedSinceLinkRetryingScopes.Record(ctx, stats.RetryingScopes)
	r.Instruments.ChangedSinceLinkPoisonedScopes.Record(ctx, stats.PoisonedScopes)
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
		slog.Int("poisoned", result.Poisoned),
		slog.Int("retries", result.Retries),
		slog.Int("failures", result.Failures),
		slog.Int("orphan_scopes_deleted", result.Orphans),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}

// logReadError logs a failed ledger read (stats, orphan scan or delete, a
// cycle's journal pass) at ERROR. When the runner's context is done the
// error is the shutdown itself, so the line drops to INFO with
// shutdown=true instead of paging on every restart. Recorded link failures
// and poisonings use logError: they happened, whatever the context.
func (r *Runner) logReadError(ctx context.Context, msg string, err error, attrs ...any) {
	if r.Logger != nil && ctx.Err() != nil {
		r.Logger.InfoContext(ctx, msg, append([]any{
			log.Err(err), slog.Bool("shutdown", true), telemetry.PhaseAttr(telemetry.PhaseReduction),
		}, attrs...)...)
		return
	}
	r.logError(ctx, msg, err, attrs...)
}

// logError logs an operator-facing ERROR.
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
