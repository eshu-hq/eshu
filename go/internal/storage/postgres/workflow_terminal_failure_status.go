// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const (
	// workflowCollectorTerminalFailuresQuery returns, per eligible collector
	// instance, the newest unresolved terminal work-item failure inside that
	// instance's window (#8803). Go binds one row per enabled, non-deactivated
	// instance as ($1 kind, $2 instance_id, $3 cutoff), cutoff = asOf -
	// GREATEST(3 x scan_interval, 30 min), so no Go duration string is parsed
	// in SQL. A failed_terminal row counts only while updated_at >= cutoff and
	// no later completed row of the same kind, instance, and scope exists:
	// only a completed row proves recovery, so a pending, claimed, or retrying
	// replan does not flap the signal, and another instance's success on a
	// shared scope_id does not clear this one. The inner read seeks
	// workflow_work_items_failed_terminal_instance_updated_idx (migration 211)
	// newest first with work_item_id as the index's tiebreak, so it stops at
	// the first unresolved row without a sort; the anti-join probes the
	// (kind, scope) index per candidate. Only the bounded class and operation
	// tokens are selected; last_failure_message and last_failure_status_code
	// can carry provider text and are never read. last_failure_terminal is not
	// read either: the failed_terminal status is the terminal truth, and the
	// scanner worker dead-letters to failed_terminal without setting that
	// flag, so gating on it would read a terminal failure as healthy.
	//
	// Operator acknowledgements (#8830, migration 231) split the answer in two.
	// The u lateral is the newest unresolved failure that no acknowledgement
	// covers; it is the failure that degrades the instance. Only when u finds
	// nothing does the a lateral look for the newest unresolved failure an
	// acknowledgement covers, so a covered failure is reported as acknowledged
	// and never hides an uncovered failure on a sibling scope. An
	// acknowledgement covers a failed_terminal row t of the same kind,
	// instance, and scope when t has the acknowledged class, t is not older
	// than the acknowledged failure, and no completed row of that target is
	// newer than the acknowledged failure: a recovery or a different class
	// reports again with no write. workflowTerminalFailureAckCovers is the one
	// shared predicate, so the two laterals cannot drift apart. The primary-key
	// probe on the acknowledgement table runs first, and the completed-row
	// probe only for a row an acknowledgement already matches.
	workflowCollectorTerminalFailuresQuery = `
SELECT
    i.collector_kind,
    i.instance_id,
    COALESCE(COALESCE(u.last_failure_class, a.last_failure_class), ''),
    COALESCE(COALESCE(u.last_failure_operation, a.last_failure_operation), ''),
    COALESCE(u.updated_at, a.updated_at),
    a.updated_at IS NOT NULL,
    a.acknowledged_at,
    COALESCE(a.reason, '')
FROM unnest($1::text[], $2::text[], $3::timestamptz[]) AS i(collector_kind, instance_id, cutoff)
LEFT JOIN LATERAL (
    SELECT
        t.last_failure_class,
        t.last_failure_operation,
        t.updated_at
    FROM workflow_work_items AS t
    WHERE t.collector_kind = i.collector_kind
      AND t.collector_instance_id = i.instance_id
      AND t.status = 'failed_terminal'
      AND t.updated_at >= i.cutoff
      AND NOT EXISTS (
          SELECT 1
          FROM workflow_work_items AS s
          WHERE s.collector_kind = t.collector_kind
            AND s.collector_instance_id = t.collector_instance_id
            AND s.scope_id = t.scope_id
            AND s.status = 'completed'
            AND s.updated_at > t.updated_at
      )
      AND NOT EXISTS (
          SELECT 1
          FROM collector_terminal_failure_acknowledgements AS k
          WHERE ` + workflowTerminalFailureAckCovers + `
      )
    ORDER BY t.updated_at DESC, t.work_item_id ASC
    LIMIT 1
) AS u ON TRUE
LEFT JOIN LATERAL (
    SELECT
        t.last_failure_class,
        t.last_failure_operation,
        t.updated_at,
        k.acknowledged_at,
        k.reason
    FROM workflow_work_items AS t
    JOIN collector_terminal_failure_acknowledgements AS k
      ON ` + workflowTerminalFailureAckCovers + `
    WHERE u.updated_at IS NULL
      AND t.collector_kind = i.collector_kind
      AND t.collector_instance_id = i.instance_id
      AND t.status = 'failed_terminal'
      AND t.updated_at >= i.cutoff
    ORDER BY t.updated_at DESC, t.work_item_id ASC
    LIMIT 1
) AS a ON TRUE
WHERE u.updated_at IS NOT NULL OR a.updated_at IS NOT NULL
ORDER BY i.collector_kind ASC, i.instance_id ASC
`
)

