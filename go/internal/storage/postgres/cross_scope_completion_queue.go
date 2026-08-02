// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const crossScopeCompletionLiveDomainConstraint = "cross_scope_completion_events_live_domain_uniq"

const claimCrossScopeCompletionQuery = `
WITH candidate AS (
    SELECT event.event_id
    FROM cross_scope_completion_events AS event
    WHERE (
            (event.status IN ('pending', 'retrying')
             AND (event.visible_at IS NULL OR event.visible_at <= $1))
         OR (event.status IN ('claimed', 'running')
             AND event.claim_until <= $1)
      )
      AND NOT EXISTS (
          SELECT 1
          FROM cross_scope_completion_events AS live
          WHERE live.producer_domain = event.producer_domain
            AND live.status IN ('claimed', 'running')
            AND live.claim_until > $1
      )
    ORDER BY CASE
                 WHEN event.status IN ('claimed', 'running') THEN 0
                 ELSE 1
             END,
             event.event_id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE cross_scope_completion_events AS event
SET status = 'claimed',
    attempt_count = event.attempt_count + 1,
    lease_owner = $2,
    claim_until = $3,
    visible_at = NULL,
    claim_epoch = event.claim_epoch + 1,
    failure_message = NULL,
    updated_at = $1
FROM candidate
WHERE event.event_id = candidate.event_id
RETURNING event.event_id,
          event.producer_domain,
          event.claim_epoch,
          event.attempt_count
`

const heartbeatCrossScopeCompletionQuery = `
UPDATE cross_scope_completion_events
SET claim_until = $1,
    updated_at = $2
WHERE event_id = $3
  AND producer_domain = $4
  AND lease_owner = $5
  AND claim_epoch = $6
  AND status IN ('claimed', 'running')
`

const retryCrossScopeCompletionQuery = `
WITH owned AS MATERIALIZED (
    DELETE FROM cross_scope_completion_events
    WHERE event_id = $4
      AND producer_domain = $5
      AND lease_owner = $6
      AND claim_epoch = $7
      AND status IN ('claimed', 'running')
    RETURNING producer_domain, producer_item_count, attempt_count, created_at
)
INSERT INTO cross_scope_completion_events (
    producer_domain, producer_item_count, status, attempt_count,
    visible_at, failure_message, created_at, updated_at
)
SELECT producer_domain, producer_item_count, 'retrying', attempt_count,
       $1, $2, created_at, $3
FROM owned
ON CONFLICT (producer_domain) WHERE status IN ('pending', 'retrying') DO UPDATE SET
    producer_item_count =
        cross_scope_completion_events.producer_item_count +
        EXCLUDED.producer_item_count,
    status = 'retrying',
    attempt_count = GREATEST(
        cross_scope_completion_events.attempt_count,
        EXCLUDED.attempt_count
    ),
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = GREATEST(
        cross_scope_completion_events.visible_at,
        EXCLUDED.visible_at
    ),
    failure_message = EXCLUDED.failure_message,
    created_at = LEAST(
        cross_scope_completion_events.created_at,
        EXCLUDED.created_at
    ),
    updated_at = EXCLUDED.updated_at
`

// ErrCrossScopeCompletionClaimRejected means a stale owner or claim epoch may
// no longer heartbeat, retry, or fan out an event.
var ErrCrossScopeCompletionClaimRejected = errors.New("cross-scope completion claim rejected")

// CrossScopeCompletionStore is the Postgres-backed durable completion-event
// queue and set-based consumer fanout.
type CrossScopeCompletionStore struct {
	db  ExecQueryer
	Now func() time.Time
}

// NewCrossScopeCompletionStore returns a completion queue over db.
func NewCrossScopeCompletionStore(db ExecQueryer) *CrossScopeCompletionStore {
	return &CrossScopeCompletionStore{db: db}
}

