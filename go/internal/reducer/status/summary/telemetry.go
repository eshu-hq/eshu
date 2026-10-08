// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// failureClass is the failure_class log value of a failed writer pass.
const failureClass = "status_summary_writer_error"

// record emits one pass's metrics, span events, and logs, one set per model
// transaction.
func (r *Runner) record(ctx context.Context, pass Pass) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("eshu.status_summary.model_key", r.Statement.ModelKey),
		attribute.String("eshu.status_summary.outcome", pass.Outcome),
		attribute.Float64("eshu.status_summary.pass_ms", milliseconds(pass.Duration)),
		attribute.Int("eshu.status_summary.row_count", pass.RowCount),
	)
	if !pass.AsOf.IsZero() {
		span.SetAttributes(attribute.String("eshu.status_summary.as_of", pass.AsOf.Format(time.RFC3339Nano)))
	}
	failedModels := 0
	for _, model := range pass.Models {
		if model.Err != nil {
			// Every failed model's error is on the span, not only the first
			// model's, so a failed companion marks the pass trace as an error.
			failedModels++
			span.RecordError(model.Err, trace.WithAttributes(attribute.String("eshu.status_summary.model_key", model.ModelKey)))
		}
	}
	if failedModels > 0 {
		span.SetStatus(codes.Error, "status summary writer pass failed")
	}
	for _, model := range pass.Models {
		attrs := []attribute.KeyValue{
			attribute.String("eshu.status_summary.model_key", model.ModelKey),
			attribute.String("eshu.status_summary.outcome", model.Outcome),
			attribute.Float64("eshu.status_summary.pass_ms", milliseconds(model.Duration)),
			attribute.Float64("eshu.status_summary.compute_ms", milliseconds(model.Compute)),
			attribute.Int("eshu.status_summary.row_count", model.RowCount),
		}
		if !model.AsOf.IsZero() {
			attrs = append(attrs, attribute.String("eshu.status_summary.as_of", model.AsOf.Format(time.RFC3339Nano)))
		}
		span.AddEvent("status_summary.model", trace.WithAttributes(attrs...))
		r.recordModel(ctx, model)
		r.logModel(ctx, model)
	}
}

// recordModel emits one model transaction's pass counter and, for a model that
// opened a transaction, its duration histogram sample.
func (r *Runner) recordModel(ctx context.Context, model ModelPass) {
	if r.Instruments == nil {
		return
	}
	attrs := metric.WithAttributes(telemetry.AttrModelKey(model.ModelKey), telemetry.AttrOutcome(model.Outcome))
	if r.Instruments.StatusSummaryWriterPasses != nil {
		r.Instruments.StatusSummaryWriterPasses.Add(ctx, 1, attrs)
	}
	if r.Instruments.StatusSummaryWriterPassDuration != nil && model.Duration > 0 {
		r.Instruments.StatusSummaryWriterPassDuration.Record(ctx, model.Duration.Seconds(), attrs)
	}
}

// logModel writes one model transaction's structured log line at the level its
// outcome deserves.
func (r *Runner) logModel(ctx context.Context, model ModelPass) {
	if r.Logger == nil {
		return
	}
	attrs := []any{
		slog.String("model_key", model.ModelKey),
		slog.String("outcome", model.Outcome),
		slog.Float64("pass_ms", milliseconds(model.Duration)),
		slog.Float64("compute_ms", milliseconds(model.Compute)),
		slog.Int("row_count", model.RowCount),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	}
	if !model.AsOf.IsZero() {
		attrs = append(attrs, slog.Time("as_of", model.AsOf))
	}
	switch model.Outcome {
	case OutcomeError:
		attrs = append(attrs, log.Err(model.Err), telemetry.FailureClassAttr(failureClass))
		if state := sqlState(model.Err); state != "" {
			attrs = append(attrs, slog.String("sqlstate", state))
		}
		r.Logger.ErrorContext(ctx, "status summary writer pass failed", attrs...)
	case OutcomeSkippedMissingTable:
		if r.warnedMissingTable.CompareAndSwap(false, true) {
			r.Logger.WarnContext(ctx, "status summary table is not installed; writer skips until the migration applies", attrs...)
		}
	case OutcomeRejectedGuard:
		r.Logger.WarnContext(ctx, "status summary row not advanced: the stored as_of is as new or newer", attrs...)
	default:
		r.Logger.DebugContext(ctx, "status summary writer pass completed", attrs...)
	}
}

// recordOverrun counts and warns about a pass longer than the interval. The
// counter keeps the first model's key as before; the warning names every
// model's own transaction time so a slow companion is attributable.
func (r *Runner) recordOverrun(ctx context.Context, pass Pass, interval time.Duration) {
	if r.Instruments != nil && r.Instruments.StatusSummaryWriterOverruns != nil {
		r.Instruments.StatusSummaryWriterOverruns.Add(ctx, 1,
			metric.WithAttributes(telemetry.AttrModelKey(r.Statement.ModelKey)))
	}
	if r.Logger != nil {
		attrs := []any{
			slog.String("model_key", r.Statement.ModelKey),
			slog.String("outcome", pass.Outcome),
			slog.Float64("pass_ms", milliseconds(pass.Duration)),
			slog.String("interval", interval.String()),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		}
		for _, model := range pass.Models {
			attrs = append(attrs, slog.Float64(model.ModelKey+"_ms", milliseconds(model.Duration)))
		}
		r.Logger.WarnContext(ctx, "status summary writer pass overran its interval", attrs...)
	}
}

// recordUp sets the writer_up gauge for every model the runner writes: 1 while
// the loop runs, 0 after.
func (r *Runner) recordUp(ctx context.Context, value int64) {
	if r.Instruments == nil || r.Instruments.StatusSummaryWriterUp == nil {
		return
	}
	for _, statement := range r.statements() {
		r.Instruments.StatusSummaryWriterUp.Record(ctx, value,
			metric.WithAttributes(telemetry.AttrModelKey(statement.ModelKey)))
	}
}

// logStart logs the writer's configuration once when the loop starts, with
// every model's key and source digest.
func (r *Runner) logStart(ctx context.Context, interval time.Duration) {
	if r.Logger == nil {
		return
	}
	attrs := []any{
		slog.String("model_key", r.Statement.ModelKey),
		slog.String("interval", interval.String()),
		slog.String("pass_deadline", (2 * interval).String()),
		slog.String("lock_key", fmt.Sprintf("%#x", store.WriterLockKey)),
		slog.String("source_sha256", r.Statement.SourceSHA256),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	}
	for _, companion := range r.Companions {
		attrs = append(attrs, slog.String(companion.ModelKey+"_source_sha256", companion.SourceSHA256))
	}
	r.Logger.InfoContext(ctx, "status summary writer started", attrs...)
}

// sqlState returns the Postgres SQLSTATE in err's chain, or an empty string.
func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func milliseconds(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
