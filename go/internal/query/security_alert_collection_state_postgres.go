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
// security_alert work item, whether any committed generation exists, and the
// unread repositories of the target's active generation (#8943).
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
//
// The unread read follows ingestion_scopes (primary key) to the active
// generation, then security_alert_target_read_states (primary key), then the
// unread rows of only the requested repositories through the primary key of
// security_alert_unread_repositories (generation_id, repository). A target
// with no read state (a generation committed before the carrier, or a
// repository-scope target) returns zero and empty arrays. $2 is the requested
// slugs; empty for an unanchored read, which then reads no child row.
// Evidence: docs/internal/evidence/8803-security-alert-empty-answer-truth.md,
// docs/internal/evidence/8840-security-alert-latest-work-item-index.md and
// docs/internal/evidence/8943-security-alert-org-read-state.md.
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
       ) AS committed,
       COALESCE(rs.unread_total, 0),
       COALESCE(rs.unread_reasons, '{}'::text[]),
       COALESCE(unread.repositories, '{}'::text[]),
       COALESCE(unread.reasons, '{}'::text[])
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
LEFT JOIN ingestion_scopes sc ON sc.scope_id = t.scope_id
LEFT JOIN security_alert_target_read_states rs ON rs.generation_id = sc.active_generation_id
LEFT JOIN LATERAL (
    SELECT array_agg(u.repository ORDER BY u.repository) AS repositories,
           array_agg(u.reason ORDER BY u.repository) AS reasons
    FROM security_alert_unread_repositories u
    WHERE u.generation_id = rs.generation_id
      AND u.repository = ANY($2::text[])
) unread ON TRUE
ORDER BY t.scope_id`

// PostgresSecurityAlertCollectionStateStore reads the security-alert
// collector's configuration and per-target collection state for an empty
// security-alert answer (#8803). It reads collector_instances,
// workflow_work_items, scope_generations, ingestion_scopes, and the
// security-alert read-state tables only; it never reads last_failure_message
// or any fact payload.
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

// SecurityAlertTargetStates returns the latest work item, committed
// generation existence, and unread repositories for each target scope id, in
// scope id order. requestedSlugs are the normalized slugs of an anchored read.
func (s PostgresSecurityAlertCollectionStateStore) SecurityAlertTargetStates(
	ctx context.Context,
	scopeIDs []string,
	requestedSlugs []string,
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
	rows, err := s.reader.QueryContext(ctx, securityAlertTargetStatesQuery, array.Of(scopeIDs), array.Of(requestedSlugs))
	if err != nil {
		return nil, fmt.Errorf("read security alert target states: %w", err)
	}
	defer func() { _ = rows.Close() }()

	states := make([]supplychain.SecurityAlertTargetState, 0, len(scopeIDs))
	for rows.Next() {
		var state supplychain.SecurityAlertTargetState
		var unreadReasons, unreadRepositories, unreadRepositoryReasons array.StringArray
		if err := rows.Scan(
			&state.ScopeID,
			&state.LatestWorkItemStatus,
			&state.LatestFailureClass,
			&state.LatestFailureTerminal,
			&state.CommittedGeneration,
			&state.UnreadTotal,
			&unreadReasons,
			&unreadRepositories,
			&unreadRepositoryReasons,
		); err != nil {
			return nil, fmt.Errorf("scan security alert target state: %w", err)
		}
		if len(unreadReasons) > 0 {
			state.UnreadReasons = []string(unreadReasons)
		}
		if len(unreadRepositories) > 0 && len(unreadRepositories) == len(unreadRepositoryReasons) {
			state.UnreadRequested = make(map[string]string, len(unreadRepositories))
			for i, repository := range unreadRepositories {
				state.UnreadRequested[repository] = unreadRepositoryReasons[i]
			}
		}
		states = append(states, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read security alert target state rows: %w", err)
	}
	return states, nil
}
