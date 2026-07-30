// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ensureConfigStateDriftRedriveScheduledQuery durably records that a
// config_state_drift work item for (scope_id, generation_id) may need a
// redrive. ON CONFLICT DO NOTHING makes repeated calls for the same key
// (e.g. a retried Ack, or the same generation reported by more than one
// caller) idempotent -- only the FIRST call's next_attempt_at wins.
const ensureConfigStateDriftRedriveScheduledQuery = `
INSERT INTO config_state_drift_redrive (
    scope_id, generation_id, attempt_count, next_attempt_at, first_scheduled_at, updated_at
) VALUES ($1, $2, 0, $3, $3, $3)
ON CONFLICT (scope_id, generation_id) DO NOTHING
`

// claimAndAdvanceConfigStateDriftRedrivesQuery atomically claims up to $3 due
// rows ($1 = now) whose NEXT attempt will still leave them under the
// caller's attempt bound ($2) -- i.e. attempt_count < $2-1 -- advancing
// attempt_count and rescheduling next_attempt_at ($4) in the SAME statement.
// Rows on their FINAL allowed attempt do NOT match this query; they are
// claimed (and deleted) by claimAndDeleteExhaustedConfigStateDriftRedrivesQuery
// instead. FOR UPDATE SKIP LOCKED lets concurrent ingester replicas each
// claim a disjoint batch without blocking each other.
const claimAndAdvanceConfigStateDriftRedrivesQuery = `
WITH due AS (
    SELECT scope_id, generation_id
    FROM config_state_drift_redrive
    WHERE next_attempt_at <= $1
      AND attempt_count < $2 - 1
    ORDER BY next_attempt_at ASC
    LIMIT $3
    FOR UPDATE SKIP LOCKED
)
UPDATE config_state_drift_redrive AS redrive
SET attempt_count = redrive.attempt_count + 1,
    next_attempt_at = $4,
    updated_at = $1
FROM due
WHERE redrive.scope_id = due.scope_id
  AND redrive.generation_id = due.generation_id
RETURNING redrive.scope_id, redrive.generation_id, redrive.attempt_count
`

// claimAndDeleteExhaustedConfigStateDriftRedrivesQuery atomically claims and
// DELETES up to $3 due rows ($1 = now) whose attempt_count already equals
// $2-1 -- this claim is their LAST allowed attempt. Deleting on the final
// claim (issue #5593 P1-B) is what bounds the ledger's growth: without it, a
// row that reaches maxAttempts would sit forever with next_attempt_at frozen
// in the past, permanently re-satisfying the due-row scan's index condition
// and relying only on the non-indexed attempt_count residual filter to skip
// it, on every tick, for the life of the deployment. With this query, EVERY
// row EnsureScheduled creates is guaranteed to be deleted within exactly
// maxAttempts claims of it (regardless of whether ReplayDomain actually
// found a 'succeeded' row to reopen), so steady-state table size is bounded
// by (rows scheduled in roughly the last maxAttempts attempt-spacing
// window), never unbounded accumulation over the deployment's lifetime.
// Proven against a scratch Postgres 16 instance (issue #5593 P1-B): a
// maxAttempts=3 row is claimed by claimAndAdvance at attempt_count 0->1 and
// 1->2, does NOT match claimAndAdvance a third time (2 < 2 is false), DOES
// match this query (2 = 2), and the table is empty immediately after.
const claimAndDeleteExhaustedConfigStateDriftRedrivesQuery = `
WITH due AS (
    SELECT scope_id, generation_id
    FROM config_state_drift_redrive
    WHERE next_attempt_at <= $1
      AND attempt_count = $2 - 1
    ORDER BY next_attempt_at ASC
    LIMIT $3
    FOR UPDATE SKIP LOCKED
)
DELETE FROM config_state_drift_redrive AS redrive
USING due
WHERE redrive.scope_id = due.scope_id
  AND redrive.generation_id = due.generation_id
RETURNING redrive.scope_id, redrive.generation_id, redrive.attempt_count + 1
`

// ConfigStateDriftRedriveClaim identifies one config_state_drift
// (scope_id, generation_id) work item the catch-up loop just claimed for a
// redrive attempt, and the attempt number this claim represents (1-indexed:
// the value AFTER this claim's increment). Exhausted is true when this claim
// was the row's LAST allowed attempt -- the ledger row is already deleted by
// the time the caller sees this claim, so ClaimDue never needs to be called
// again for this (ScopeID, GenerationID) pair.
type ConfigStateDriftRedriveClaim struct {
	ScopeID      string
	GenerationID string
	AttemptCount int
	Exhausted    bool
}

// ConfigStateDriftRedriveStore persists the bounded redrive ledger backing
// the config_state_drift runtime recovery path (issue #5593 P1-1). See
// migrations/087_config_state_drift_runtime_redrive.sql for why this needs
// no claim/lease/fencing-token machinery, unlike CrossplaneRedriveStateStore.
type ConfigStateDriftRedriveStore struct {
	db  ExecQueryer
	Now func() time.Time
}