// workflowTerminalFailureAckCovers is the join condition between a
// failed_terminal row t and an acknowledgement k (#8830): same kind, instance,
// and scope, and either t is older than the acknowledged failure or t has the
// acknowledged class with no completed row of the target newer than the
// acknowledged failure.
//
// A row older than the acknowledged failure is history that the acknowledged
// failure superseded; without this arm, acknowledging a newer failure would
// resurrect the older failed rows of the same scope. A row at or after the
// acknowledged failure is the same episode only while its class is unchanged
// and no completed row ended it: a recovery or a different class reports
// again. The probe is by primary key first and only reaches the completed rows
// when a key and class match.
const workflowTerminalFailureAckCovers = `k.collector_kind = t.collector_kind
          AND k.collector_instance_id = t.collector_instance_id
          AND k.scope_id = t.scope_id
          AND (
              t.updated_at < k.failure_at
              OR (
                  k.failure_class = COALESCE(t.last_failure_class, '')
                  AND NOT EXISTS (
                      SELECT 1
                      FROM workflow_work_items AS c
                      WHERE c.collector_kind = t.collector_kind
                        AND c.collector_instance_id = t.collector_instance_id
                        AND c.scope_id = t.scope_id
                        AND c.status = 'completed'
                        AND c.updated_at > k.failure_at
                  )
              )
          )`

// terminalFailureWindowCycles is how many of an instance's scan intervals a
// terminal failure keeps counting without a later completed item for its
// scope (#8803). One interval covers the coordinator's replan, one covers
// claim and execution lag, and one is spare, so a permanently failing
// instance re-fails inside the window every cycle and stays reported, while
// an orphaned target ages out. The window never drops below
// defaultCoordinatorRecentFailureWindow.
const terminalFailureWindowCycles = 3

// terminalFailureCutoff returns the oldest updated_at a terminal failure may
// carry and still count: asOf minus GREATEST(terminalFailureWindowCycles x
// scanInterval, defaultCoordinatorRecentFailureWindow). A non-positive
// interval uses schedule.DefaultReconcileInterval, which the floor covers.
func terminalFailureCutoff(asOf time.Time, scanInterval time.Duration) time.Time {
	if scanInterval <= 0 {
		scanInterval = schedule.DefaultReconcileInterval
	}
	window := time.Duration(math.MaxInt64)
	if scanInterval <= window/terminalFailureWindowCycles {
		window = terminalFailureWindowCycles * scanInterval
	}
	window = max(window, defaultCoordinatorRecentFailureWindow)
	return asOf.UTC().Add(-window)
}

func readWorkflowCollectorTerminalFailures(
	ctx context.Context,
	queryer db.Queryer,
	instances []collector.InstanceSummary,
	asOf time.Time,
) ([]collector.TerminalFailure, error) {
	kinds := make([]string, 0, len(instances))
	ids := make([]string, 0, len(instances))
	cutoffs := make([]time.Time, 0, len(instances))
	for _, instance := range instances {
		if !instance.Enabled || !instance.DeactivatedAt.IsZero() {
			continue
		}
		kinds = append(kinds, instance.CollectorKind)
		ids = append(ids, instance.InstanceID)
		cutoffs = append(cutoffs, terminalFailureCutoff(asOf, instance.ScanInterval))
	}
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := queryer.QueryContext(ctx, workflowCollectorTerminalFailuresQuery, kinds, ids, cutoffs)
	if err != nil {
		return nil, fmt.Errorf("read workflow collector terminal failures: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var failures []collector.TerminalFailure
	for rows.Next() {
		var failure collector.TerminalFailure
		var acknowledgedAt sql.NullTime
		if err := rows.Scan(
			&failure.CollectorKind,
			&failure.InstanceID,
			&failure.FailureClass,
			&failure.FailureOperation,
			&failure.ObservedAt,
			&failure.Acknowledged,
			&acknowledgedAt,
			&failure.AcknowledgedReason,
		); err != nil {
			return nil, fmt.Errorf("read workflow collector terminal failures: %w", err)
		}
		failure.ObservedAt = failure.ObservedAt.UTC()
		if acknowledgedAt.Valid {
			failure.AcknowledgedAt = acknowledgedAt.Time.UTC()
		}
		failures = append(failures, failure)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read workflow collector terminal failures: %w", err)
	}
	return failures, nil
}
