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
// FencingToken must be presented back to MarkCompleted: it is bumped on
// every claim (fresh or reclaim), so a stale invocation whose lease expired
// and was reclaimed by a DIFFERENT invocation under the same static owner
// string can never complete a claim it no longer holds (see migration 076's
// doc comment for the split-brain this closes).
type CrossplaneRedriveClaim struct {
	XRDScopeID      string
	XRDGenerationID string
	FencingToken    int64
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
// already holds instead of blocking on it. claim_fencing_token is bumped on
// every claim (fresh or reclaim) and returned to the caller, who must present
// it back to MarkCompleted -- see migration 076's doc comment.
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
    claim_fencing_token = state.claim_fencing_token + 1,
    updated_at = $3
FROM claimable
WHERE state.xrd_scope_id = claimable.xrd_scope_id
  AND state.xrd_generation_id = claimable.xrd_generation_id
RETURNING state.xrd_scope_id, state.xrd_generation_id, state.claim_fencing_token
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
    claim_fencing_token = state.claim_fencing_token + 1,
    updated_at = $1
FROM claimable
WHERE state.xrd_scope_id = claimable.xrd_scope_id
  AND state.xrd_generation_id = claimable.xrd_generation_id
RETURNING state.xrd_scope_id, state.xrd_generation_id, state.claim_fencing_token
`

// markCrossplaneRedriveCompletedQuery records durable sweep completion,
// fenced by (status='claimed' AND claim_fencing_token=$4): a stale claimant
// whose lease already expired and was reclaimed -- bumping the token -- by
// ANY invocation (including one sharing the same static owner string)
// affects zero rows here, so its (safe, idempotent, but now-redundant)
// completion never overwrites the reclaiming invocation's outcome. See
// migration 076's doc comment for why claimed_by alone is not a safe fence.
const markCrossplaneRedriveCompletedQuery = `
UPDATE crossplane_satisfied_by_redrive_state
SET status = 'completed',
    completed_at = $1,
    updated_at = $1
WHERE xrd_scope_id = $2
  AND xrd_generation_id = $3
  AND status = 'claimed'
  AND claim_fencing_token = $4
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

// ClaimExact attempts to claim exactly one XRD generation's sweep. claimed is
// false when the row is already claimed by a live (non-expired) owner, is
// already completed, or does not exist (EnsureQueued was never called). On a
// successful claim, fencingToken MUST be presented back to MarkCompleted.
func (s CrossplaneRedriveStateStore) ClaimExact(
	ctx context.Context,
	xrdScopeID, xrdGenerationID, owner string,
	leaseDuration time.Duration,
) (claimed bool, fencingToken int64, err error) {
	if s.db == nil {
		return false, 0, errors.New("crossplane redrive state database is required")
	}
	if owner == "" {
		return false, 0, errors.New("crossplane redrive claim owner is required")
	}
	if leaseDuration <= 0 {
		return false, 0, errors.New("crossplane redrive claim lease duration must be positive")
	}
	now := s.now()
	rows, queryErr := s.db.QueryContext(ctx, claimCrossplaneRedriveExactQuery,
		xrdScopeID, xrdGenerationID, now, owner, now.Add(leaseDuration))
	if queryErr != nil {
		return false, 0, fmt.Errorf("claim crossplane redrive: %w", queryErr)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, 0, fmt.Errorf("claim crossplane redrive: %w", err)
		}
		return false, 0, nil
	}
	var scopeID, generationID string
	if scanErr := rows.Scan(&scopeID, &generationID, &fencingToken); scanErr != nil {
		return false, 0, fmt.Errorf("scan crossplane redrive claim: %w", scanErr)
	}
	return true, fencingToken, nil
}

// ClaimBatch claims up to limit stale (queued or lease-expired) XRD
// generation sweeps for the startup/periodic catch-up path. Each returned
// claim's FencingToken must be presented back to MarkCompleted.
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
		if err := rows.Scan(&claim.XRDScopeID, &claim.XRDGenerationID, &claim.FencingToken); err != nil {
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
// fenced to the fencingToken the caller received from ClaimExact/ClaimBatch.
// ok is false when the claim has since been reclaimed (the token no longer
// matches -- this invocation's lease expired mid-sweep, possibly reclaimed by
// another invocation sharing the same owner string) -- the caller's
// now-redundant completion is a safe no-op.
func (s CrossplaneRedriveStateStore) MarkCompleted(
	ctx context.Context,
	xrdScopeID, xrdGenerationID string,
	fencingToken int64,
) (bool, error) {
	if s.db == nil {
		return false, errors.New("crossplane redrive state database is required")
	}
	result, err := s.db.ExecContext(ctx, markCrossplaneRedriveCompletedQuery, s.now(), xrdScopeID, xrdGenerationID, fencingToken)
	if err != nil {
		return false, fmt.Errorf("mark crossplane redrive completed: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("mark crossplane redrive completed: rows affected: %w", err)
	}
	return rowsAffected > 0, nil
}
