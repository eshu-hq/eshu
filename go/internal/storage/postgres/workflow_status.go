package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
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
    ) AS oldest_pending_age_seconds
`
)

func readCoordinatorSnapshot(ctx context.Context, queryer Queryer, asOf time.Time) (*statuspkg.CoordinatorSnapshot, error) {
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
	if len(instances) == 0 && len(runCounts) == 0 && len(workItemCounts) == 0 && len(completenessCounts) == 0 && activeClaims == 0 && overdueClaims == 0 && oldestPendingAge == 0 {
		return nil, nil
	}

	return &statuspkg.CoordinatorSnapshot{
		CollectorInstances:   instances,
		RunStatusCounts:      runCounts,
		WorkItemStatusCounts: workItemCounts,
		CompletenessCounts:   completenessCounts,
		ActiveClaims:         activeClaims,
		OverdueClaims:        overdueClaims,
		OldestPendingAge:     oldestPendingAge,
	}, nil
}

func listCoordinatorCollectorInstances(ctx context.Context, queryer Queryer) ([]statuspkg.CollectorInstanceSummary, error) {
	rows, err := queryer.QueryContext(ctx, workflowCoordinatorCollectorInstancesQuery)
	if err != nil {
		return nil, fmt.Errorf("list coordinator collector instances: %w", err)
	}
	defer func() { _ = rows.Close() }()

	instances := make([]statuspkg.CollectorInstanceSummary, 0)
	for rows.Next() {
		var instance statuspkg.CollectorInstanceSummary
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
	queryer Queryer,
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
	return activeClaims, overdueClaims, time.Duration(oldestPendingAgeSeconds * float64(time.Second)), nil
}
