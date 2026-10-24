// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"

	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// securityAlertCollectionStateMaxInstances bounds the configuration read. A
// deployment runs a handful of security_alert instances; a read past the bound
// fails with supplychain.ErrSecurityAlertCollectionStateBound rather than
// silently dropping targets, which would misreport target membership.
const securityAlertCollectionStateMaxInstances = 64

// securityAlertCollectionStateMaxTargets bounds the target-state read for the
// same reason: every covering target must be read, or none.
const securityAlertCollectionStateMaxTargets = 1024

// securityAlertCollectorConfigurationsQuery returns the configuration of every
// enabled, non-deactivated security_alert instance. collector_instances holds
// one row per configured instance across all kinds (tens of rows), and the
// predicate matches the configured-collector probe, so a deactivated instance
// never contributes a target.
const securityAlertCollectorConfigurationsQuery = `
SELECT configuration::text
FROM collector_instances
WHERE collector_kind = 'security_alert'
  AND enabled = TRUE
  AND deactivated_at IS NULL
ORDER BY instance_id
LIMIT $1`

// securityAlertTargetStatesQuery reads, per target scope id, the latest
// security_alert work item and whether any committed generation exists.
//
// The latest work item is found by an index-only probe of
// workflow_work_items_security_alert_scope_updated_idx (migration 231:
// scope_id, updated_at DESC, work_item_id ASC, partial on collector_kind =
// 'security_alert'), whose key matches the lateral's ORDER BY, so it reads one
// entry per target however long the target's history is. A primary-key fetch
// then reads that one row. Keep the collector_kind literal: the partial index
// is only usable because the planner can prove it from this predicate.
// Before migration 231 the lateral scanned and top-1 sorted the target's whole
// history on workflow_work_items_collector_scope_generation_updated_idx, where
// generation_id (a hash) sits before updated_at. The committed generation
// check is an index-only EXISTS on scope_generations_scope_idx.
// Evidence: docs/internal/evidence/8803-security-alert-empty-answer-truth.md
// and docs/internal/evidence/8840-security-alert-latest-work-item-index.md.
const securityAlertTargetStatesQuery = `
SELECT t.scope_id,
       COALESCE(w.status, ''),
       COALESCE(w.last_failure_class, ''),
       COALESCE(w.last_failure_terminal, FALSE),
       EXISTS (
           SELECT 1
           FROM scope_generations g
           WHERE g.scope_id = t.scope_id
             AND g.status IN ('active', 'superseded', 'completed')
       ) AS committed
FROM unnest($1::text[]) AS t(scope_id)
LEFT JOIN LATERAL (
    SELECT l.work_item_id
    FROM workflow_work_items l
    WHERE l.collector_kind = 'security_alert'
      AND l.scope_id = t.scope_id
    ORDER BY l.updated_at DESC, l.work_item_id ASC
    LIMIT 1
) latest ON TRUE
LEFT JOIN workflow_work_items w ON w.work_item_id = latest.work_item_id
ORDER BY t.scope_id`

// PostgresSecurityAlertCollectionStateStore reads the security-alert
// collector's configuration and per-target collection state for an empty
// security-alert answer (#8803). It reads collector_instances,
// workflow_work_items, and scope_generations only; it never reads
// last_failure_message or any fact payload.
type PostgresSecurityAlertCollectionStateStore struct {
	reader db.Queryer
}

var _ supplychain.SecurityAlertCollectionStateStore = PostgresSecurityAlertCollectionStateStore{}

// NewPostgresSecurityAlertCollectionStateStoreWithReadStore builds the store
// over a guarded query-only port.
func NewPostgresSecurityAlertCollectionStateStoreWithReadStore(
	reader db.Queryer,
) PostgresSecurityAlertCollectionStateStore {
	return PostgresSecurityAlertCollectionStateStore{reader: reader}
}

// SecurityAlertCollectorConfigurations returns the raw configuration JSON of
// every enabled, non-deactivated security_alert instance.
func (s PostgresSecurityAlertCollectionStateStore) SecurityAlertCollectorConfigurations(
	ctx context.Context,
) ([]string, error) {
	if s.reader == nil {
		return nil, fmt.Errorf("security alert collection state database is required")
	}
	rows, err := s.reader.QueryContext(ctx, securityAlertCollectorConfigurationsQuery, securityAlertCollectionStateMaxInstances+1)
	if err != nil {
		return nil, fmt.Errorf("read security alert collector configurations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	configs := make([]string, 0, 1)
	for rows.Next() {
		var config string
		if err := rows.Scan(&config); err != nil {
			return nil, fmt.Errorf("scan security alert collector configuration: %w", err)
		}
		configs = append(configs, config)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read security alert collector configuration rows: %w", err)
	}
	if len(configs) > securityAlertCollectionStateMaxInstances {
		return nil, fmt.Errorf("security alert collector instances exceed the %d-instance read bound: %w",
			securityAlertCollectionStateMaxInstances, supplychain.ErrSecurityAlertCollectionStateBound)
	}
	return configs, nil
}

// SecurityAlertTargetStates returns the latest work item and committed
// generation existence for each target scope id, in scope id order.
func (s PostgresSecurityAlertCollectionStateStore) SecurityAlertTargetStates(
	ctx context.Context,
	scopeIDs []string,
) ([]supplychain.SecurityAlertTargetState, error) {
	if s.reader == nil {
		return nil, fmt.Errorf("security alert collection state database is required")
	}
	if len(scopeIDs) == 0 {
		return nil, nil
	}
	if len(scopeIDs) > securityAlertCollectionStateMaxTargets {
		return nil, fmt.Errorf("security alert targets exceed the %d-target read bound: %w",
			securityAlertCollectionStateMaxTargets, supplychain.ErrSecurityAlertCollectionStateBound)
	}
	rows, err := s.reader.QueryContext(ctx, securityAlertTargetStatesQuery, array.Of(scopeIDs))
	if err != nil {
		return nil, fmt.Errorf("read security alert target states: %w", err)
	}
	defer func() { _ = rows.Close() }()

	states := make([]supplychain.SecurityAlertTargetState, 0, len(scopeIDs))
	for rows.Next() {
		var state supplychain.SecurityAlertTargetState
		if err := rows.Scan(
			&state.ScopeID,
			&state.LatestWorkItemStatus,
			&state.LatestFailureClass,
			&state.LatestFailureTerminal,
			&state.CommittedGeneration,
		); err != nil {
			return nil, fmt.Errorf("scan security alert target state: %w", err)
		}
		states = append(states, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read security alert target state rows: %w", err)
	}
	return states, nil
}
