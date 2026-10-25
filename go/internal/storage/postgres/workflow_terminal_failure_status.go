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

// workflowCollectorTerminalFailuresQuery returns, per eligible collector
// instance, up to two terminal work-item failures inside that instance's window
// (#8803, #8830): the newest unresolved failure no operator acknowledgement
// covers, and the newest unresolved failure an acknowledgement covers. Go binds
// one row per enabled, non-deactivated instance as ($1 kind, $2 instance_id, $3
// cutoff), cutoff = asOf - GREATEST(3 x scan_interval, 30 min), so no Go
// duration string is parsed in SQL.
//
// A failed_terminal row counts only while updated_at >= cutoff and no later
// completed row of the same kind, instance, and scope exists: only a completed
// row proves recovery, so a pending, claimed, or retrying replan does not flap
// the signal, and another instance's success on a shared scope_id does not
// clear this one. Each lateral seeks
// workflow_work_items_failed_terminal_instance_updated_idx (migration 211)
// newest first with work_item_id as the index's tiebreak, so it stops at its
// first match without a sort; the anti-join probes the (kind, scope) index per
// candidate. Only the bounded class and operation tokens are selected;
// last_failure_message and last_failure_status_code can carry provider text and
// are never read. last_failure_terminal is not read either: the failed_terminal
// status is the terminal truth, and the scanner worker dead-letters to
// failed_terminal without setting that flag.
//
// Suppression runs here, in SQL, never on a single returned row: a Go-side
// filter on one LIMIT 1 row would hide an older unacknowledged failure on
// another scope of the same instance. workflowTerminalFailureAckCovers is the
// one definition of "an acknowledgement covers t". The u lateral keeps the
// failures it does not cover. The scope filter in front of its cover probe
// (t.scope_id <> ALL(acknowledged scopes of the instance)) lets a row on an
// unacknowledged scope skip the probe, and keeps the planner from running the
// completed probe on a covered row before rejecting it: measured on 542,880
// rows, that reorder is the difference between 2.9 ms and 4.0 ms with the hot
// scope acknowledged. The a lateral returns the newest covered failure and is
// skipped by a One-Time Filter for an instance with no acknowledgement. Its
// acknowledged_at comes from a LIMIT 1 lateral on the primary key.
const workflowCollectorTerminalFailuresQuery = `
SELECT
    i.collector_kind,
    i.instance_id,
    COALESCE(f.last_failure_class, ''),
    COALESCE(f.last_failure_operation, ''),
    f.updated_at,
    f.scope_id,
    f.work_item_id,
    f.acknowledged,
    f.acknowledged_at
FROM unnest($1::text[], $2::text[], $3::timestamptz[]) AS i(collector_kind, instance_id, cutoff)
CROSS JOIN LATERAL (
    (
        SELECT
            t.last_failure_class,
            t.last_failure_operation,
            t.updated_at,
            t.scope_id,
            t.work_item_id,
            FALSE AS acknowledged,
            NULL::timestamptz AS acknowledged_at
        FROM workflow_work_items AS t
        WHERE t.collector_kind = i.collector_kind
          AND t.collector_instance_id = i.instance_id
          AND t.status = 'failed_terminal'
          AND t.updated_at >= i.cutoff
          AND (
              t.scope_id <> ALL (` + workflowTerminalFailureAckScopes + `)
              OR NOT EXISTS (
                  SELECT 1
                  FROM collector_terminal_failure_acknowledgements AS k
                  WHERE ` + workflowTerminalFailureAckCovers + `
              )
          )
          AND NOT EXISTS (
              SELECT 1
              FROM workflow_work_items AS s
              WHERE ` + workflowTerminalFailureResolvedBy + `
          )
        ORDER BY t.updated_at DESC, t.work_item_id ASC
        LIMIT 1
    )
    UNION ALL
    (
        SELECT
            t.last_failure_class,
            t.last_failure_operation,
            t.updated_at,
            t.scope_id,
            t.work_item_id,
            TRUE AS acknowledged,
            k.acknowledged_at
        FROM workflow_work_items AS t
        JOIN LATERAL (
            SELECT k.acknowledged_at
            FROM collector_terminal_failure_acknowledgements AS k
            WHERE ` + workflowTerminalFailureAckCovers + `
            LIMIT 1
        ) AS k ON TRUE
        WHERE EXISTS (
              SELECT 1
              FROM collector_terminal_failure_acknowledgements AS a
              WHERE a.collector_kind = i.collector_kind
                AND a.collector_instance_id = i.instance_id
          )
          AND t.collector_kind = i.collector_kind
          AND t.collector_instance_id = i.instance_id
          AND t.status = 'failed_terminal'
          AND t.updated_at >= i.cutoff
          AND NOT EXISTS (
              SELECT 1
              FROM workflow_work_items AS s
              WHERE ` + workflowTerminalFailureResolvedBy + `
          )
        ORDER BY t.updated_at DESC, t.work_item_id ASC
        LIMIT 1
    )
) AS f
ORDER BY i.collector_kind ASC, i.instance_id ASC, f.acknowledged ASC
`

