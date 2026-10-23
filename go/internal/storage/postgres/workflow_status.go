// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/scalars"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

const (
	workflowCoordinatorCollectorInstancesQuery = `
SELECT
    instance_id,
    collector_kind,
    mode,
    enabled,
    bootstrap,
    claims_enabled,
    COALESCE(display_name, ''),
    last_observed_at,
    updated_at,
    deactivated_at
FROM collector_instances
ORDER BY collector_kind ASC, instance_id ASC
`
	workflowCoordinatorRunStatusCountsQuery = `
SELECT status, COUNT(*) AS count
FROM workflow_runs
GROUP BY status
ORDER BY status
`
	workflowCoordinatorWorkItemStatusCountsQuery = `
SELECT status, COUNT(*) AS count
FROM workflow_work_items
GROUP BY status
ORDER BY status
`
	workflowCoordinatorCompletenessCountsQuery = `
SELECT status, COUNT(*) AS count
FROM workflow_run_completeness
GROUP BY status
ORDER BY status
`
	workflowCoordinatorClaimSnapshotQuery = `
SELECT
    (SELECT COUNT(*)
     FROM workflow_claims
     WHERE status = 'active') AS active_claim_count,
    (SELECT COUNT(*)
     FROM workflow_claims
     WHERE status = 'active'
       AND lease_expires_at < $1) AS overdue_claim_count,
    GREATEST(
      COALESCE(
        EXTRACT(
          EPOCH FROM (
            $1 - (
              SELECT MIN(COALESCE(visible_at, created_at))
              FROM workflow_work_items
              WHERE status = 'pending'
            )
          )
        ),
        0
      ),
      0
    ) AS oldest_pending_age_seconds
`
	// workflowCoordinatorRecentFailuresQuery counts failures whose row was last
	// updated within the recent window ($1 = cutoff). It drives the degraded
	// health state so a recovered stack reports healthy again instead of
	// staying degraded on aged all-time failures. Each subquery is served by the
	// existing (status, updated_at DESC) indexes on its table.
	workflowCoordinatorRecentFailuresQuery = `
SELECT
    (SELECT COUNT(*)
     FROM workflow_runs
     WHERE status = 'failed'
       AND updated_at >= $1) AS recent_failed_runs,
    (SELECT COUNT(*)
     FROM workflow_run_completeness
     WHERE status = 'blocked'
       AND updated_at >= $1) AS recent_blocked_completeness,
    (SELECT COUNT(*)
     FROM workflow_work_items
     WHERE status IN ('failed_terminal', 'expired')
       AND updated_at >= $1) AS recent_terminal_work_items
`

	// workflowCollectorTerminalFailuresQuery returns, per enabled collector
	// instance, the newest terminal work-item failure that no later completed
	// work item for the same scope has superseded (#8803). The inner query
	// takes the 20 newest failed_terminal rows of the instance from
	// workflow_work_items_claimable_idx (kind, instance, status), so the cost
	// follows the instance's terminal-row history, not the table size. The
	// anti-join probes workflow_work_items_collector_scope_generation_updated_idx
	// per candidate. A terminal failure followed by a later success on the same
	// scope therefore drops out. Only the bounded class and operation tokens are
	// selected; last_failure_message and last_failure_status_code can carry
	// provider text and are never read.
	workflowCollectorTerminalFailuresQuery = `
SELECT
    i.collector_kind,
    i.instance_id,
    COALESCE(f.last_failure_class, ''),
    COALESCE(f.last_failure_operation, ''),
    COALESCE(f.last_failure_terminal, TRUE),
    f.updated_at
FROM collector_instances AS i
CROSS JOIN LATERAL (
    SELECT
        c.last_failure_class,
        c.last_failure_operation,
        c.last_failure_terminal,
        c.updated_at
    FROM (
        SELECT
            t.scope_id,
            t.last_failure_class,
            t.last_failure_operation,
            t.last_failure_terminal,
            t.updated_at
        FROM workflow_work_items AS t
        WHERE t.collector_kind = i.collector_kind
          AND t.collector_instance_id = i.instance_id
          AND t.status = 'failed_terminal'
        ORDER BY t.updated_at DESC
        LIMIT 20
    ) AS c
    WHERE NOT EXISTS (
        SELECT 1
        FROM workflow_work_items AS s
        WHERE s.collector_kind = i.collector_kind
          AND s.scope_id = c.scope_id
          AND s.status = 'completed'
          AND s.updated_at > c.updated_at
    )
    ORDER BY c.updated_at DESC
    LIMIT 1
) AS f
WHERE i.enabled
  AND i.deactivated_at IS NULL
ORDER BY i.collector_kind ASC, i.instance_id ASC
`
)

