// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

const workflowCollectorBackpressureQuery = `
WITH workflow_collector_backpressure AS (
  SELECT
      collector_kind,
      collector_instance_id,
      source_system,
      COUNT(*) FILTER (WHERE status = 'pending') AS pending_count,
      COUNT(*) FILTER (WHERE status = 'claimed') AS claimed_count,
      COUNT(*) FILTER (
        WHERE status = 'failed_retryable'
           OR (status = 'pending' AND visible_at > $1)
      ) AS retrying_count,
      COUNT(*) FILTER (WHERE status = 'failed_terminal') AS terminal_failed_count,
      COUNT(*) FILTER (WHERE status = 'expired') AS expired_count,
      GREATEST(
        COALESCE(
          EXTRACT(
            EPOCH FROM (
              $1 - (
                MIN(COALESCE(visible_at, created_at))
                  FILTER (WHERE status = 'pending')
              )
            )
          ),
          0
        ),
        0
      ) AS oldest_pending_age_seconds,
      GREATEST(
        COALESCE(
          EXTRACT(
            EPOCH FROM (
              $1 - (
                MIN(updated_at)
                  FILTER (
                    WHERE status = 'failed_retryable'
                       OR (status = 'pending' AND visible_at > $1)
                  )
              )
            )
          ),
          0
        ),
        0
      ) AS oldest_retry_age_seconds,
      GREATEST(
        COALESCE(
          EXTRACT(
            EPOCH FROM (
              (
                MIN(visible_at)
                  FILTER (
                    WHERE (status = 'failed_retryable' AND visible_at > $1)
                       OR (status = 'pending' AND visible_at > $1)
                  )
              ) - $1
            )
          ),
          0
        ),
        0
      ) AS next_retry_delay_seconds
  FROM workflow_work_items
  WHERE status IN ('pending', 'claimed', 'failed_retryable', 'failed_terminal', 'expired')
  GROUP BY collector_kind, collector_instance_id, source_system
),
collector_generation_dead_letter_backpressure AS (
  SELECT
      collector_kind,
      '' AS collector_instance_id,
      source_system,
      COUNT(*) FILTER (WHERE status = 'dead_letter') AS dead_letter_count
  FROM collector_generation_dead_letters
  WHERE status = 'dead_letter'
  GROUP BY collector_kind, source_system
),
active_claims AS (
  SELECT
      item.collector_kind,
      item.collector_instance_id,
      item.source_system,
      COUNT(*) AS active_claim_count,
      COUNT(*) FILTER (WHERE claim.lease_expires_at < $1) AS overdue_claim_count,
      GREATEST(
        COALESCE(
          EXTRACT(EPOCH FROM ($1 - MIN(claim.claimed_at))),
          0
        ),
        0
      ) AS oldest_claim_age_seconds
  FROM workflow_claims AS claim
  JOIN workflow_work_items AS item
    ON item.work_item_id = claim.work_item_id
  WHERE claim.status = 'active'
  GROUP BY item.collector_kind, item.collector_instance_id, item.source_system
)
SELECT
    COALESCE(work.collector_kind, claims.collector_kind, dead_letters.collector_kind) AS collector_kind,
    COALESCE(work.collector_instance_id, claims.collector_instance_id, dead_letters.collector_instance_id) AS collector_instance_id,
    COALESCE(work.source_system, claims.source_system, dead_letters.source_system) AS source_system,
    COALESCE(work.pending_count, 0) AS pending_count,
    COALESCE(work.claimed_count, 0) AS claimed_count,
    COALESCE(work.retrying_count, 0) AS retrying_count,
    COALESCE(dead_letters.dead_letter_count, 0) AS dead_letter_count,
    COALESCE(work.terminal_failed_count, 0) AS terminal_failed_count,
    COALESCE(work.expired_count, 0) AS expired_count,
    COALESCE(claims.active_claim_count, 0) AS active_claim_count,
    COALESCE(claims.overdue_claim_count, 0) AS overdue_claim_count,
    COALESCE(work.oldest_pending_age_seconds, 0) AS oldest_pending_age_seconds,
    COALESCE(work.oldest_retry_age_seconds, 0) AS oldest_retry_age_seconds,
    COALESCE(claims.oldest_claim_age_seconds, 0) AS oldest_claim_age_seconds,
    COALESCE(work.next_retry_delay_seconds, 0) AS next_retry_delay_seconds
FROM workflow_collector_backpressure AS work
FULL OUTER JOIN active_claims AS claims
  ON claims.collector_kind = work.collector_kind
 AND claims.collector_instance_id = work.collector_instance_id
 AND claims.source_system = work.source_system
FULL OUTER JOIN collector_generation_dead_letter_backpressure AS dead_letters
  ON dead_letters.collector_kind = COALESCE(work.collector_kind, claims.collector_kind)
 AND dead_letters.collector_instance_id = COALESCE(work.collector_instance_id, claims.collector_instance_id)
 AND dead_letters.source_system = COALESCE(work.source_system, claims.source_system)
ORDER BY collector_kind ASC, collector_instance_id ASC, source_system ASC
`