func (s *CrossScopeCompletionStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// Claim leases the oldest available producer-domain event. A partial unique
// index admits at most one live lease for each producer domain.
func (s *CrossScopeCompletionStore) Claim(
	ctx context.Context,
	leaseOwner string,
	leaseDuration time.Duration,
) (reducer.CrossScopeCompletionLease, bool, error) {
	if s == nil || s.db == nil {
		return reducer.CrossScopeCompletionLease{}, false, errors.New("cross-scope completion database is required")
	}
	if strings.TrimSpace(leaseOwner) == "" {
		return reducer.CrossScopeCompletionLease{}, false, errors.New("cross-scope completion lease owner is required")
	}
	if leaseDuration <= 0 {
		return reducer.CrossScopeCompletionLease{}, false, errors.New("cross-scope completion lease duration must be positive")
	}
	now := s.now()
	rows, err := s.db.QueryContext(
		ctx,
		claimCrossScopeCompletionQuery,
		now,
		leaseOwner,
		now.Add(leaseDuration),
	)
	if err != nil {
		if isCrossScopeCompletionLiveLeaseConflict(err) {
			return reducer.CrossScopeCompletionLease{}, false, nil
		}
		return reducer.CrossScopeCompletionLease{}, false, fmt.Errorf("claim cross-scope completion event: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			if isCrossScopeCompletionLiveLeaseConflict(err) {
				return reducer.CrossScopeCompletionLease{}, false, nil
			}
			return reducer.CrossScopeCompletionLease{}, false, fmt.Errorf("claim cross-scope completion event: %w", err)
		}
		return reducer.CrossScopeCompletionLease{}, false, nil
	}
	var (
		lease     reducer.CrossScopeCompletionLease
		domainRaw string
	)
	if err := rows.Scan(
		&lease.EventID,
		&domainRaw,
		&lease.ClaimEpoch,
		&lease.AttemptCount,
	); err != nil {
		return reducer.CrossScopeCompletionLease{}, false, fmt.Errorf("scan cross-scope completion claim: %w", err)
	}
	domain, err := reducer.ParseDomain(domainRaw)
	if err != nil {
		return reducer.CrossScopeCompletionLease{}, false, fmt.Errorf("scan cross-scope completion producer: %w", err)
	}
	lease.ProducerDomain = domain
	lease.LeaseOwner = leaseOwner
	return lease, true, nil
}

// Heartbeat extends one exact completion-event lease.
func (s *CrossScopeCompletionStore) Heartbeat(
	ctx context.Context,
	lease reducer.CrossScopeCompletionLease,
	leaseDuration time.Duration,
) error {
	if s == nil || s.db == nil {
		return errors.New("cross-scope completion database is required")
	}
	now := s.now()
	result, err := s.db.ExecContext(
		ctx,
		heartbeatCrossScopeCompletionQuery,
		now.Add(leaseDuration),
		now,
		lease.EventID,
		lease.ProducerDomain,
		lease.LeaseOwner,
		lease.ClaimEpoch,
	)
	if err != nil {
		return fmt.Errorf("heartbeat cross-scope completion event: %w", err)
	}
	return requireCrossScopeCompletionMutation(result)
}

// Retry releases one exact lease for automatic bounded-backoff redrive.
func (s *CrossScopeCompletionStore) Retry(
	ctx context.Context,
	lease reducer.CrossScopeCompletionLease,
	cause error,
	visibleAt time.Time,
) error {
	if s == nil || s.db == nil {
		return errors.New("cross-scope completion database is required")
	}
	if cause == nil {
		return errors.New("cross-scope completion retry cause is required")
	}
	result, err := s.db.ExecContext(
		ctx,
		retryCrossScopeCompletionQuery,
		visibleAt.UTC(),
		cause.Error(),
		s.now(),
		lease.EventID,
		lease.ProducerDomain,
		lease.LeaseOwner,
		lease.ClaimEpoch,
	)
	if err != nil {
		return fmt.Errorf("retry cross-scope completion event: %w", err)
	}
	return requireCrossScopeCompletionMutation(result)
}

func requireCrossScopeCompletionMutation(result sql.Result) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("cross-scope completion rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrCrossScopeCompletionClaimRejected
	}
	return nil
}

func isCrossScopeCompletionLiveLeaseConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == crossScopeCompletionLiveDomainConstraint
}
