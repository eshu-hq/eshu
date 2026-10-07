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

// record emits one pass's metrics, span attributes, and logs.
func (r *Runner) record(ctx context.Context, pass Pass) {
	modelKey := r.Statement.ModelKey
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("eshu.status_summary.model_key", modelKey),
		attribute.String("eshu.status_summary.outcome", pass.Outcome),
		attribute.Float64("eshu.status_summary.pass_ms", milliseconds(pass.Duration)),
		attribute.Int("eshu.status_summary.row_count", pass.RowCount),
	)
	if !pass.AsOf.IsZero() {
		span.SetAttributes(attribute.String("eshu.status_summary.as_of", pass.AsOf.Format(time.RFC3339Nano)))
	}
	if pass.Err != nil {
		span.RecordError(pass.Err)
		span.SetStatus(codes.Error, "status summary writer pass failed")
	}
	for _, row := range pass.Rows {
		span.AddEvent("status_summary.row", trace.WithAttributes(
			attribute.String("eshu.status_summary.model_key", row.ModelKey),
			attribute.String("eshu.status_summary.outcome", row.Outcome),
			attribute.Int("eshu.status_summary.row_count", row.RowCount),
			attribute.Float64("eshu.status_summary.compute_ms", milliseconds(row.Compute)),
		))
	}
	r.recordCounters(ctx, pass)
	r.logPass(ctx, pass)
}

// recordCounters emits the pass counter once per model with that model's own
// outcome (a pass that never reached the row writes counts for every model
// with the pass outcome), the per-model compute histogram for each row, and
// one pass duration sample under the first model's key.
func (r *Runner) recordCounters(ctx context.Context, pass Pass) {
	if r.Instruments == nil {
		return
	}
	passes, compute := r.Instruments.StatusSummaryWriterPasses, r.Instruments.StatusSummaryWriterModelCompute
	if len(pass.Rows) > 0 {
		for _, row := range pass.Rows {
			if passes != nil {
				passes.Add(ctx, 1, metric.WithAttributes(telemetry.AttrModelKey(row.ModelKey), telemetry.AttrOutcome(row.Outcome)))
			}
			if compute != nil {
				compute.Record(ctx, row.Compute.Seconds(), metric.WithAttributes(telemetry.AttrModelKey(row.ModelKey)))
			}
		}
	} else if passes != nil {
		for _, statement := range r.statements() {
			passes.Add(ctx, 1, metric.WithAttributes(telemetry.AttrModelKey(statement.ModelKey), telemetry.AttrOutcome(pass.Outcome)))
		}
	}
	if duration := r.Instruments.StatusSummaryWriterPassDuration; duration != nil {
		duration.Record(ctx, pass.Duration.Seconds(),
			metric.WithAttributes(telemetry.AttrModelKey(r.Statement.ModelKey), telemetry.AttrOutcome(pass.Outcome)))
	}
}

// logPass writes the pass's structured log line at the level its outcome
// deserves.
func (r *Runner) logPass(ctx context.Context, pass Pass) {
	if r.Logger == nil {
		return
	}
	attrs := []any{
		slog.String("model_key", r.Statement.ModelKey),
		slog.String("outcome", pass.Outcome),
		slog.Float64("pass_ms", milliseconds(pass.Duration)),
		slog.Int("row_count", pass.RowCount),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	}
	if !pass.AsOf.IsZero() {
		attrs = append(attrs, slog.Time("as_of", pass.AsOf))
	}
	switch pass.Outcome {
	case OutcomeError:
		attrs = append(attrs, log.Err(pass.Err), telemetry.FailureClassAttr(failureClass))
		if state := sqlState(pass.Err); state != "" {
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

// recordOverrun counts and warns about a pass longer than the interval.
func (r *Runner) recordOverrun(ctx context.Context, pass Pass, interval time.Duration) {
	if r.Instruments != nil && r.Instruments.StatusSummaryWriterOverruns != nil {
		r.Instruments.StatusSummaryWriterOverruns.Add(ctx, 1,
			metric.WithAttributes(telemetry.AttrModelKey(r.Statement.ModelKey)))
	}
	if r.Logger != nil {
		r.Logger.WarnContext(ctx, "status summary writer pass overran its interval",
			slog.String("model_key", r.Statement.ModelKey),
			slog.String("outcome", pass.Outcome),
			slog.Float64("pass_ms", milliseconds(pass.Duration)),
			slog.String("interval", interval.String()),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}

// recordUp sets the writer_up gauge: 1 while the loop runs, 0 after.
func (r *Runner) recordUp(ctx context.Context, value int64) {
	if r.Instruments == nil || r.Instruments.StatusSummaryWriterUp == nil {
		return
	}
	r.Instruments.StatusSummaryWriterUp.Record(ctx, value,
		metric.WithAttributes(telemetry.AttrModelKey(r.Statement.ModelKey)))
}

// logStart logs the writer's configuration once when the loop starts.
func (r *Runner) logStart(ctx context.Context, interval time.Duration) {
	if r.Logger == nil {
		return
	}
	r.Logger.InfoContext(ctx, "status summary writer started",
		slog.String("model_key", r.Statement.ModelKey),
		slog.String("interval", interval.String()),
		slog.String("pass_deadline", (2*interval).String()),
		slog.String("lock_key", fmt.Sprintf("%#x", store.WriterLockKey)),
		slog.String("source_sha256", r.Statement.SourceSHA256),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
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
