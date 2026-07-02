// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const workflowRunReconciliationMaxAttempts = 3

const listWorkflowRunsForReconciliationQuery = `
SELECT
    run_id,
    trigger_kind,
    status,
    requested_scope_set::text,
    requested_collector,
    created_at,
    updated_at,
    finished_at
FROM workflow_runs
WHERE status NOT IN ('complete', 'failed')
ORDER BY updated_at ASC, run_id ASC
`

const listWorkflowCollectorProgressQuery = `
SELECT
    collector_kind,
    COUNT(*) AS total_work_items,
    COUNT(*) FILTER (WHERE status = 'pending') AS pending_work_items,
    COUNT(*) FILTER (WHERE status = 'claimed') AS claimed_work_items,
    COUNT(*) FILTER (WHERE status = 'completed') AS completed_work_items,
    COUNT(*) FILTER (WHERE status = 'failed_terminal') AS failed_terminal_work_items
FROM workflow_work_items
WHERE run_id = $1
GROUP BY collector_kind
ORDER BY collector_kind ASC
`

const listWorkflowCollectorPhaseCountsQuery = `
SELECT
    item.collector_kind,
    phase.keyspace,
    phase.phase,
    COUNT(DISTINCT item.work_item_id) AS published_work_items
FROM workflow_work_items AS item
JOIN graph_projection_phase_state AS phase
  ON phase.scope_id = item.scope_id
 AND phase.acceptance_unit_id = item.acceptance_unit_id
 AND phase.source_run_id = item.source_run_id
 AND phase.generation_id = item.generation_id
WHERE item.run_id = $1
GROUP BY item.collector_kind, phase.keyspace, phase.phase
ORDER BY item.collector_kind ASC, phase.keyspace ASC, phase.phase ASC
`

// workflowReducerDeadLetterPhaseBridge lists the reducer domains that
// terminally dead-letter into a bounded, single-owner graph projection phase,
// so a fact_work_items dead-letter can be attributed to the exact required
// phase it will never publish (#4459). Keep this list in lockstep with the
// DeadLetterDomain values set in collector_contract.go: both sides name the
// same reducer domain as the sole writer of that phase. Only add a domain
// here when it owns publishing exactly one (keyspace, phase) pair, so a
// dead-letter can never be mis-attributed to a phase another domain might
// still legitimately publish.
const workflowReducerDeadLetterPhaseBridgeSQL = `
    VALUES
        ('deployment_mapping', 'service_uid', 'deployment_mapping'),
        ('workload_materialization', 'service_uid', 'workload_materialization')
`

// listWorkflowCollectorTerminalDeadLetterCountsQuery counts, per collector
// kind and bridged phase, how many same-scope-generation fact_work_items rows
// have permanently dead-lettered (status = 'dead_letter', never a retrying or
// otherwise non-terminal status) for the reducer domain that owns that
// phase's publication. This is deliberately scoped to the domains in
// workflowReducerDeadLetterPhaseBridgeSQL: a dead-letter on any other domain
// is not attributed to a required phase and leaves that phase's completeness
// at its normal "still pending" status, never a false block (#4459).
const listWorkflowCollectorTerminalDeadLetterCountsQuery = `
WITH reducer_dead_letter_phase_bridge(domain, keyspace, phase) AS (
` + workflowReducerDeadLetterPhaseBridgeSQL + `
)
SELECT
    item.collector_kind,
    bridge.keyspace,
    bridge.phase,
    COUNT(DISTINCT fact.work_item_id) AS dead_lettered_work_items
FROM workflow_work_items AS item
JOIN fact_work_items AS fact
  ON fact.scope_id = item.scope_id
 AND fact.generation_id = item.generation_id
 AND fact.stage = 'reducer'
 AND fact.status = 'dead_letter'
JOIN reducer_dead_letter_phase_bridge AS bridge
  ON bridge.domain = fact.domain
WHERE item.run_id = $1
GROUP BY item.collector_kind, bridge.keyspace, bridge.phase
ORDER BY item.collector_kind ASC, bridge.keyspace ASC, bridge.phase ASC
`

const updateWorkflowRunStatusQuery = `
UPDATE workflow_runs
SET status = $2,
    updated_at = $3::timestamptz,
    finished_at = CASE
        WHEN $4 THEN $3::timestamptz
        ELSE NULL::timestamptz
    END
WHERE run_id = $1
`