const workflowCollectorBackpressureFailureClassQuery = `
SELECT
    collector_kind,
    collector_instance_id,
    source_system,
    last_failure_class,
    COUNT(*) AS count
FROM workflow_work_items
WHERE (
    status IN ('failed_retryable', 'failed_terminal', 'expired')
    OR (status = 'pending' AND visible_at IS NOT NULL AND visible_at > $1)
  )
  AND COALESCE(last_failure_class, '') <> ''
GROUP BY collector_kind, collector_instance_id, source_system, last_failure_class
UNION ALL
SELECT
    collector_kind,
    '' AS collector_instance_id,
    source_system,
    failure_class AS last_failure_class,
    COUNT(*) AS count
FROM collector_generation_dead_letters
WHERE status = 'dead_letter'
  AND COALESCE(failure_class, '') <> ''
GROUP BY collector_kind, source_system, failure_class
ORDER BY collector_kind ASC, collector_instance_id ASC, source_system ASC, last_failure_class ASC
`

func readWorkflowCollectorBackpressureStatus(
	ctx context.Context,
	queryer Queryer,
	asOf time.Time,
) ([]statuspkg.CollectorBackpressureSnapshot, error) {
	rows, err := queryer.QueryContext(ctx, workflowCollectorBackpressureQuery, asOf.UTC())
	if err != nil {
		return nil, fmt.Errorf("read workflow collector backpressure: %w", err)
	}
	defer func() { _ = rows.Close() }()

	backpressure := []statuspkg.CollectorBackpressureSnapshot{}
	byKey := map[string]int{}
	for rows.Next() {
		var row statuspkg.CollectorBackpressureSnapshot
		var oldestPendingSeconds float64
		var oldestRetrySeconds float64
		var oldestClaimSeconds float64
		var nextRetrySeconds float64
		if scanErr := rows.Scan(
			&row.CollectorKind,
			&row.CollectorInstanceID,
			&row.SourceSystem,
			&row.Pending,
			&row.Claimed,
			&row.Retrying,
			&row.DeadLetter,
			&row.TerminalFailed,
			&row.Expired,
			&row.ActiveClaims,
			&row.OverdueClaims,
			&oldestPendingSeconds,
			&oldestRetrySeconds,
			&oldestClaimSeconds,
			&nextRetrySeconds,
		); scanErr != nil {
			return nil, fmt.Errorf("read workflow collector backpressure: %w", scanErr)
		}
		row.OldestPendingAge = durationFromSeconds(oldestPendingSeconds)
		row.OldestRetryAge = durationFromSeconds(oldestRetrySeconds)
		row.OldestClaimAge = durationFromSeconds(oldestClaimSeconds)
		row.NextRetryDelay = durationFromSeconds(nextRetrySeconds)
		byKey[collectorBackpressureKey(row.CollectorKind, row.CollectorInstanceID, row.SourceSystem)] = len(backpressure)
		backpressure = append(backpressure, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read workflow collector backpressure: %w", err)
	}

	if err := attachWorkflowCollectorBackpressureFailureClasses(ctx, queryer, asOf, backpressure, byKey); err != nil {
		return nil, err
	}
	return backpressure, nil
}

func attachWorkflowCollectorBackpressureFailureClasses(
	ctx context.Context,
	queryer Queryer,
	asOf time.Time,
	rows []statuspkg.CollectorBackpressureSnapshot,
	byKey map[string]int,
) error {
	classRows, err := queryer.QueryContext(ctx, workflowCollectorBackpressureFailureClassQuery, asOf.UTC())
	if err != nil {
		return fmt.Errorf("read workflow collector backpressure failure classes: %w", err)
	}
	defer func() { _ = classRows.Close() }()

	for classRows.Next() {
		var collectorKind string
		var collectorInstanceID string
		var sourceSystem string
		var failureClass string
		var count int64
		if scanErr := classRows.Scan(&collectorKind, &collectorInstanceID, &sourceSystem, &failureClass, &count); scanErr != nil {
			return fmt.Errorf("read workflow collector backpressure failure classes: %w", scanErr)
		}
		index, ok := byKey[collectorBackpressureKey(collectorKind, collectorInstanceID, sourceSystem)]
		if !ok {
			continue
		}
		rows[index].FailureClassCounts = append(rows[index].FailureClassCounts, statuspkg.NamedCount{
			Name:  failureClass,
			Count: int(count),
		})
	}
	if err := classRows.Err(); err != nil {
		return fmt.Errorf("read workflow collector backpressure failure classes: %w", err)
	}
	return nil
}

func collectorBackpressureKey(collectorKind string, collectorInstanceID string, sourceSystem string) string {
	return strings.TrimSpace(collectorKind) + "\x00" +
		strings.TrimSpace(collectorInstanceID) + "\x00" +
		strings.TrimSpace(sourceSystem)
}
