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

// claimDueConfigStateDriftRedrivesQuery atomically claims up to $3 rows that
// are due ($1 = now) and still under the caller-supplied attempt bound ($2),
// advancing attempt_count and rescheduling next_attempt_at ($4) in the SAME
// statement. FOR UPDATE SKIP LOCKED lets concurrent ingester replicas each
// claim a disjoint batch without blocking each other. Proven against a
// scratch Postgres 16 instance (issue #5593 P1-1): after attempt_count
// reaches the caller's bound, the WHERE clause excludes the row from every
// later claim -- this is the query that makes "genuine no-owner, forever"
// terminate instead of retrying without limit.
const claimDueConfigStateDriftRedrivesQuery = `
WITH due AS (
    SELECT scope_id, generation_id
    FROM config_state_drift_redrive
    WHERE next_attempt_at <= $1
      AND attempt_count < $2
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

// ConfigStateDriftRedriveClaim identifies one config_state_drift
// (scope_id, generation_id) work item the catch-up loop just claimed for a
// redrive attempt, and the attempt number this claim represents (1-indexed:
// the value AFTER this claim's increment).
type ConfigStateDriftRedriveClaim struct {
	ScopeID      string
	GenerationID string
	AttemptCount int
}

// ConfigStateDriftRedriveStore persists the bounded redrive ledger backing
// ConfigStateDriftRuntimeTrigger's recovery path (issue #5593 P1-1): see
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
// attempt_count is still under maxAttempts, advances their attempt_count,
// and reschedules them nextAttemptAt for their next try. A row whose
// attempt_count reaches maxAttempts on this call is claimed one last time
// (the caller gets its final redrive chance) and then never claimed again --
// the bounded-termination guarantee.
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
	rows, err := s.db.QueryContext(
		ctx, claimDueConfigStateDriftRedrivesQuery, now, maxAttempts, limit, nextAttemptAt.UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("claim due config state drift redrives: %w", err)
	}
	defer func() { _ = rows.Close() }()

	claims := make([]ConfigStateDriftRedriveClaim, 0, limit)
	for rows.Next() {
		var claim ConfigStateDriftRedriveClaim
		if err := rows.Scan(&claim.ScopeID, &claim.GenerationID, &claim.AttemptCount); err != nil {
			return nil, fmt.Errorf("scan config state drift redrive claim: %w", err)
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim due config state drift redrives: %w", err)
	}
	return claims, nil
}
