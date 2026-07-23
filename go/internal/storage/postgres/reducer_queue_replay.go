// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const reopenSucceededReducerWorkQuery = `
UPDATE fact_work_items
SET status = 'pending',
    attempt_count = 0,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $1,
    next_attempt_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE work_item_id = $2
  AND stage = 'reducer'
  AND status = 'succeeded'
`

const replaySucceededReducerDomainQuery = `
UPDATE fact_work_items
SET status = 'pending',
    attempt_count = 0,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $1,
    next_attempt_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE scope_id = $2
  AND generation_id = $3
  AND domain = $4
  AND stage = 'reducer'
  AND status = 'succeeded'
`

const countInFlightReducerWorkByDomainQuery = `
SELECT COUNT(*)
FROM fact_work_items
WHERE stage = 'reducer'
  AND domain = $1
  AND status NOT IN ('succeeded', 'dead_letter')
`

const (
	workloadMaterializationReplayReason = "deployment mapping resolved stronger evidence"
	// crossplaneSatisfiedByRedriveReplayReason tags a re-drive-triggered
	// SATISFIED_BY replay (issue #5476) distinctly from the original
	// projector-triggered enqueue, so a completion log or dead-letter can
	// tell the two apart.
	crossplaneSatisfiedByRedriveReplayReason = "cross-scope crossplane xrd redrive"
)

// ReopenSucceeded moves one succeeded reducer work item back to pending so it
// can be replayed through the normal reducer claim path. The returned boolean
// reports whether a succeeded row was actually transitioned.
func (q ReducerQueue) ReopenSucceeded(
	ctx context.Context,
	workItemID string,
) (bool, error) {
	if err := q.validateDB(); err != nil {
		return false, err
	}
	if strings.TrimSpace(workItemID) == "" {
		return false, errors.New("reducer work item id is required")
	}

	result, err := q.db.ExecContext(ctx, reopenSucceededReducerWorkQuery, q.now(), workItemID)
	if err != nil {
		return false, fmt.Errorf("reopen succeeded reducer work: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("reopen succeeded reducer work: rows affected: %w", err)
	}

	return rowsAffected > 0, nil
}

// ReplayDomain moves succeeded reducer work items for one scope-generation
// domain back to pending so they can be replayed through the normal claim path.
func (q ReducerQueue) ReplayDomain(
	ctx context.Context,
	scopeID string,
	generationID string,
	domain reducer.Domain,
) (bool, error) {
	if err := q.validateDB(); err != nil {
		return false, err
	}
	if strings.TrimSpace(scopeID) == "" {
		return false, errors.New("reducer replay scope id is required")
	}
	if strings.TrimSpace(generationID) == "" {
		return false, errors.New("reducer replay generation id is required")
	}
	if err := domain.Validate(); err != nil {
		return false, fmt.Errorf("reducer replay domain: %w", err)
	}

	result, err := q.db.ExecContext(
		ctx,
		replaySucceededReducerDomainQuery,
		q.now(),
		scopeID,
		generationID,
		string(domain),
	)
	if err != nil {
		return false, fmt.Errorf("replay succeeded reducer domain: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("replay succeeded reducer domain: rows affected: %w", err)
	}

	return rowsAffected > 0, nil
}

// ReplayWorkloadMaterialization replays the canonical workload materialization
// intent(s) for one scope generation after stronger deployment evidence lands.
func (q ReducerQueue) ReplayWorkloadMaterialization(
	ctx context.Context,
	scopeID string,
	generationID string,
	entityKey string,
) (bool, error) {
	// Replay is enqueue-only: it opportunistically reopens a succeeded row via
	// ReopenSucceeded (which runs its own validateDB) and otherwise enqueues a
	// fresh intent through enqueueReducerBatch. No lease fields are read on
	// this path, so use the enqueue-side check rather than demanding
	// LeaseOwner/LeaseDuration the call does not need.
	if err := q.validateEnqueue(); err != nil {
		return false, err
	}
	if strings.TrimSpace(entityKey) == "" {
		return false, errors.New("workload materialization replay entity key is required")
	}

	intent := projector.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainWorkloadMaterialization,
		EntityKey:    entityKey,
		Reason:       workloadMaterializationReplayReason,
		SourceSystem: "reducer",
	}
	workItemID := reducerWorkItemID(intent)

	reopened, err := q.ReopenSucceeded(ctx, workItemID)
	if err != nil {
		return false, fmt.Errorf("schedule workload materialization replay: %w", err)
	}
	if reopened {
		return true, nil
	}
	if err := q.enqueueReducerBatch(ctx, []projector.ReducerIntent{intent}, q.now()); err != nil {
		return false, fmt.Errorf("schedule workload materialization replay: %w", err)
	}

	return true, nil
}

// ReplayCrossplaneSatisfiedByMaterialization re-drives one target Claim
// scope's SATISFIED_BY materialization intent after a cross-scope XRD
// generation activates (issue #5476). Mirrors ReplayWorkloadMaterialization
// exactly: enqueue-only, opportunistically reopening a succeeded row so a
// previously-resolved-with-zero-XRD-visible generation gets a fresh pass, and
// otherwise enqueueing a fresh intent through the normal claim path. Reusing
// the SAME per-scope EntityKey the projector's own trigger uses
// ("crossplane_satisfied_by_materialization:<scopeID>") means the underlying
// work_item_id is identical to the one the projector would have enqueued for
// this exact scope generation, so this call is naturally idempotent under
// concurrent or repeated sweep invocations: ON CONFLICT DO NOTHING (fresh
// enqueue) or a no-op ReopenSucceeded (already pending/claimed/running) never
// duplicates work.
func (q ReducerQueue) ReplayCrossplaneSatisfiedByMaterialization(
	ctx context.Context,
	targetScopeID string,
	targetGenerationID string,
) (bool, error) {
	if err := q.validateEnqueue(); err != nil {
		return false, err
	}
	if strings.TrimSpace(targetScopeID) == "" {
		return false, errors.New("crossplane satisfied-by redrive target scope id is required")
	}
	if strings.TrimSpace(targetGenerationID) == "" {
		return false, errors.New("crossplane satisfied-by redrive target generation id is required")
	}

	intent := projector.ReducerIntent{
		ScopeID:      targetScopeID,
		GenerationID: targetGenerationID,
		Domain:       reducer.DomainCrossplaneSatisfiedByMaterialization,
		EntityKey:    "crossplane_satisfied_by_materialization:" + targetScopeID,
		Reason:       crossplaneSatisfiedByRedriveReplayReason,
		SourceSystem: "reducer",
	}
	workItemID := reducerWorkItemID(intent)

	reopened, err := q.ReopenSucceeded(ctx, workItemID)
	if err != nil {
		return false, fmt.Errorf("schedule crossplane satisfied-by redrive replay: %w", err)
	}
	if reopened {
		return true, nil
	}
	if err := q.enqueueReducerBatch(ctx, []projector.ReducerIntent{intent}, q.now()); err != nil {
		return false, fmt.Errorf("schedule crossplane satisfied-by redrive replay: %w", err)
	}

	return true, nil
}

// CountInFlightByDomain returns the number of reducer work items for one domain
// that have not yet reached a terminal status.
func (q ReducerQueue) CountInFlightByDomain(
	ctx context.Context,
	domain reducer.Domain,
) (int, error) {
	if err := q.validateDB(); err != nil {
		return 0, err
	}
	if err := domain.Validate(); err != nil {
		return 0, fmt.Errorf("count in-flight reducer work: %w", err)
	}

	rows, err := q.db.QueryContext(
		ctx,
		countInFlightReducerWorkByDomainQuery,
		string(domain),
	)
	if err != nil {
		return 0, fmt.Errorf("count in-flight reducer work: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("count in-flight reducer work: %w", err)
		}
		return 0, errors.New("count in-flight reducer work: missing count row")
	}

	var count int
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("count in-flight reducer work: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("count in-flight reducer work: %w", err)
	}

	return count, nil
}

func (q ReducerQueue) validateDB() error {
	if q.db == nil {
		return errors.New("reducer queue database is required")
	}

	return nil
}
