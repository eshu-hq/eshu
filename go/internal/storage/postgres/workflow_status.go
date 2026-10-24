// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
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
    deactivated_at,
    (configuration->'scan_interval')::text
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
	workflowCollectorTerminalFailuresQuery = `
SELECT
    i.collector_kind,
    i.instance_id,
    COALESCE(f.last_failure_class, ''),
    COALESCE(f.last_failure_operation, ''),
    f.updated_at
FROM unnest($1::text[], $2::text[], $3::timestamptz[]) AS i(collector_kind, instance_id, cutoff)
CROSS JOIN LATERAL (
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
    ORDER BY t.updated_at DESC, t.work_item_id ASC
    LIMIT 1
) AS f
ORDER BY i.collector_kind ASC, i.instance_id ASC
`
)

// defaultCoordinatorRecentFailureWindow bounds how far back a workflow failure
// counts toward the degraded health state. Failures older than this window are
// treated as aged history and surfaced only as cumulative detail.
const defaultCoordinatorRecentFailureWindow = 30 * time.Minute

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
	terminalFailures, err := readWorkflowCollectorTerminalFailures(ctx, queryer, instances, asOf)
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
// work-item failure inside each enabled, non-deactivated instance's window.
// The cutoffs derive from the same asOf as the rest of the snapshot, so the
// instance list and the failure read agree on one clock. No statement runs
// when no instance is eligible. The result carries only bounded class and
// operation tokens; the status layer re-validates them before they reach a
// payload.
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
		if err := rows.Scan(
			&failure.CollectorKind,
			&failure.InstanceID,
			&failure.FailureClass,
			&failure.FailureOperation,
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
	var malformedScanIntervals []string
	for rows.Next() {
		var instance collector.InstanceSummary
		var deactivatedAt sql.NullTime
		var scanInterval sql.NullString
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
			&scanInterval,
		); err != nil {
			return nil, fmt.Errorf("list coordinator collector instances: %w", err)
		}
		if deactivatedAt.Valid {
			instance.DeactivatedAt = deactivatedAt.Time.UTC()
		}
		if scanInterval.Valid {
			// The coordinator rejects a malformed value at startup; here it
			// only sizes a status window, so it falls back to the floor.
			interval, set, err := schedule.ParseScanIntervalValue(json.RawMessage(scanInterval.String))
			switch {
			case err != nil:
				malformedScanIntervals = append(malformedScanIntervals, instance.InstanceID)
			case set:
				instance.ScanInterval = interval
			}
		}
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list coordinator collector instances: %w", err)
	}
	if len(malformedScanIntervals) > 0 {
		slog.DebugContext(ctx, "collector scan_interval unreadable; terminal failure window uses the floor",
			slog.Int("malformed_scan_interval_instances", len(malformedScanIntervals)),
			slog.Any("collector_instance_ids", malformedScanIntervals),
			slog.Duration("window_floor", defaultCoordinatorRecentFailureWindow))
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
