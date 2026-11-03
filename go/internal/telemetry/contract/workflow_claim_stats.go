// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Workflow work-item statistics telemetry (#9871). A freshly bulk-loaded
// workflow_work_items table carries no planner statistics until autovacuum
// analyzes it, and the fair-claim candidate plans a 19 s family-index scan in
// that window. The admission paths run ANALYZE (SKIP_LOCKED) once, right after
// the admission commits, when the table has never been analyzed. Without this
// signal an analyzed admission is indistinguishable from one that left the
// table cold.
//
// Instruments driven by this contract:
//
//   - eshu_dp_workflow_work_items_stats_analyze_total (site, outcome) — one
//     increment per admission that reached the statistics gate with rows
//     admitted: analyzed, skipped (already analyzed), or failed (gate read
//     or ANALYZE error; the admission already committed, so the helper is
//     non-fatal by design). Zero-row admissions tick nothing.
const (
	// MetricWorkflowWorkItemsStatsAnalyze is the post-admission ANALYZE
	// counter name.
	MetricWorkflowWorkItemsStatsAnalyze = "eshu_dp_workflow_work_items_stats_analyze_total"
	// SpanWorkflowWorkItemsStatsAnalyze wraps one post-admission statistics
	// gate check plus the ANALYZE it may run.
	SpanWorkflowWorkItemsStatsAnalyze = "workflow.work_items.stats_analyze"
)

// Workflow stats-analyze sites (#9871): the in-tree admission paths that can
// leave a cold table behind.
const (
	// WorkflowStatsAnalyzeSiteEnqueue is the unguarded bulk insert path
	// (WorkflowControlStore.EnqueueWorkItems).
	WorkflowStatsAnalyzeSiteEnqueue = "enqueue"
	// WorkflowStatsAnalyzeSiteGuarded is the guarded scheduler admission
	// (CreateRunWithWorkItemsIfNoOpenTargets).
	WorkflowStatsAnalyzeSiteGuarded = "guarded"
	// WorkflowStatsAnalyzeSiteScanner is the scanner target admission.
	WorkflowStatsAnalyzeSiteScanner = "scanner"
)

// Workflow stats-analyze outcomes (#9871), recorded on the existing outcome
// dimension.
const (
	// WorkflowStatsAnalyzeOutcomeAnalyzed means the gate found no statistics
	// and the ANALYZE executed.
	WorkflowStatsAnalyzeOutcomeAnalyzed = "analyzed"
	// WorkflowStatsAnalyzeOutcomeSkipped means the table already carried
	// statistics, so no ANALYZE ran.
	WorkflowStatsAnalyzeOutcomeSkipped = "skipped"
	// WorkflowStatsAnalyzeOutcomeFailed means the gate read or the ANALYZE
	// failed. The admission had already committed; the outcome is counted
	// and logged, never returned.
	WorkflowStatsAnalyzeOutcomeFailed = "failed"
)

const (
	// LogKeyWorkflowStatsAdmission is the admission site on a stats-analyze
	// log line.
	LogKeyWorkflowStatsAdmission = "workflow.stats.admission"
	// LogKeyWorkflowStatsOutcome is the gate outcome on a stats-analyze log
	// line.
	LogKeyWorkflowStatsOutcome = "workflow.stats.outcome"
	// LogKeyWorkflowStatsRows is the admitted row count on a stats-analyze
	// log line.
	LogKeyWorkflowStatsRows = "workflow.stats.rows_admitted"
)
