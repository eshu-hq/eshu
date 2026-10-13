// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// startSpan opens the search span with the shape's table and operation.
func (s *Searcher) startSpan(ctx context.Context, table, operation string) (context.Context, trace.Span) {
	if s.Tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return s.Tracer.Start(ctx, "postgres.query", trace.WithAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.operation", operation),
		attribute.String("db.sql.table", table),
	))
}

// record emits the operator signals for one finished search: span attributes
// (search.want is offset + limit + 1, the matches the walk had to find), the
// outcome counter, the tail-cancel, step-cancel and no-progress counters,
// the elapsed and overrun histograms, and one structured log line on every
// partial. Every metric carries the shape's table label. None of them carries
// the search pattern.
func record[R any, K key](ctx context.Context, s *Searcher, span trace.Span, w *walk[R, K], p *partial[K], cursorPresent bool) {
	result := w.outcome()
	elapsed := w.elapsed()
	spanAttrs := make([]attribute.KeyValue, 0, 16)
	spanAttrs = append(spanAttrs, attribute.String("search.scope", "unscoped"))
	if w.shape.contentKind != "" {
		spanAttrs = append(spanAttrs, attribute.String("search.content_kind", w.shape.contentKind))
	}
	span.SetAttributes(append(spanAttrs,
		attribute.Int64("search.budget_ms", w.plan.budget.Milliseconds()),
		attribute.Int64("search.elapsed_ms", elapsed.Milliseconds()),
		attribute.Int64("search.overrun_ms", w.overrun.Milliseconds()),
		attribute.Int("search.probe_rows_visited", w.probeRowsVisited),
		attribute.Int("search.continuation_steps", w.steps),
		attribute.Int("search.continuation_rows", w.stepRowsVisited),
		attribute.Bool("search.tail_ran", w.tailRan),
		attribute.Bool("search.tail_cancelled", w.tailCancelled),
		attribute.String("search.outcome", string(result)),
		attribute.Bool("search.cursor_present", cursorPresent),
		attribute.Int64("search.phase_one_cap_ms", w.plan.phaseOneCap.Milliseconds()),
		attribute.String("search.ended_in", string(w.endedIn)),
		attribute.Int("search.steps_cancelled", w.stepsCancelled),
		attribute.Bool("search.progressed", w.progressed()),
		attribute.Int("search.want", w.want),
	)...)
	if inst := s.Instruments; inst != nil {
		// Every instrument carries the shape's table so an operator can split
		// the file and entity routes; outcome is added where it applies.
		table := metric.WithAttributes(telemetry.AttrTable(w.shape.table))
		byOutcome := metric.WithAttributes(telemetry.AttrTable(w.shape.table), telemetry.AttrOutcome(string(result)))
		inst.ContentSearchUnscoped.Add(ctx, 1, byOutcome)
		inst.ContentSearchUnscopedDuration.Record(ctx, elapsed.Seconds(), byOutcome)
		inst.ContentSearchUnscopedOverrun.Record(ctx, w.overrun.Seconds(), table)
		if w.tailCancelled {
			inst.ContentSearchTailCancel.Add(ctx, 1, table)
		}
		if w.stepsCancelled > 0 {
			inst.ContentSearchStepCancel.Add(ctx, int64(w.stepsCancelled), table)
		}
		if p != nil && !p.progressed {
			inst.ContentSearchNoProgress.Add(ctx, 1, table)
		}
	}
	if p == nil {
		return
	}
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.LogAttrs(ctx, slog.LevelInfo, w.shape.partialMessage,
		slog.String("event_name", w.shape.partialEvent),
		slog.String("reason", p.reason),
		slog.Int64("budget_ms", p.budgetMS),
		slog.Int64("elapsed_ms", p.elapsedMS),
		slog.Int64("overrun_ms", p.overrunMS),
		slog.Int("rows_scanned_in_order", p.rowsScannedInOrder),
		slog.Int("rows_matched", p.rowsMatched),
		slog.Bool("tail_ran", w.tailRan),
		slog.Bool("tail_cancelled", w.tailCancelled),
		slog.Bool("progressed", p.progressed),
		slog.Int("steps_cancelled", w.stepsCancelled),
		slog.String("ended_in", string(w.endedIn)),
	)
}
