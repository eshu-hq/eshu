package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

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
	for i := range progress {
		progress[i].PublishedPhaseCounts = phaseCounts[string(progress[i].CollectorKind)]
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
	return nil
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