// defaultCoordinatorRecentFailureWindow bounds how far back a workflow failure
// counts toward the degraded health state. Failures older than this window are
// treated as aged history and surfaced only as cumulative detail.
const defaultCoordinatorRecentFailureWindow = 30 * time.Minute

func readCoordinatorSnapshot(ctx context.Context, queryer db.Queryer, asOf time.Time) (*statuspkg.CoordinatorSnapshot, error) {
	instances, err := listCoordinatorCollectorInstances(ctx, queryer)
	if err != nil {
		return nil, err
	}
	runCounts, err := listNamedCounts(ctx, queryer, workflowCoordinatorRunStatusCountsQuery, "list workflow run status counts")
	if err != nil {
		return nil, err
	}
	workItemCounts, err := listNamedCounts(ctx, queryer, workflowCoordinatorWorkItemStatusCountsQuery, "list workflow work-item status counts")
	if err != nil {
		return nil, err
	}
	completenessCounts, err := listNamedCounts(ctx, queryer, workflowCoordinatorCompletenessCountsQuery, "list workflow completeness counts")
	if err != nil {
		return nil, err
	}
	activeClaims, overdueClaims, oldestPendingAge, err := readWorkflowCoordinatorClaimSnapshot(ctx, queryer, asOf)
	if err != nil {
		return nil, err
	}
	recentFailures, err := readWorkflowCoordinatorRecentFailures(ctx, queryer, asOf, defaultCoordinatorRecentFailureWindow)
	if err != nil {
		return nil, err
	}
	backpressure, err := readWorkflowCollectorBackpressureStatus(ctx, queryer, asOf)
	if err != nil {
		return nil, err
	}
	terminalFailures, err := readWorkflowCollectorTerminalFailures(ctx, queryer)
	if err != nil {
		return nil, err
	}
	if len(instances) == 0 && len(runCounts) == 0 && len(workItemCounts) == 0 && len(completenessCounts) == 0 && activeClaims == 0 && overdueClaims == 0 && oldestPendingAge == 0 && len(backpressure) == 0 {
		return nil, nil
	}

	return &statuspkg.CoordinatorSnapshot{
		CollectorInstances:        instances,
		RunStatusCounts:           runCounts,
		WorkItemStatusCounts:      workItemCounts,
		CompletenessCounts:        completenessCounts,
		CollectorBackpressure:     backpressure,
		CollectorTerminalFailures: terminalFailures,
		ActiveClaims:              activeClaims,
		OverdueClaims:             overdueClaims,
		OldestPendingAge:          oldestPendingAge,
		RecentFailures:            recentFailures,
	}, nil
}

// readWorkflowCoordinatorRecentFailures counts workflow failures whose row was
// last updated within window before asOf. The result drives the degraded health
// state; cumulative totals remain on the snapshot for operator detail. A nil
// window or non-positive window yields the package default.
func readWorkflowCoordinatorRecentFailures(
	ctx context.Context,
	queryer db.Queryer,
	asOf time.Time,
	window time.Duration,
) (*statuspkg.CoordinatorRecentFailures, error) {
	if window <= 0 {
		window = defaultCoordinatorRecentFailureWindow
	}
	cutoff := asOf.UTC().Add(-window)

	rows, err := queryer.QueryContext(ctx, workflowCoordinatorRecentFailuresQuery, cutoff)
	if err != nil {
		return nil, fmt.Errorf("read workflow coordinator recent failures: %w", err)
	}
	defer func() { _ = rows.Close() }()

	recent := &statuspkg.CoordinatorRecentFailures{Window: window}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("read workflow coordinator recent failures: %w", err)
		}
		return recent, nil
	}

	var failedRuns, blockedCompleteness, terminalWorkItems int64
	if err := rows.Scan(&failedRuns, &blockedCompleteness, &terminalWorkItems); err != nil {
		return nil, fmt.Errorf("read workflow coordinator recent failures: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read workflow coordinator recent failures: %w", err)
	}
	recent.FailedRuns = int(failedRuns)
	recent.BlockedCompleteness = int(blockedCompleteness)
	recent.TerminalWorkItems = int(terminalWorkItems)
	return recent, nil
}

