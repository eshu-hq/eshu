package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

const replayFailedWorkItemsQuery = `
WITH replayed AS (
    UPDATE fact_work_items
    SET status = 'pending',
        attempt_count = 0,
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = $1,
        next_attempt_at = NULL,
        failure_class = NULL,
        failure_message = NULL,
        failure_details = NULL,
        updated_at = $1
    WHERE status IN ('dead_letter', 'failed')
      AND stage = $2
    RETURNING work_item_id
)
SELECT work_item_id FROM replayed ORDER BY work_item_id
`

const replayFailedWorkItemsByScopeQuery = `
WITH replayed AS (
    UPDATE fact_work_items
    SET status = 'pending',
        attempt_count = 0,
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = $1,
        next_attempt_at = NULL,
        failure_class = NULL,
        failure_message = NULL,
        failure_details = NULL,
        updated_at = $1
    WHERE status IN ('dead_letter', 'failed')
      AND stage = $2
      AND scope_id = ANY($3)
    RETURNING work_item_id
)
SELECT work_item_id FROM replayed ORDER BY work_item_id
`

const replayFailedWorkItemsByClassQuery = `
WITH replayed AS (
    UPDATE fact_work_items
    SET status = 'pending',
        attempt_count = 0,
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = $1,
        next_attempt_at = NULL,
        failure_class = NULL,
        failure_message = NULL,
        failure_details = NULL,
        updated_at = $1
    WHERE status IN ('dead_letter', 'failed')
      AND stage = $2
      AND failure_class = $3
    RETURNING work_item_id
)
SELECT work_item_id FROM replayed ORDER BY work_item_id
`

const replayFailedWorkItemsByScopeAndClassQuery = `
WITH replayed AS (
    UPDATE fact_work_items
    SET status = 'pending',
        attempt_count = 0,
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = $1,
        next_attempt_at = NULL,
        failure_class = NULL,
        failure_message = NULL,
        failure_details = NULL,
        updated_at = $1
    WHERE status IN ('dead_letter', 'failed')
      AND stage = $2
      AND scope_id = ANY($3)
      AND failure_class = $4
    RETURNING work_item_id
)
SELECT work_item_id FROM replayed ORDER BY work_item_id
`

const refinalizeScopeProjectionsQuery = `
INSERT INTO fact_work_items (
    work_item_id,
    scope_id,
    generation_id,
    stage,
    domain,
    status,
    attempt_count,
    lease_owner,
    claim_until,
    visible_at,
    last_attempt_at,
    next_attempt_at,
    failure_class,
    failure_message,
    failure_details,
    payload,
    created_at,
    updated_at
)
SELECT
    'refinalize_' || scope.scope_id || '_' || scope.active_generation_id,
    scope.scope_id,
    scope.active_generation_id,
    'projector',
    'source_local',
    'pending',
    0,
    NULL,
    NULL,
    $1,
    NULL,
    NULL,
    NULL,
    NULL,
    NULL,
    '{}'::jsonb,
    $1,
    $1
FROM ingestion_scopes AS scope
WHERE scope.scope_id = ANY($2)
  AND scope.active_generation_id IS NOT NULL
  AND scope.status = 'active'
ON CONFLICT (work_item_id) DO UPDATE
SET status = 'pending',
    attempt_count = 0,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = EXCLUDED.visible_at,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL,
    updated_at = EXCLUDED.updated_at
RETURNING scope_id
`

// RecoveryStore implements recovery.ReplayStore over Postgres.
type RecoveryStore struct {
	db ExecQueryer
}

// NewRecoveryStore constructs a Postgres-backed recovery store.
func NewRecoveryStore(db ExecQueryer) RecoveryStore {
	return RecoveryStore{db: db}
}

// ReplayFailedWorkItems resets terminal work items to pending for the given
// stage and filter criteria. New Go runtime rows use dead_letter; legacy failed
// rows remain replayable until they age out.
func (s RecoveryStore) ReplayFailedWorkItems(
	ctx context.Context,
	filter recovery.ReplayFilter,
	now time.Time,
) (recovery.ReplayResult, error) {
	if s.db == nil {
		return recovery.ReplayResult{}, fmt.Errorf("recovery store database is required")
	}

	stage := string(filter.Stage)
	hasScopeIDs := len(filter.ScopeIDs) > 0
	hasFailureClass := strings.TrimSpace(filter.FailureClass) != ""

	var query string
	var args []any

	switch {
	case hasScopeIDs && hasFailureClass:
		query = replayFailedWorkItemsByScopeAndClassQuery
		args = []any{now.UTC(), stage, filter.ScopeIDs, filter.FailureClass}
	case hasScopeIDs:
		query = replayFailedWorkItemsByScopeQuery
		args = []any{now.UTC(), stage, filter.ScopeIDs}
	case hasFailureClass:
		query = replayFailedWorkItemsByClassQuery
		args = []any{now.UTC(), stage, filter.FailureClass}
	default:
		query = replayFailedWorkItemsQuery
		args = []any{now.UTC(), stage}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return recovery.ReplayResult{}, fmt.Errorf("replay failed work items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var workItemIDs []string
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			return recovery.ReplayResult{}, fmt.Errorf("replay failed work items: %w", scanErr)
		}
		if filter.Limit > 0 && len(workItemIDs) >= filter.Limit {
			break
		}
		workItemIDs = append(workItemIDs, id)
	}
	if err := rows.Err(); err != nil {
		return recovery.ReplayResult{}, fmt.Errorf("replay failed work items: %w", err)
	}

	return recovery.ReplayResult{
		Stage:       filter.Stage,
		Replayed:    len(workItemIDs),
		WorkItemIDs: workItemIDs,
	}, nil
}

// RefinalizeScopeProjections re-enqueues projector work for the given scope
// IDs by inserting new pending work items for their active generations.
func (s RecoveryStore) RefinalizeScopeProjections(
	ctx context.Context,
	filter recovery.RefinalizeFilter,
	now time.Time,
) (recovery.RefinalizeResult, error) {
	if s.db == nil {
		return recovery.RefinalizeResult{}, fmt.Errorf("recovery store database is required")
	}

	rows, err := s.db.QueryContext(ctx, refinalizeScopeProjectionsQuery, now.UTC(), filter.ScopeIDs)
	if err != nil {
		return recovery.RefinalizeResult{}, fmt.Errorf("refinalize scope projections: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var scopeIDs []string
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			return recovery.RefinalizeResult{}, fmt.Errorf("refinalize scope projections: %w", scanErr)
		}
		scopeIDs = append(scopeIDs, id)
	}
	if err := rows.Err(); err != nil {
		return recovery.RefinalizeResult{}, fmt.Errorf("refinalize scope projections: %w", err)
	}

	return recovery.RefinalizeResult{
		Enqueued: len(scopeIDs),
		ScopeIDs: scopeIDs,
	}, nil
}