// NewConfigStateDriftRedriveStore constructs the redrive ledger store.
func NewConfigStateDriftRedriveStore(db ExecQueryer) ConfigStateDriftRedriveStore {
	return ConfigStateDriftRedriveStore{db: db}
}

func (s ConfigStateDriftRedriveStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// EnsureScheduled records that (scopeID, generationID) may need a
// config_state_drift redrive, first eligible at firstAttemptAt. Idempotent:
// a (scope, generation) pair already tracked is left untouched, so calling
// this more than once for the same generation never resets its attempt
// count or pushes its schedule back out.
func (s ConfigStateDriftRedriveStore) EnsureScheduled(
	ctx context.Context,
	scopeID string,
	generationID string,
	firstAttemptAt time.Time,
) error {
	if s.db == nil {
		return errors.New("config state drift redrive store database is required")
	}
	scopeID = strings.TrimSpace(scopeID)
	generationID = strings.TrimSpace(generationID)
	if scopeID == "" || generationID == "" {
		return errors.New("config state drift redrive requires scope id and generation id")
	}
	if _, err := s.db.ExecContext(
		ctx, ensureConfigStateDriftRedriveScheduledQuery, scopeID, generationID, firstAttemptAt.UTC(),
	); err != nil {
		return fmt.Errorf("ensure config state drift redrive scheduled: %w", err)
	}
	return nil
}

// ClaimDue claims up to limit due (scope, generation) pairs whose
// attempt_count is still under maxAttempts. A row on a non-final attempt is
// advanced and rescheduled nextAttemptAt for its next try; a row on its
// FINAL allowed attempt is claimed and immediately DELETED (Exhausted=true
// on the returned claim) -- issue #5593 P1-B's bounded-growth fix. Runs two
// queries (claimAndAdvance, then claimAndDeleteExhausted with the REMAINING
// batch budget) so the aggregate claimed count across both never exceeds
// limit.
func (s ConfigStateDriftRedriveStore) ClaimDue(
	ctx context.Context,
	maxAttempts int,
	limit int,
	nextAttemptAt time.Time,
) ([]ConfigStateDriftRedriveClaim, error) {
	if s.db == nil {
		return nil, errors.New("config state drift redrive store database is required")
	}
	if maxAttempts <= 0 {
		return nil, errors.New("config state drift redrive max attempts must be positive")
	}
	if limit <= 0 {
		return nil, errors.New("config state drift redrive claim limit must be positive")
	}

	now := s.now()

	advanced, err := s.claimAndAdvance(ctx, now, maxAttempts, limit, nextAttemptAt)
	if err != nil {
		return nil, err
	}

	remaining := limit - len(advanced)
	if remaining <= 0 {
		return advanced, nil
	}

	exhausted, err := s.claimAndDeleteExhausted(ctx, now, maxAttempts, remaining)
	if err != nil {
		return nil, err
	}

	return append(advanced, exhausted...), nil
}

func (s ConfigStateDriftRedriveStore) claimAndAdvance(
	ctx context.Context,
	now time.Time,
	maxAttempts int,
	limit int,
	nextAttemptAt time.Time,
) ([]ConfigStateDriftRedriveClaim, error) {
	rows, err := s.db.QueryContext(
		ctx, claimAndAdvanceConfigStateDriftRedrivesQuery, now, maxAttempts, limit, nextAttemptAt.UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("claim and advance config state drift redrives: %w", err)
	}
	defer func() { _ = rows.Close() }()

	claims := make([]ConfigStateDriftRedriveClaim, 0, limit)
	for rows.Next() {
		var claim ConfigStateDriftRedriveClaim
		if err := rows.Scan(&claim.ScopeID, &claim.GenerationID, &claim.AttemptCount); err != nil {
			return nil, fmt.Errorf("scan config state drift redrive claim-and-advance: %w", err)
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim and advance config state drift redrives: %w", err)
	}
	return claims, nil
}

func (s ConfigStateDriftRedriveStore) claimAndDeleteExhausted(
	ctx context.Context,
	now time.Time,
	maxAttempts int,
	limit int,
) ([]ConfigStateDriftRedriveClaim, error) {
	rows, err := s.db.QueryContext(
		ctx, claimAndDeleteExhaustedConfigStateDriftRedrivesQuery, now, maxAttempts, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("claim and delete exhausted config state drift redrives: %w", err)
	}
	defer func() { _ = rows.Close() }()

	claims := make([]ConfigStateDriftRedriveClaim, 0, limit)
	for rows.Next() {
		var claim ConfigStateDriftRedriveClaim
		if err := rows.Scan(&claim.ScopeID, &claim.GenerationID, &claim.AttemptCount); err != nil {
			return nil, fmt.Errorf("scan config state drift redrive claim-and-delete: %w", err)
		}
		claim.Exhausted = true
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim and delete exhausted config state drift redrives: %w", err)
	}
	return claims, nil
}