// readWorkflowCollectorTerminalFailures reads the newest unresolved terminal
// work-item failure per enabled collector instance. The result carries only
// bounded class and operation tokens; the status layer re-validates them
// before they reach a payload.
func readWorkflowCollectorTerminalFailures(
	ctx context.Context,
	queryer db.Queryer,
) ([]collector.TerminalFailure, error) {
	rows, err := queryer.QueryContext(ctx, workflowCollectorTerminalFailuresQuery)
	if err != nil {
		return nil, fmt.Errorf("read workflow collector terminal failures: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var failures []collector.TerminalFailure
	for rows.Next() {
		var failure collector.TerminalFailure
		if err := rows.Scan(
			&failure.CollectorKind,
			&failure.InstanceID,
			&failure.FailureClass,
			&failure.FailureOperation,
			&failure.Terminal,
			&failure.ObservedAt,
		); err != nil {
			return nil, fmt.Errorf("read workflow collector terminal failures: %w", err)
		}
		failure.ObservedAt = failure.ObservedAt.UTC()
		failures = append(failures, failure)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read workflow collector terminal failures: %w", err)
	}
	return failures, nil
}

func listCoordinatorCollectorInstances(ctx context.Context, queryer db.Queryer) ([]collector.InstanceSummary, error) {
	rows, err := queryer.QueryContext(ctx, workflowCoordinatorCollectorInstancesQuery)
	if err != nil {
		return nil, fmt.Errorf("list coordinator collector instances: %w", err)
	}
	defer func() { _ = rows.Close() }()

	instances := make([]collector.InstanceSummary, 0)
	for rows.Next() {
		var instance collector.InstanceSummary
		var deactivatedAt sql.NullTime
		if err := rows.Scan(
			&instance.InstanceID,
			&instance.CollectorKind,
			&instance.Mode,
			&instance.Enabled,
			&instance.Bootstrap,
			&instance.ClaimsEnabled,
			&instance.DisplayName,
			&instance.LastObservedAt,
			&instance.UpdatedAt,
			&deactivatedAt,
		); err != nil {
			return nil, fmt.Errorf("list coordinator collector instances: %w", err)
		}
		if deactivatedAt.Valid {
			instance.DeactivatedAt = deactivatedAt.Time.UTC()
		}
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list coordinator collector instances: %w", err)
	}
	return instances, nil
}

func readWorkflowCoordinatorClaimSnapshot(
	ctx context.Context,
	queryer db.Queryer,
	asOf time.Time,
) (int, int, time.Duration, error) {
	rows, err := queryer.QueryContext(ctx, workflowCoordinatorClaimSnapshotQuery, asOf.UTC())
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read workflow coordinator claim snapshot: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, 0, 0, fmt.Errorf("read workflow coordinator claim snapshot: %w", err)
		}
		return 0, 0, 0, nil
	}

	var activeClaims int
	var overdueClaims int
	var oldestPendingAgeSeconds float64
	if err := rows.Scan(&activeClaims, &overdueClaims, &oldestPendingAgeSeconds); err != nil {
		return 0, 0, 0, fmt.Errorf("read workflow coordinator claim snapshot: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, 0, fmt.Errorf("read workflow coordinator claim snapshot: %w", err)
	}
	return activeClaims, overdueClaims, scalars.DurationFromSeconds(oldestPendingAgeSeconds), nil
}
