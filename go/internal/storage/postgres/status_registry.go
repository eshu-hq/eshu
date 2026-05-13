package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

const (
	registryCollectorStatusQuery = `
WITH registry_kinds(collector_kind) AS (
    VALUES ('oci_registry'), ('package_registry')
),
instance_counts AS (
    SELECT
        collector_kind,
        COUNT(DISTINCT instance_id)
            FILTER (
                WHERE enabled = TRUE
                  AND deactivated_at IS NULL
            ) AS configured_instances
    FROM collector_instances
    WHERE collector_kind IN ('oci_registry', 'package_registry')
    GROUP BY collector_kind
),
work_counts AS (
    SELECT
        collector_kind,
        COUNT(DISTINCT scope_id)
            FILTER (
                WHERE status IN ('pending', 'claimed', 'failed_retryable')
            ) AS active_scopes,
        COUNT(work_item_id)
            FILTER (WHERE status = 'completed') AS completed_generations,
        MAX(updated_at)
            FILTER (WHERE status = 'completed') AS last_completed_at,
        COUNT(work_item_id)
            FILTER (WHERE status = 'failed_retryable') AS retryable_failures,
        COUNT(work_item_id)
            FILTER (WHERE status = 'failed_terminal') AS terminal_failures
    FROM workflow_work_items
    WHERE collector_kind IN ('oci_registry', 'package_registry')
    GROUP BY collector_kind
)
SELECT
    kinds.collector_kind,
    COALESCE(instance_counts.configured_instances, 0) AS configured_instances,
    COALESCE(work_counts.active_scopes, 0) AS active_scopes,
    COALESCE(work_counts.completed_generations, 0) AS completed_generations,
    work_counts.last_completed_at,
    COALESCE(work_counts.retryable_failures, 0) AS retryable_failures,
    COALESCE(work_counts.terminal_failures, 0) AS terminal_failures
FROM registry_kinds AS kinds
LEFT JOIN instance_counts
  ON instance_counts.collector_kind = kinds.collector_kind
LEFT JOIN work_counts
  ON work_counts.collector_kind = kinds.collector_kind
ORDER BY kinds.collector_kind
`
	registryCollectorFailureClassQuery = `
SELECT
    collector_kind,
    COALESCE(NULLIF(BTRIM(last_failure_class), ''), 'unknown') AS failure_class,
    COUNT(*) AS count
FROM workflow_work_items
WHERE collector_kind IN ('oci_registry', 'package_registry')
  AND NULLIF(BTRIM(COALESCE(last_failure_class, '')), '') IS NOT NULL
GROUP BY collector_kind, failure_class
ORDER BY collector_kind, failure_class
`
)

func readRegistryCollectorSnapshots(
	ctx context.Context,
	queryer Queryer,
) ([]statuspkg.RegistryCollectorSnapshot, error) {
	rows, err := queryer.QueryContext(ctx, registryCollectorStatusQuery)
	if err != nil {
		return nil, fmt.Errorf("read registry collector status: %w", err)
	}
	defer func() { _ = rows.Close() }()

	snapshots := make([]statuspkg.RegistryCollectorSnapshot, 0, 2)
	for rows.Next() {
		var snapshot statuspkg.RegistryCollectorSnapshot
		var configuredInstances int64
		var activeScopes int64
		var completedGenerations int64
		var lastCompletedAt sql.NullTime
		var retryableFailures int64
		var terminalFailures int64
		if err := rows.Scan(
			&snapshot.CollectorKind,
			&configuredInstances,
			&activeScopes,
			&completedGenerations,
			&lastCompletedAt,
			&retryableFailures,
			&terminalFailures,
		); err != nil {
			return nil, fmt.Errorf("read registry collector status: %w", err)
		}
		snapshot.CollectorKind = strings.TrimSpace(snapshot.CollectorKind)
		snapshot.ConfiguredInstances = int(configuredInstances)
		snapshot.ActiveScopes = int(activeScopes)
		snapshot.CompletedGenerations = int(completedGenerations)
		if lastCompletedAt.Valid {
			snapshot.LastCompletedAt = lastCompletedAt.Time.UTC()
		}
		snapshot.RetryableFailures = int(retryableFailures)
		snapshot.TerminalFailures = int(terminalFailures)
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read registry collector status: %w", err)
	}

	failureCounts, err := readRegistryCollectorFailureClassCounts(ctx, queryer)
	if err != nil {
		return nil, err
	}
	for i := range snapshots {
		snapshots[i].FailureClassCounts = failureCounts[snapshots[i].CollectorKind]
	}
	return snapshots, nil
}

func readRegistryCollectorFailureClassCounts(
	ctx context.Context,
	queryer Queryer,
) (map[string][]statuspkg.NamedCount, error) {
	rows, err := queryer.QueryContext(ctx, registryCollectorFailureClassQuery)
	if err != nil {
		return nil, fmt.Errorf("read registry collector failure classes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := map[string][]statuspkg.NamedCount{}
	for rows.Next() {
		var collectorKind string
		var failureClass string
		var count int64
		if err := rows.Scan(&collectorKind, &failureClass, &count); err != nil {
			return nil, fmt.Errorf("read registry collector failure classes: %w", err)
		}
		collectorKind = strings.TrimSpace(collectorKind)
		failureClass = strings.TrimSpace(failureClass)
		if collectorKind == "" || failureClass == "" {
			continue
		}
		counts[collectorKind] = append(counts[collectorKind], statuspkg.NamedCount{
			Name:  failureClass,
			Count: int(count),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read registry collector failure classes: %w", err)
	}
	return counts, nil
}