// workflowTerminalFailureResolvedBy is the one definition of "a later completed
// work item of the same target resolved failed_terminal row t": a row s of the
// same collector kind, instance, and scope that is completed and newer than t.
// Only a completed row proves recovery. The status read, the acknowledgement
// target read, and the failure list share it, so what the list shows is what
// status counts. Callers wrap it as NOT EXISTS (SELECT 1 FROM workflow_work_items
// AS s WHERE ...) over t; the list adds a cutoff bound of its own.
const workflowTerminalFailureResolvedBy = `s.collector_kind = t.collector_kind
                AND s.collector_instance_id = t.collector_instance_id
                AND s.scope_id = t.scope_id
                AND s.status = 'completed'
                AND s.updated_at > t.updated_at`

// workflowTerminalFailureAckScopes is the scope ids that carry an
// acknowledgement for the instance bound to i. It is a cheap pre-filter in front
// of workflowTerminalFailureAckCovers, not part of the rule.
const workflowTerminalFailureAckScopes = `ARRAY(
                  SELECT a.scope_id
                  FROM collector_terminal_failure_acknowledgements AS a
                  WHERE a.collector_kind = i.collector_kind
                    AND a.collector_instance_id = i.instance_id
              )`

// workflowTerminalFailureAckCovers is the join condition between a
// failed_terminal row t and an acknowledgement k (#8830): same kind, instance,
// and scope, and either t is not newer than the acknowledged failure
// (k.failure_at, the updated_at of the row the operator acknowledged), or t has
// the acknowledged raw class and no completed row of the target landed between
// the acknowledged failure and t.
//
// The first arm keeps older rows of the acknowledged scope from surfacing once
// the newer ones are suppressed. The second arm is the episode: the same cause
// keeps failing every cycle and stays covered, a completed row in between is a
// recovery so the next failure is new, and a different raw class is a different
// cause. The class compare is on the raw persisted token (NULL and ” agree),
// so a free-text class that varies per cycle lapses the acknowledgement; that
// class already reads "unknown" and bounding it here would duplicate
// collector.BoundedFailureClassForInstance. The predicate reads only updated_at
// values written by one clock family, never now().
const workflowTerminalFailureAckCovers = `k.collector_kind = t.collector_kind
                AND k.collector_instance_id = t.collector_instance_id
                AND k.scope_id = t.scope_id
                AND (
                    t.updated_at <= k.failure_at
                    OR (
                        t.last_failure_class IS NOT DISTINCT FROM NULLIF(k.failure_class, '')
                        AND NOT EXISTS (
                            SELECT 1
                            FROM workflow_work_items AS c
                            WHERE c.collector_kind = t.collector_kind
                              AND c.collector_instance_id = t.collector_instance_id
                              AND c.scope_id = t.scope_id
                              AND c.status = 'completed'
                              AND k.failure_at < c.updated_at
                              AND c.updated_at < t.updated_at
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
			&failure.ScopeID,
			&failure.WorkItemID,
			&failure.Acknowledged,
			&acknowledgedAt,
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