// ReconcileWorkflowRuns derives run status and completeness rows from durable
// workflow work-item progress and reducer-owned phase truth.
func (s *WorkflowControlStore) ReconcileWorkflowRuns(ctx context.Context, observedAt time.Time) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("workflow control store database is required")
	}
	rows, err := s.db.QueryContext(ctx, listWorkflowRunsForReconciliationQuery)
	if err != nil {
		return 0, fmt.Errorf("list workflow runs for reconciliation: %w", err)
	}
	defer func() { _ = rows.Close() }()

	runs := make([]workflow.Run, 0)
	for rows.Next() {
		run, err := scanWorkflowRun(rows)
		if err != nil {
			return 0, fmt.Errorf("list workflow runs for reconciliation: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("list workflow runs for reconciliation: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("list workflow runs for reconciliation: %w", err)
	}

	reconciled := 0
	for _, run := range runs {
		if err := s.reconcileWorkflowRun(ctx, run, observedAt.UTC()); err != nil {
			return 0, err
		}
		reconciled++
	}
	return reconciled, nil
}

func (s *WorkflowControlStore) reconcileWorkflowRun(ctx context.Context, run workflow.Run, observedAt time.Time) error {
	for attempt := 1; attempt <= workflowRunReconciliationMaxAttempts; attempt++ {
		err := s.reconcileWorkflowRunOnce(ctx, run, observedAt)
		if err == nil {
			return nil
		}
		if !isRetryableWorkflowReconciliationError(err) || attempt == workflowRunReconciliationMaxAttempts {
			return err
		}
	}
	return nil
}

func (s *WorkflowControlStore) reconcileWorkflowRunOnce(ctx context.Context, run workflow.Run, observedAt time.Time) error {
	queryTarget := s.db
	execTarget := s.db
	commit := func() error { return nil }
	rollback := func() error { return nil }
	if s.beginner != nil {
		tx, err := s.beginner.Begin(ctx)
		if err != nil {
			return fmt.Errorf("reconcile workflow run %s: begin transaction: %w", run.RunID, err)
		}
		queryTarget = tx
		execTarget = tx
		commit = tx.Commit
		rollback = tx.Rollback
	}
	defer func() { _ = rollback() }()

	progress, err := s.listWorkflowCollectorProgress(ctx, queryTarget, run.RunID)
	if err != nil {
		return fmt.Errorf("reconcile workflow run %s: %w", run.RunID, err)
	}
	phaseCounts, err := s.listWorkflowCollectorPhaseCounts(ctx, queryTarget, run.RunID)
	if err != nil {
		return fmt.Errorf("reconcile workflow run %s: %w", run.RunID, err)
	}
	deadLetterCounts, err := s.listWorkflowCollectorTerminalDeadLetterCounts(ctx, queryTarget, run.RunID)
	if err != nil {
		return fmt.Errorf("reconcile workflow run %s: %w", run.RunID, err)
	}
	for i := range progress {
		progress[i].PublishedPhaseCounts = phaseCounts[string(progress[i].CollectorKind)]
		progress[i].TerminalDeadLetterCounts = deadLetterCounts[string(progress[i].CollectorKind)]
	}

	nextRun, completeness, err := workflow.ReconcileRunProgress(workflow.RunProgressSnapshot{
		Run:        run,
		Collectors: progress,
	}, observedAt)
	if err != nil {
		return fmt.Errorf("reconcile workflow run %s: %w", run.RunID, err)
	}
	if _, err := execTarget.ExecContext(
		ctx,
		updateWorkflowRunStatusQuery,
		nextRun.RunID,
		string(nextRun.Status),
		nextRun.UpdatedAt.UTC(),
		!nextRun.FinishedAt.IsZero(),
	); err != nil {
		return fmt.Errorf("reconcile workflow run %s: update run status: %w", run.RunID, err)
	}
	if err := s.upsertCompletenessStatesWithExecutor(ctx, execTarget, completeness); err != nil {
		return fmt.Errorf("reconcile workflow run %s: upsert completeness: %w", run.RunID, err)
	}
	if err := commit(); err != nil {
		return fmt.Errorf("reconcile workflow run %s: commit transaction: %w", run.RunID, err)
	}
	rollback = func() error { return nil }
	// Emit only after a successful commit: reconcileWorkflowRun retries this
	// function up to workflowRunReconciliationMaxAttempts times on a
	// deadlock/serialization conflict, and a retried attempt must not double
	// count (or count at all, if it never committed) the terminal-dead-letter
	// block signal.
	s.recordTerminalDeadLetterBlocks(ctx, run.RunID, progress, completeness)
	return nil
}

// recordTerminalDeadLetterBlocks emits the operator-facing signal for #4459:
// which required phase was blocked because its owning reducer domain
// dead-lettered terminally. Labeled by collector_kind and domain only
// (bounded, never run_id/scope_id/generation_id, per telemetry cardinality
// rules). A nil Instruments (the default) makes this a no-op so binaries
// without a wired meter provider are unaffected.
func (s *WorkflowControlStore) recordTerminalDeadLetterBlocks(
	ctx context.Context,
	runID string,
	progress []workflow.CollectorRunProgress,
	completeness []workflow.CompletenessState,
) {
	deadLetterCountsByCollector := make(map[string]map[workflow.PhasePublicationKey]int, len(progress))
	for _, collector := range progress {
		deadLetterCountsByCollector[string(collector.CollectorKind)] = collector.TerminalDeadLetterCounts
	}

	for _, state := range completeness {
		if state.Status != workflow.CompletenessStatusBlocked {
			continue
		}
		counts := deadLetterCountsByCollector[string(state.CollectorKind)]
		key := workflow.PhasePublicationKey{
			Keyspace:  state.Keyspace,
			PhaseName: reducer.GraphProjectionPhase(state.PhaseName),
		}
		if counts[key] <= 0 {
			// Blocked for a different reason (terminal collector failure),
			// not a bridged reducer dead-letter. Do not attribute it here.
			continue
		}
		domain := workflowReducerDeadLetterDomainForPhase(state.PhaseName)
		if s.Instruments != nil && s.Instruments.WorkflowRunTerminalDeadLetterBlocks != nil {
			s.Instruments.WorkflowRunTerminalDeadLetterBlocks.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrCollectorKind(string(state.CollectorKind)),
				telemetry.AttrDomain(domain),
			))
		}
		slog.WarnContext(
			ctx, "workflow run blocked by terminal reducer dead-letter",
			slog.String("run_id", runID),
			slog.String("collector_kind", string(state.CollectorKind)),
			slog.String("keyspace", string(state.Keyspace)),
			slog.String("phase", state.PhaseName),
			slog.String("domain", domain),
			slog.Int("dead_lettered_work_items", counts[key]),
		)
	}
}

