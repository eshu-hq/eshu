// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// rationaleRetractProbeSpanEvent names the span event
// observeRationaleRetractProbe adds to the caller's current span so a trace
// reader can see the #5998 rationale retract probe-guard decision without a
// dedicated span.
const rationaleRetractProbeSpanEvent = "rationale_retract_probe"

func (w *EdgeWriter) logSharedEdgeWrite(
	domain string,
	evidenceSource string,
	executionMode string,
	inputRows int,
	writtenRows int,
	droppedRows int,
	routeCount int,
	batchSize int,
	groupBatchSize int,
	duration float64,
	stmts []Statement,
) {
	if w.Logger == nil {
		return
	}
	attrs := []any{
		"domain", domain,
		"evidence_source", evidenceSource,
		"execution_mode", executionMode,
		"input_intents", inputRows,
		"accepted_intents", writtenRows,
		// skipped_intents counts every row that produced no write statement,
		// which includes control rows that carry no edge by design. It is
		// therefore NOT the lost-edge count: dropped_rows below is, and the two
		// differ by exactly the control rows in the batch. They are reported
		// together so an operator reading one is not left to guess the other.
		"skipped_intents", inputRows - writtenRows,
		"dropped_rows", droppedRows,
		"executed_rows", statementRowCount(stmts),
		"route_count", routeCount,
		"statement_count", len(stmts),
		"batch_size", batchSize,
		"group_batch_size", groupBatchSize,
		"duration_seconds", duration,
	}
	if summaries := sharedEdgeStatementSummaries(stmts); len(summaries) > 0 {
		attrs = append(attrs, "statement_summaries", summaries)
	}
	w.Logger.Info("shared edge write completed", attrs...)
}

func (w *EdgeWriter) logSharedEdgeRetractStatement(
	domain string,
	evidenceSource string,
	statementRole string,
	repoCount int,
	duration float64,
	stmt Statement,
) {
	if w.Logger == nil {
		return
	}
	attrs := []any{
		"domain", domain,
		"evidence_source", evidenceSource,
		"statement_role", statementRole,
		"repo_count", repoCount,
		"statement_count", 1,
		"duration_seconds", duration,
	}
	if summary, ok := stmt.Parameters[StatementMetadataSummaryKey].(string); ok && summary != "" {
		attrs = append(attrs, "statement_summary", summary)
	}
	w.Logger.Info("shared edge retract statement completed", attrs...)
}

func (w *EdgeWriter) logSharedEdgeRetractRoleOmitted(
	domain string,
	evidenceSource string,
	statementRole string,
	repoCount int,
	reason string,
) {
	if w.Logger == nil {
		return
	}
	w.Logger.Info(
		"shared edge retract role omitted",
		"domain", domain,
		"evidence_source", evidenceSource,
		"statement_role", statementRole,
		"repo_count", repoCount,
		"reason", reason,
	)
}

func (w *EdgeWriter) recordSharedEdgeRunsOnRetractOmission(
	ctx context.Context,
	domain string,
	reason string,
) {
	if w.Instruments == nil {
		return
	}
	w.Instruments.SharedEdgeRunsOnRetractOmissions.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrDomain(domain),
		telemetry.AttrReason(reason),
	))
}

