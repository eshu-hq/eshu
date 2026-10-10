// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// operationName keeps the db.operation label the unscoped file search has
// always used, so existing dashboards keep matching.
const operationName = "search_file_content_any_repo_page"

func (s *Searcher) startSpan(ctx context.Context) (context.Context, trace.Span) {
	if s.Tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return s.Tracer.Start(ctx, "postgres.query", trace.WithAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.operation", operationName),
		attribute.String("db.sql.table", "content_files"),
	))
}

// record emits the operator signals for one finished search: span attributes,
// the outcome counter, the tail-cancel counter, the elapsed and overrun
// histograms, and one structured log line on every partial. None of them
// carries the search pattern.
func (s *Searcher) record(ctx context.Context, span trace.Span, w *walk, page querycontract.FileSearchPage, cursorPresent bool) {
	result := w.outcome()
	elapsed := w.elapsed()
	span.SetAttributes(
		attribute.String("search.scope", "unscoped"),
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
	)
	if inst := s.Instruments; inst != nil {
		attrs := metric.WithAttributes(telemetry.AttrOutcome(string(result)))
		inst.ContentSearchUnscoped.Add(ctx, 1, attrs)
		inst.ContentSearchUnscopedDuration.Record(ctx, elapsed.Seconds(), attrs)
		inst.ContentSearchUnscopedOverrun.Record(ctx, w.overrun.Seconds())
		if w.tailCancelled {
			inst.ContentSearchTailCancel.Add(ctx, 1)
		}
	}
	if page.Partial == nil {
		return
	}
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.LogAttrs(ctx, slog.LevelInfo, "unscoped content search returned a partial result",
		slog.String("event_name", "content_search.unscoped_partial"),
		slog.String("reason", page.Partial.Reason),
		slog.Int64("budget_ms", page.Partial.BudgetMS),
		slog.Int64("elapsed_ms", page.Partial.ElapsedMS),
		slog.Int64("overrun_ms", page.Partial.OverrunMS),
		slog.Int("rows_scanned_in_order", page.Partial.RowsScannedInOrder),
		slog.Int("rows_matched", page.Partial.RowsMatched),
		slog.Bool("tail_ran", w.tailRan),
		slog.Bool("tail_cancelled", w.tailCancelled),
	)
}
