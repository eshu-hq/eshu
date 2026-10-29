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
	"github.com/eshu-hq/eshu/go/internal/status/collector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Each lateral arm is keyed by an enabled, registered extension instance.
// The outcome arm includes a withheld partial's fenced retry row so an older
// clean completion cannot make an incomplete latest attempt look healthy.
const workflowExtensionOutcomesQuery = `
WITH requested AS (
    SELECT DISTINCT collector_kind, collector_instance_id
    FROM unnest($1::text[], $2::text[]) AS instance(collector_kind, collector_instance_id)
)
SELECT requested.collector_kind, requested.collector_instance_id,
       completed.scope_count, completed.partial_scopes,
       completed.warning_count, active.in_flight_scopes,
       GREATEST(completed.updated_at, active.updated_at),
       committed.last_committed_at
FROM requested
LEFT JOIN LATERAL (
    SELECT COUNT(*) AS scope_count,
           COUNT(*) FILTER (WHERE extension_partial IS TRUE) AS partial_scopes,
           COALESCE(SUM(extension_warning_count), 0) AS warning_count,
           MAX(updated_at) AS updated_at
    FROM (
        SELECT DISTINCT ON (scope_id)
            scope_id, extension_partial, extension_warning_count, updated_at
        FROM (
            SELECT scope_id, extension_partial, extension_warning_count,
                   updated_at, work_item_id
            FROM workflow_work_items
            WHERE collector_kind = requested.collector_kind
              AND collector_instance_id = requested.collector_instance_id
              AND status = 'completed'
              AND extension_result_state IS NOT NULL
            UNION ALL
            SELECT scope_id, extension_partial, extension_warning_count,
                   updated_at, work_item_id
            FROM workflow_work_items
            WHERE collector_kind = requested.collector_kind
              AND collector_instance_id = requested.collector_instance_id
              AND status IN ('pending', 'claimed', 'failed_terminal')
              AND extension_result_state = 'partial_withheld'
        ) candidates
        ORDER BY scope_id, updated_at DESC, work_item_id DESC
    ) latest
) completed ON TRUE
LEFT JOIN LATERAL (
    SELECT COUNT(DISTINCT scope_id) AS in_flight_scopes,
           MAX(updated_at) AS updated_at
    FROM workflow_work_items
    WHERE collector_kind = requested.collector_kind
      AND collector_instance_id = requested.collector_instance_id
      AND status = 'claimed'
) active ON TRUE
LEFT JOIN LATERAL (
    SELECT MAX(updated_at) AS last_committed_at
    FROM workflow_work_items
    WHERE collector_kind = requested.collector_kind
      AND collector_instance_id = requested.collector_instance_id
      AND status = 'completed'
      AND extension_result_state IS NOT NULL
) committed ON TRUE
WHERE completed.scope_count > 0 OR active.in_flight_scopes > 0
ORDER BY requested.collector_kind, requested.collector_instance_id
`

func readWorkflowExtensionOutcomes(ctx context.Context, queryer db.Queryer, instances []collector.InstanceSummary) ([]collector.ExtensionOutcome, error) {
	kinds := make([]string, 0, len(instances))
	ids := make([]string, 0, len(instances))
	for _, instance := range instances {
		if !instance.ExtensionHosted || !instance.Enabled || !instance.DeactivatedAt.IsZero() {
			continue
		}
		kind := strings.TrimSpace(instance.CollectorKind)
		id := strings.TrimSpace(instance.InstanceID)
		if kind == "" || id == "" {
			continue
		}
		kinds = append(kinds, kind)
		ids = append(ids, id)
	}
	rows, err := queryer.QueryContext(ctx, workflowExtensionOutcomesQuery, kinds, ids)
	if err != nil {
		return nil, fmt.Errorf("read extension claim outcomes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var outcomes []collector.ExtensionOutcome
	for rows.Next() {
		var row collector.ExtensionOutcome
		var scopes, partial, warnings, inFlight int64
		var updated time.Time
		var committed sql.NullTime
		if err := rows.Scan(&row.CollectorKind, &row.InstanceID, &scopes, &partial, &warnings, &inFlight, &updated, &committed); err != nil {
			return nil, fmt.Errorf("scan extension claim outcome: %w", err)
		}
		row.ScopeCount = int(scopes)
		row.PartialScopes = int(partial)
		row.WarningCount = int(warnings)
		row.InFlightScopes = int(inFlight)
		row.UpdatedAt = updated.UTC()
		if committed.Valid {
			row.LastCommittedAt = committed.Time.UTC()
		}
		outcomes = append(outcomes, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read extension claim outcomes: %w", err)
	}
	return outcomes, nil
}

func (s StatusStore) readCollectorEvidence(ctx context.Context, include bool, coordinator *statuspkg.CoordinatorSnapshot) ([]collector.FactEvidence, []collector.ExtensionOutcome, error) {
	if !include {
		return nil, nil, nil
	}
	q, done := s.read(ctx, statusReadCollectorFactEvidence)
	facts, err := readCollectorFactEvidence(ctx, q)
	if err = done(err); err != nil {
		return nil, nil, err
	}
	q, done = s.read(ctx, statusReadExtensionOutcomes)
	var instances []collector.InstanceSummary
	if coordinator != nil {
		instances = coordinator.CollectorInstances
	}
	outcomes, err := readWorkflowExtensionOutcomes(ctx, q, instances)
	if err = done(err); err != nil {
		return nil, nil, err
	}
	return facts, outcomes, nil
}