// observeRationaleRetractProbe records the #5998 rationale retract
// probe-guard decision (retractRationaleEdgesWithProbe,
// edge_writer_rationale_labels.go) through every signal an operator needs to
// distinguish a working guard from a broken probe at 3 AM: a bounded
// outcome-labelled counter, a span event, and a structured log, all carrying
// the same repo_count/sample_repo_id and outcome so the three never disagree.
//
// probeDurationSeconds is wall time around the whole ExecuteProbe call, which
// in the production chain includes waiting for a BackpressureExecutor permit
// (backpressure_executor.go's ExecuteProbe acquires from the same gate as
// writes). It does NOT include retry sleeps: RetryingExecutor.ExecuteProbe
// deliberately does not retry (#5998 review), so a probe_error outcome reports
// one attempt plus its permit wait rather than a retry sequence, and a
// read-only probe never appears in the deadlock-retry series. It is
// therefore NOT comparable to the per-statement backend figures
// in the ledger (ledger:5998-delta-per-label-probe-seeded and its empty-store
// pair): under a saturated canonical gate this number is dominated by permit
// queueing, and an operator who reads it as pure statement latency will
// conclude the probe got slow when the gate is what is full.
//
// repo_count plus one sample_repo_id, not the full repoIDs slice (review F9):
// batch size scales with BatchLimit (default 100), so a full list would put
// roughly a hundred identifiers on every span event and log line per retract.
// This mirrors reportUnroutableRows' input_rows/dropped_rows counts plus one
// sample_intent_id in edge_writer_unroutable.go -- the established pattern in
// this file's package for a per-write operator signal that must stay bounded
// regardless of batch size.
//
// scope is a second bounded label taking exactly two values,
// rationaleWholeScopeProbeScope and rationaleDeltaProbeScope. Without it the
// two guarded paths collapse into one series: the delta path fires seven
// statements per RetractEdges batch (one per target label, binding a union of
// delta_file_paths across every delta row in the batch) on every incremental
// sync while the whole-scope path fires one per batch, binding every
// repository in it. So a `skipped` count that mixes them cannot
// show which guard is doing the work, and a delta guard that silently stopped
// engaging would be invisible behind the whole-scope path's counts.
func (w *EdgeWriter) observeRationaleRetractProbe(
	ctx context.Context,
	outcome rationaleProbeOutcome,
	repoIDs []string,
	scope rationaleProbeScope,
	probeDurationSeconds float64,
	probeErr error,
) {
	var sampleRepoID string
	if len(repoIDs) > 0 {
		sampleRepoID = repoIDs[0]
	}

	spanAttrs := []attribute.KeyValue{
		telemetry.AttrOutcome(string(outcome)),
		attribute.Int("repo_count", len(repoIDs)),
		attribute.String("sample_repo_id", sampleRepoID),
		attribute.String("scope", string(scope)),
	}
	if probeErr != nil {
		spanAttrs = append(spanAttrs, attribute.String("error", probeErr.Error()))
	}
	trace.SpanFromContext(ctx).AddEvent(rationaleRetractProbeSpanEvent, trace.WithAttributes(spanAttrs...))

	if w.Logger != nil {
		logAttrs := []any{
			"outcome", string(outcome),
			"repo_count", len(repoIDs),
			"sample_repo_id", sampleRepoID,
			"scope", string(scope),
			"probe_duration_seconds", probeDurationSeconds,
		}
		if probeErr != nil {
			logAttrs = append(logAttrs, "error", probeErr.Error())
		}
		w.Logger.Info("rationale retract probe completed", logAttrs...)
	}

	if w.Instruments != nil && w.Instruments.RationaleRetractProbeOutcomes != nil {
		w.Instruments.RationaleRetractProbeOutcomes.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrOutcome(string(outcome)),
			attribute.String("scope", string(scope)),
		))
	}
}

func statementRowCount(stmts []Statement) int {
	total := 0
	for _, stmt := range stmts {
		rows, ok := stmt.Parameters["rows"].([]map[string]any)
		if ok {
			total += len(rows)
		}
	}
	return total
}

func sharedEdgeStatementSummaries(stmts []Statement) []string {
	var summaries []string
	for _, stmt := range stmts {
		summary, ok := stmt.Parameters[StatementMetadataSummaryKey].(string)
		if !ok || summary == "" {
			continue
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

// logWholeScopeRetractSkipped reports the one outcome of the #6166 narrowing
// that would otherwise leave no trace anywhere.
//
// The narrowed half of wholeScopeRetractDomains
// (edge_writer_retract_scope.go) binds collectWholeScopeRefreshRepoIDs, so
// a retract row that carries no refresh intent_type contributes no repository
// id -- which domains those are is that table's business, not this comment's
// (#6276). When
// EVERY row in a batch is unmarked the list comes back empty and the whole-repo
// DELETE is skipped: the rows still write, and any edge the DELETE would have
// removed stays behind. That is a lost retract, and unlike the over-delete it
// replaced it produces no error, no dead letter, and no grouped-write record --
// recordGroupedWrite is never reached, so even the statement counter stays
// silent.
//
// Today no production emitter can produce such a row (every per-edge intent
// builder stamps retract_via_refresh unconditionally, and planRepoWideRetractWork
// routes a marked row to the write path), which is what
// TestSiblingProductionIntentsNeverReachRetractAsUnmarkedRows and its rationale
// sibling pin. This log is the safety net for the day that stops being true:
// it fires only on the anomalous path, so it costs nothing on the normal one,
// and it turns an invisible failure into a greppable one.
func (w *EdgeWriter) logWholeScopeRetractSkipped(domain string, evidenceSource string, rowCount int) {
	if w.Logger == nil || rowCount == 0 {
		return
	}
	w.Logger.Warn(
		"whole-scope retract skipped: no rows carried the refresh intent_type",
		"domain", domain,
		"evidence_source", evidenceSource,
		"retract_row_count", rowCount,
		"repo_ids_bound", 0,
	)
}
