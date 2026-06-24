// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

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
            FILTER (
                WHERE status = 'completed'
                  AND updated_at >= $1::timestamptz - INTERVAL '24 hours'
            ) AS recent_completed_generations,
        COUNT(work_item_id)
            FILTER (WHERE status = 'failed_retryable') AS retryable_failures,
        COUNT(work_item_id)
            FILTER (WHERE status = 'failed_terminal') AS terminal_failures
    FROM workflow_work_items
    WHERE collector_kind IN ('oci_registry', 'package_registry')
      AND status IN ('pending', 'claimed', 'failed_retryable', 'failed_terminal', 'completed')
      AND (status <> 'completed' OR updated_at >= $1::timestamptz - INTERVAL '24 hours')
    GROUP BY collector_kind
),
latest_completed AS (
    SELECT DISTINCT ON (collector_kind)
        collector_kind,
        updated_at AS last_completed_at
    FROM workflow_work_items
    WHERE collector_kind IN ('oci_registry', 'package_registry')
      AND status = 'completed'
    ORDER BY collector_kind, updated_at DESC
)
SELECT
    kinds.collector_kind,
    COALESCE(instance_counts.configured_instances, 0) AS configured_instances,
    COALESCE(work_counts.active_scopes, 0) AS active_scopes,
    COALESCE(work_counts.recent_completed_generations, 0) AS recent_completed_generations,
    latest_completed.last_completed_at,
    COALESCE(work_counts.retryable_failures, 0) AS retryable_failures,
    COALESCE(work_counts.terminal_failures, 0) AS terminal_failures
FROM registry_kinds AS kinds
LEFT JOIN instance_counts
  ON instance_counts.collector_kind = kinds.collector_kind
LEFT JOIN work_counts
  ON work_counts.collector_kind = kinds.collector_kind
LEFT JOIN latest_completed
  ON latest_completed.collector_kind = kinds.collector_kind
ORDER BY kinds.collector_kind
`
	registryCollectorFailureClassQuery = `
SELECT
    collector_kind,
    BTRIM(last_failure_class) AS failure_class,
    COUNT(*) AS count
FROM workflow_work_items
WHERE collector_kind IN ('oci_registry', 'package_registry')
  AND status IN ('failed_retryable', 'failed_terminal')
  AND NULLIF(BTRIM(COALESCE(last_failure_class, '')), '') IS NOT NULL
GROUP BY collector_kind, failure_class
ORDER BY collector_kind, failure_class
`
	registryMetadataTargetStatusQuery = `
WITH work_counts AS (
    SELECT
        collector_kind,
        NULLIF(BTRIM(SPLIT_PART(fairness_key, ':', 4)), '') AS ecosystem,
        COUNT(*) AS planned,
        COUNT(*) FILTER (WHERE status = 'completed') AS completed,
        COUNT(*) FILTER (
            WHERE status IN ('pending', 'claimed', 'failed_retryable')
              AND updated_at < $1::timestamptz - INTERVAL '24 hours'
        ) AS stale,
        COUNT(*) FILTER (WHERE status IN ('failed_retryable', 'failed_terminal')) AS failed,
        COUNT(*) FILTER (WHERE last_failure_class = 'registry_rate_limited') AS rate_limited
    FROM workflow_work_items
    WHERE collector_kind = 'package_registry'
      AND NULLIF(BTRIM(SPLIT_PART(fairness_key, ':', 4)), '') IS NOT NULL
    GROUP BY collector_kind, ecosystem
),
warning_counts AS (
    SELECT
        NULLIF(LOWER(BTRIM(fact.payload->>'ecosystem')), '') AS ecosystem,
        COUNT(*) AS skipped
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'package_registry.warning'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
      AND fact.payload->>'warning_code' IN (
        'unsupported_metadata_source',
        'registry_not_found',
        'metadata_too_large',
        'malformed_metadata',
        'credentials_missing'
      )
    GROUP BY ecosystem
)
SELECT
    'package_registry' AS collector_kind,
    COALESCE(work_counts.ecosystem, warning_counts.ecosystem) AS ecosystem,
    COALESCE(work_counts.planned, 0) AS planned,
    COALESCE(work_counts.completed, 0) AS completed,
    COALESCE(warning_counts.skipped, 0) AS skipped,
    COALESCE(work_counts.stale, 0) AS stale,
    COALESCE(work_counts.failed, 0) AS failed,
    COALESCE(work_counts.rate_limited, 0) AS rate_limited