// workflowReducerDeadLetterDomainForPhase reports the reducer domain
// attributed to a blocked phase name for the log/metric emitted by
// recordTerminalDeadLetterBlocks. Kept in lockstep with
// workflowReducerDeadLetterPhaseBridgeSQL and the DeadLetterDomain values in
// collector_contract.go: today both bridged phases carry the same string as
// their owning domain, but this indirection keeps the log/metric correct even
// if that coincidence ever changes.
func workflowReducerDeadLetterDomainForPhase(phaseName string) string {
	switch phaseName {
	case string(reducer.GraphProjectionPhaseDeploymentMapping):
		return string(reducer.DomainDeploymentMapping)
	case string(reducer.GraphProjectionPhaseWorkloadMaterialization):
		return string(reducer.DomainWorkloadMaterialization)
	default:
		return phaseName
	}
}

func isRetryableWorkflowReconciliationError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.Code {
	case "40001", "40P01":
		return true
	default:
		return false
	}
}

func (s *WorkflowControlStore) listWorkflowCollectorProgress(ctx context.Context, queryer Queryer, runID string) ([]workflow.CollectorRunProgress, error) {
	rows, err := queryer.QueryContext(ctx, listWorkflowCollectorProgressQuery, runID)
	if err != nil {
		return nil, fmt.Errorf("list workflow collector progress: %w", err)
	}
	defer func() { _ = rows.Close() }()

	progress := make([]workflow.CollectorRunProgress, 0)
	for rows.Next() {
		var collectorKind string
		var row workflow.CollectorRunProgress
		if err := rows.Scan(
			&collectorKind,
			&row.TotalWorkItems,
			&row.PendingWorkItems,
			&row.ClaimedWorkItems,
			&row.CompletedWorkItems,
			&row.FailedTerminalItems,
		); err != nil {
			return nil, fmt.Errorf("list workflow collector progress: %w", err)
		}
		row.CollectorKind = scope.CollectorKind(strings.TrimSpace(collectorKind))
		row.PublishedPhaseCounts = make(map[workflow.PhasePublicationKey]int)
		progress = append(progress, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list workflow collector progress: %w", err)
	}
	return progress, nil
}

func (s *WorkflowControlStore) listWorkflowCollectorPhaseCounts(
	ctx context.Context,
	queryer Queryer,
	runID string,
) (map[string]map[workflow.PhasePublicationKey]int, error) {
	rows, err := queryer.QueryContext(ctx, listWorkflowCollectorPhaseCountsQuery, runID)
	if err != nil {
		return nil, fmt.Errorf("list workflow collector phase counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	phaseCounts := make(map[string]map[workflow.PhasePublicationKey]int)
	for rows.Next() {
		var collectorKind string
		var keyspace string
		var phaseName string
		var publishedCount int
		if err := rows.Scan(&collectorKind, &keyspace, &phaseName, &publishedCount); err != nil {
			return nil, fmt.Errorf("list workflow collector phase counts: %w", err)
		}
		collectorKind = strings.TrimSpace(collectorKind)
		if _, ok := phaseCounts[collectorKind]; !ok {
			phaseCounts[collectorKind] = make(map[workflow.PhasePublicationKey]int)
		}
		phaseCounts[collectorKind][workflow.PhasePublicationKey{
			Keyspace:  reducer.GraphProjectionKeyspace(strings.TrimSpace(keyspace)),
			PhaseName: reducer.GraphProjectionPhase(strings.TrimSpace(phaseName)),
		}] = publishedCount
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list workflow collector phase counts: %w", err)
	}
	return phaseCounts, nil
}

// listWorkflowCollectorTerminalDeadLetterCounts loads, per collector kind,
// the terminal reducer dead-letter count for each bridged
// (keyspace, phase) pair the run's work items map to (#4459). The returned
// map only ever contains bridged phases with a confirmed dead_letter row —
// an absent entry means zero, which callers must treat as "no terminal
// dead-letter observed," never as a block.
func (s *WorkflowControlStore) listWorkflowCollectorTerminalDeadLetterCounts(
	ctx context.Context,
	queryer Queryer,
	runID string,
) (map[string]map[workflow.PhasePublicationKey]int, error) {
	rows, err := queryer.QueryContext(ctx, listWorkflowCollectorTerminalDeadLetterCountsQuery, runID)
	if err != nil {
		return nil, fmt.Errorf("list workflow collector terminal dead letter counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	deadLetterCounts := make(map[string]map[workflow.PhasePublicationKey]int)
	for rows.Next() {
		var collectorKind string
		var keyspace string
		var phaseName string
		var deadLetteredCount int
		if err := rows.Scan(&collectorKind, &keyspace, &phaseName, &deadLetteredCount); err != nil {
			return nil, fmt.Errorf("list workflow collector terminal dead letter counts: %w", err)
		}
		collectorKind = strings.TrimSpace(collectorKind)
		if _, ok := deadLetterCounts[collectorKind]; !ok {
			deadLetterCounts[collectorKind] = make(map[workflow.PhasePublicationKey]int)
		}
		deadLetterCounts[collectorKind][workflow.PhasePublicationKey{
			Keyspace:  reducer.GraphProjectionKeyspace(strings.TrimSpace(keyspace)),
			PhaseName: reducer.GraphProjectionPhase(strings.TrimSpace(phaseName)),
		}] = deadLetteredCount
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list workflow collector terminal dead letter counts: %w", err)
	}
	return deadLetterCounts, nil
}

func scanWorkflowRun(rows Rows) (workflow.Run, error) {
	var run workflow.Run
	var triggerKind string
	var status string
	var requestedCollector sql.NullString
	var finishedAt sql.NullTime
	if err := rows.Scan(
		&run.RunID,
		&triggerKind,
		&status,
		&run.RequestedScopeSet,
		&requestedCollector,
		&run.CreatedAt,
		&run.UpdatedAt,
		&finishedAt,
	); err != nil {
		return workflow.Run{}, err
	}
	run.TriggerKind = workflow.TriggerKind(strings.TrimSpace(triggerKind))
	run.Status = workflow.RunStatus(strings.TrimSpace(status))
	if requestedCollector.Valid {
		run.RequestedCollector = strings.TrimSpace(requestedCollector.String)
	}
	if finishedAt.Valid {
		run.FinishedAt = finishedAt.Time.UTC()
	}
	return run, nil
}
