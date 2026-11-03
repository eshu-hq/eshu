// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Post-admission planner statistics for workflow_work_items (#9871).
//
// A freshly bulk-loaded table carries no planner statistics until autovacuum
// analyzes it (about 37 s after the load: the floor is the global
// autovacuum_naptime, so per-table autovacuum settings do not help). In that
// window the fair-claim candidate plans a family-queue-depth-index scan with
// a rows=1 estimate — 11.5 s at 100,011 items over 200 keys on PostgreSQL
// 18.6, against 4 ms after ANALYZE. Every admission path therefore runs
// ANALYZE (SKIP_LOCKED) once, after its commit, when the table has never
// been analyzed. That covers the cold table (fresh install, recreated, or
// restored table on its first large admission), which is exactly what the
// gate checks. Bulk growth onto an already-analyzed but stale table is
// out of scope here and tracked in #9926.

// workflowWorkItemsStatsGateQuery reports whether workflow_work_items has
// ever been analyzed, manually or by autovacuum.
const workflowWorkItemsStatsGateQuery = `
SELECT last_analyze IS NOT NULL, last_autoanalyze IS NOT NULL
FROM pg_stat_all_tables
WHERE relname = 'workflow_work_items'`

// workflowWorkItemsStatsAnalyzeStatement refreshes the table statistics
// without waiting on a concurrent analyzer or claimer lock: a second
// coordinator admitting onto the same cold table skips instead of queueing
// behind the first ANALYZE.
const workflowWorkItemsStatsAnalyzeStatement = `ANALYZE (SKIP_LOCKED) workflow_work_items`

// workflowStatsAnalyzeTimeout bounds one post-admission ANALYZE. The measured
// cost is under a second at 100k rows; the bound only stops a stalled
// ANALYZE from holding an admission response after its commit.
const workflowStatsAnalyzeTimeout = 60 * time.Second

// errNoWorkflowWorkItemsStatsRow marks a statistics gate read that found no
// pg_stat_all_tables row for workflow_work_items: the schema is not ensured
// on this database.
var errNoWorkflowWorkItemsStatsRow = errors.New("no pg_stat_all_tables row for workflow_work_items")

// maybeAnalyzeWorkflowWorkItems runs ANALYZE (SKIP_LOCKED) on
// workflow_work_items when rowsAdmitted rows just landed and the table has
// never been analyzed. It is non-fatal by design: the admission already
// committed, so a gate or ANALYZE failure is counted and logged, never
// returned. Zero-row admissions return silently — ANALYZE on an empty table
// records nothing, so there is no statistics debt to pay down.
func (s *WorkflowControlStore) maybeAnalyzeWorkflowWorkItems(ctx context.Context, site string, rowsAdmitted int) {
	if s.database == nil || rowsAdmitted <= 0 {
		return
	}
	outcome := s.analyzeWorkflowWorkItemsOnce(ctx, site, rowsAdmitted)
	if s.Instruments != nil && s.Instruments.WorkflowWorkItemsStatsAnalyze != nil {
		s.Instruments.WorkflowWorkItemsStatsAnalyze.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrWorkflowStatsAnalyzeSite(site),
			telemetry.AttrOutcome(outcome),
		))
	}
}

// analyzeWorkflowWorkItemsOnce checks the never-analyzed gate and runs the
// ANALYZE. It returns one of the closed outcome values the counter records.
func (s *WorkflowControlStore) analyzeWorkflowWorkItemsOnce(ctx context.Context, site string, rowsAdmitted int) string {
	ctx, span := otel.Tracer(telemetry.DefaultSignalName).Start(
		ctx,
		telemetry.SpanWorkflowWorkItemsStatsAnalyze,
		trace.WithAttributes(
			attribute.String(telemetry.LogKeyWorkflowStatsAdmission, site),
			attribute.Int(telemetry.LogKeyWorkflowStatsRows, rowsAdmitted),
		),
	)
	defer span.End()
	timeoutCtx, cancel := context.WithTimeout(ctx, workflowStatsAnalyzeTimeout)
	defer cancel()

	finish := func(outcome string, err error, log func(context.Context, string, ...any)) string {
		span.SetAttributes(attribute.String(telemetry.LogKeyWorkflowStatsOutcome, outcome))
		if err != nil {
			span.RecordError(err)
		}
		log(ctx, "workflow work-item statistics gate "+outcome,
			slog.String(telemetry.LogKeyWorkflowStatsAdmission, site),
			slog.String(telemetry.LogKeyWorkflowStatsOutcome, outcome),
			slog.Int(telemetry.LogKeyWorkflowStatsRows, rowsAdmitted))
		return outcome
	}

	rows, err := s.database.QueryContext(timeoutCtx, workflowWorkItemsStatsGateQuery)
	if err != nil {
		return finish(telemetry.WorkflowStatsAnalyzeOutcomeFailed, err, slog.WarnContext)
	}
	var manual, auto, seen bool
	if rows.Next() {
		seen = true
		if err := rows.Scan(&manual, &auto); err != nil {
			_ = rows.Close()
			return finish(telemetry.WorkflowStatsAnalyzeOutcomeFailed, err, slog.WarnContext)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return finish(telemetry.WorkflowStatsAnalyzeOutcomeFailed, err, slog.WarnContext)
	}
	_ = rows.Close()
	if !seen {
		// No pg_stat row for the table: the schema is not ensured on this
		// database. The admission committed without it, so count the miss
		// and move on.
		return finish(telemetry.WorkflowStatsAnalyzeOutcomeFailed, errNoWorkflowWorkItemsStatsRow, slog.WarnContext)
	}
	if manual || auto {
		return finish(telemetry.WorkflowStatsAnalyzeOutcomeSkipped, nil, slog.DebugContext)
	}
	if _, err := s.database.ExecContext(timeoutCtx, workflowWorkItemsStatsAnalyzeStatement); err != nil {
		return finish(telemetry.WorkflowStatsAnalyzeOutcomeFailed, err, slog.WarnContext)
	}
	return finish(telemetry.WorkflowStatsAnalyzeOutcomeAnalyzed, nil, slog.DebugContext)
}
