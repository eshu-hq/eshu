// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// CrossplaneRedriveClaim identifies one XRD source-generation whose
// cross-scope Claim re-drive sweep has just been claimed by this process.
type CrossplaneRedriveClaim struct {
	XRDScopeID      string
	XRDGenerationID string
}

const ensureCrossplaneRedriveQueuedQuery = `
INSERT INTO crossplane_satisfied_by_redrive_state (xrd_scope_id, xrd_generation_id, status, updated_at)
VALUES ($1, $2, 'queued', $3)
ON CONFLICT (xrd_scope_id, xrd_generation_id) DO NOTHING
`

// claimCrossplaneRedriveExactQuery claims exactly one (xrd_scope_id,
// xrd_generation_id) row when it is either freshly queued or its previous
// claim's lease has expired (a crash mid-sweep never blocks a retry forever).
// FOR UPDATE SKIP LOCKED lets a concurrent claimant (the live post-activation
// trigger racing a startup catch-up sweep) skip a row another transaction
// already holds instead of blocking on it.
const claimCrossplaneRedriveExactQuery = `
WITH claimable AS (
    SELECT xrd_scope_id, xrd_generation_id
    FROM crossplane_satisfied_by_redrive_state
    WHERE xrd_scope_id = $1
      AND xrd_generation_id = $2
      AND (status = 'queued' OR (status = 'claimed' AND claim_expires_at < $3))
    FOR UPDATE SKIP LOCKED
)
UPDATE crossplane_satisfied_by_redrive_state AS state
SET status = 'claimed',
    claimed_by = $4,
    claimed_at = $3,
    claim_expires_at = $5,
    updated_at = $3
FROM claimable
WHERE state.xrd_scope_id = claimable.xrd_scope_id
  AND state.xrd_generation_id = claimable.xrd_generation_id
RETURNING state.xrd_scope_id, state.xrd_generation_id
`

// claimCrossplaneRedriveBatchQuery claims up to $2 rows that are 'queued' or
// whose 'claimed' lease has expired, ordered oldest-first so a long-stuck row
// cannot starve behind a stream of freshly queued ones. Used by the
// startup/periodic catch-up sweep, which does not know a specific XRD
// generation in advance and must recover from any live-trigger miss or crash.
const claimCrossplaneRedriveBatchQuery = `
WITH claimable AS (
    SELECT xrd_scope_id, xrd_generation_id
    FROM crossplane_satisfied_by_redrive_state
    WHERE status = 'queued' OR (status = 'claimed' AND claim_expires_at < $1)
    ORDER BY updated_at ASC, xrd_scope_id ASC, xrd_generation_id ASC
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
UPDATE crossplane_satisfied_by_redrive_state AS state
SET status = 'claimed',
    claimed_by = $3,
    claimed_at = $1,
    claim_expires_at = $4,
    updated_at = $1
FROM claimable
WHERE state.xrd_scope_id = claimable.xrd_scope_id
  AND state.xrd_generation_id = claimable.xrd_generation_id
RETURNING state.xrd_scope_id, state.xrd_generation_id
`

// markCrossplaneRedriveCompletedQuery records durable sweep completion,
// fenced by (status='claimed' AND claimed_by=$4): a stale claimant whose
// lease already expired and was reclaimed by a different owner affects zero
// rows here, so its (safe, idempotent, but now-redundant) completion never
// overwrites the reclaiming owner's outcome.
const markCrossplaneRedriveCompletedQuery = `
UPDATE crossplane_satisfied_by_redrive_state
SET status = 'completed',
    completed_at = $1,
    updated_at = $1
WHERE xrd_scope_id = $2
  AND xrd_generation_id = $3
  AND status = 'claimed'
  AND claimed_by = $4
`

// CrossplaneRedriveStateStore persists the durable claim/completion state for
// the Crossplane cross-scope SATISFIED_BY re-drive sweep (issue #5476).
type CrossplaneRedriveStateStore struct {
	db  ExecQueryer
	Now func() time.Time
}

// NewCrossplaneRedriveStateStore constructs the redrive state store.
func NewCrossplaneRedriveStateStore(db ExecQueryer) CrossplaneRedriveStateStore {
	return CrossplaneRedriveStateStore{db: db}
}

func (s CrossplaneRedriveStateStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// EnsureQueued durably records that an XRD source-generation needs a
// cross-scope Claim re-drive sweep. Idempotent: a generation already tracked
// (queued, claimed, or completed) is left untouched.
func (s CrossplaneRedriveStateStore) EnsureQueued(ctx context.Context, xrdScopeID, xrdGenerationID string) error {
	if s.db == nil {
		return errors.New("crossplane redrive state database is required")
	}
	if xrdScopeID == "" || xrdGenerationID == "" {
		return errors.New("crossplane redrive state requires xrd scope id and generation id")
	}
	if _, err := s.db.ExecContext(ctx, ensureCrossplaneRedriveQueuedQuery, xrdScopeID, xrdGenerationID, s.now()); err != nil {
		return fmt.Errorf("ensure crossplane redrive queued: %w", err)
	}
	return nil
}

// ClaimExact attempts to claim exactly one XRD generation's sweep. ok is
// false when the row is already claimed by a live (non-expired) owner, is
// already completed, or does not exist (EnsureQueued was never called).
func (s CrossplaneRedriveStateStore) ClaimExact(
	ctx context.Context,
	xrdScopeID, xrdGenerationID, owner string,
	leaseDuration time.Duration,
) (bool, error) {
	if s.db == nil {
		return false, errors.New("crossplane redrive state database is required")
	}
	if owner == "" {
		return false, errors.New("crossplane redrive claim owner is required")
	}
	if leaseDuration <= 0 {
		return false, errors.New("crossplane redrive claim lease duration must be positive")
	}
	now := s.now()
	rows, err := s.db.QueryContext(ctx, claimCrossplaneRedriveExactQuery,
		xrdScopeID, xrdGenerationID, now, owner, now.Add(leaseDuration))
	if err != nil {
		return false, fmt.Errorf("claim crossplane redrive: %w", err)
	}
	defer func() { _ = rows.Close() }()
	claimed := rows.Next()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("claim crossplane redrive: %w", err)
	}
	return claimed, nil
}

// ClaimBatch claims up to limit stale (queued or lease-expired) XRD
// generation sweeps for the startup/periodic catch-up path.
func (s CrossplaneRedriveStateStore) ClaimBatch(
	ctx context.Context,
	owner string,
	leaseDuration time.Duration,
	limit int,
) ([]CrossplaneRedriveClaim, error) {
	if s.db == nil {
		return nil, errors.New("crossplane redrive state database is required")
	}
	if owner == "" {
		return nil, errors.New("crossplane redrive claim owner is required")
	}
	if leaseDuration <= 0 {
		return nil, errors.New("crossplane redrive claim lease duration must be positive")
	}
	if limit <= 0 {
		return nil, errors.New("crossplane redrive claim batch limit must be positive")
	}
	now := s.now()
	rows, err := s.db.QueryContext(ctx, claimCrossplaneRedriveBatchQuery,
		now, limit, owner, now.Add(leaseDuration))
	if err != nil {
		return nil, fmt.Errorf("claim crossplane redrive batch: %w", err)
	}
	defer func() { _ = rows.Close() }()

	claims := make([]CrossplaneRedriveClaim, 0, limit)
	for rows.Next() {
		var claim CrossplaneRedriveClaim
		if err := rows.Scan(&claim.XRDScopeID, &claim.XRDGenerationID); err != nil {
			return nil, fmt.Errorf("scan crossplane redrive claim: %w", err)
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim crossplane redrive batch: %w", err)
	}
	return claims, nil
}

// MarkCompleted records durable sweep completion for one XRD generation,
// fenced to the calling owner's still-live claim. ok is false when a
// different owner has since reclaimed the row (this owner's lease expired
// mid-sweep) -- the caller's now-redundant completion is a safe no-op.
func (s CrossplaneRedriveStateStore) MarkCompleted(
	ctx context.Context,
	xrdScopeID, xrdGenerationID, owner string,
) (bool, error) {
	if s.db == nil {
		return false, errors.New("crossplane redrive state database is required")
	}
	if owner == "" {
		return false, errors.New("crossplane redrive claim owner is required")
	}
	result, err := s.db.ExecContext(ctx, markCrossplaneRedriveCompletedQuery, s.now(), xrdScopeID, xrdGenerationID, owner)
	if err != nil {
		return false, fmt.Errorf("mark crossplane redrive completed: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("mark crossplane redrive completed: rows affected: %w", err)
	}
	return rowsAffected > 0, nil
}