FROM work_counts
FULL OUTER JOIN warning_counts
  ON warning_counts.ecosystem = work_counts.ecosystem
WHERE COALESCE(work_counts.ecosystem, warning_counts.ecosystem) IS NOT NULL
ORDER BY ecosystem
`
)

func readRegistryCollectorSnapshots(
	ctx context.Context,
	queryer Queryer,
	asOf time.Time,
) ([]statuspkg.RegistryCollectorSnapshot, error) {
	rows, err := queryer.QueryContext(ctx, registryCollectorStatusQuery, asOf.UTC())
	if err != nil {
		return nil, fmt.Errorf("read registry collector status: %w", err)
	}
	defer func() { _ = rows.Close() }()

	snapshots := make([]statuspkg.RegistryCollectorSnapshot, 0, 2)
	for rows.Next() {
		var snapshot statuspkg.RegistryCollectorSnapshot
		var configuredInstances int64
		var activeScopes int64
		var recentCompletedGenerations int64
		var lastCompletedAt sql.NullTime
		var retryableFailures int64
		var terminalFailures int64
		if err := rows.Scan(
			&snapshot.CollectorKind,
			&configuredInstances,
			&activeScopes,
			&recentCompletedGenerations,
			&lastCompletedAt,
			&retryableFailures,
			&terminalFailures,
		); err != nil {
			return nil, fmt.Errorf("read registry collector status: %w", err)
		}
		snapshot.CollectorKind = strings.TrimSpace(snapshot.CollectorKind)
		snapshot.ConfiguredInstances = int(configuredInstances)
		snapshot.ActiveScopes = int(activeScopes)
		snapshot.RecentCompletedGenerations = int(recentCompletedGenerations)
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

	metadataTargetCounts, err := readRegistryMetadataTargetCounts(ctx, queryer, asOf)
	if err != nil {
		return nil, err
	}
	failureCounts, err := readRegistryCollectorFailureClassCounts(ctx, queryer)
	if err != nil {
		return nil, err
	}
	for i := range snapshots {
		snapshots[i].FailureClassCounts = failureCounts[snapshots[i].CollectorKind]
		snapshots[i].MetadataTargetCounts = metadataTargetCounts[snapshots[i].CollectorKind]
	}
	return snapshots, nil
}

func readRegistryMetadataTargetCounts(
	ctx context.Context,
	queryer Queryer,
	asOf time.Time,
) (map[string][]statuspkg.RegistryMetadataTargetCount, error) {
	rows, err := queryer.QueryContext(ctx, registryMetadataTargetStatusQuery, asOf.UTC())
	if err != nil {
		return nil, fmt.Errorf("read registry metadata target counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := map[string][]statuspkg.RegistryMetadataTargetCount{}
	for rows.Next() {
		var collectorKind string
		var row statuspkg.RegistryMetadataTargetCount
		var planned int64
		var completed int64
		var skipped int64
		var stale int64
		var failed int64
		var rateLimited int64
		if err := rows.Scan(
			&collectorKind,
			&row.Ecosystem,
			&planned,
			&completed,
			&skipped,
			&stale,
			&failed,
			&rateLimited,
		); err != nil {
			return nil, fmt.Errorf("read registry metadata target counts: %w", err)
		}
		collectorKind = strings.TrimSpace(collectorKind)
		row.Ecosystem = strings.TrimSpace(row.Ecosystem)
		if collectorKind == "" || row.Ecosystem == "" {
			continue
		}
		row.Planned = int(planned)
		row.Completed = int(completed)
		row.Skipped = int(skipped)
		row.Stale = int(stale)
		row.Failed = int(failed)
		row.RateLimited = int(rateLimited)
		counts[collectorKind] = append(counts[collectorKind], row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read registry metadata target counts: %w", err)
	}
	return counts, nil
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
