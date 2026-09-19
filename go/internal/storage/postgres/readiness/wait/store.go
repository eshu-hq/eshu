// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wait

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// getReadinessWaitQuery reads one ledger row by primary key.
const getReadinessWaitQuery = `
SELECT first_deferred_at, missing_keys, missing_count, missing_fingerprint,
       committed_generation_id, committed_cycle_started_at, committed_fingerprint,
       settled_at, updated_at
FROM reducer_readiness_waits
WHERE scope_id = $1 AND domain = $2
`

// upsertReadinessWaitQuery writes one ledger row in a single statement. It
// never lowers or resets first_deferred_at unless $12 (reset anchor) is true,
// so two racing writers for one key keep the earlier anchor whichever commits
// last, and a replayed write leaves the row as it was.
const upsertReadinessWaitQuery = `
INSERT INTO reducer_readiness_waits AS wait (
    scope_id, domain, first_deferred_at, missing_keys, missing_count,
    missing_fingerprint, committed_generation_id, committed_cycle_started_at,
    committed_fingerprint, settled_at, updated_at
) VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (scope_id, domain) DO UPDATE SET
    first_deferred_at = CASE
        WHEN $12::boolean THEN EXCLUDED.first_deferred_at
        ELSE LEAST(wait.first_deferred_at, EXCLUDED.first_deferred_at)
    END,
    missing_keys = EXCLUDED.missing_keys,
    missing_count = EXCLUDED.missing_count,
    missing_fingerprint = EXCLUDED.missing_fingerprint,
    committed_generation_id = EXCLUDED.committed_generation_id,
    committed_cycle_started_at = EXCLUDED.committed_cycle_started_at,
    committed_fingerprint = EXCLUDED.committed_fingerprint,
    settled_at = EXCLUDED.settled_at,
    updated_at = EXCLUDED.updated_at
`

// clearReadinessWaitQuery deletes one ledger row by primary key.
const clearReadinessWaitQuery = `
DELETE FROM reducer_readiness_waits WHERE scope_id = $1 AND domain = $2
`

// Store implements crossscope.ReadinessWaitLedger over the
// reducer_readiness_waits table (migration 110). Every statement is a
// single-row primary-key read or write and holds no lock beyond its own
// statement.
type Store struct {
	DB db.ExecQueryer
}

var _ crossscope.ReadinessWaitLedger = Store{}

// GetReadinessWait returns the (scope, domain) row and whether it exists. A
// database error is returned as an error, never as a missing row.
func (s Store) GetReadinessWait(
	ctx context.Context,
	scopeID string,
	domain reducercontract.Domain,
) (crossscope.ReadinessWait, bool, error) {
	if s.DB == nil {
		return crossscope.ReadinessWait{}, false, fmt.Errorf("readiness wait store requires a database")
	}
	rows, err := s.DB.QueryContext(ctx, getReadinessWaitQuery, scopeID, string(domain))
	if err != nil {
		return crossscope.ReadinessWait{}, false, fmt.Errorf("read readiness wait %s/%s: %w", scopeID, domain, err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return crossscope.ReadinessWait{}, false, fmt.Errorf("read readiness wait %s/%s: %w", scopeID, domain, err)
		}
		return crossscope.ReadinessWait{}, false, nil
	}
	wait := crossscope.ReadinessWait{ScopeID: scopeID, Domain: domain}
	var rawKeys []byte
	var committedCycle, settledAt sql.NullTime
	if err := rows.Scan(
		&wait.FirstDeferredAt, &rawKeys, &wait.MissingCount, &wait.MissingFingerprint,
		&wait.CommittedGenerationID, &committedCycle, &wait.CommittedFingerprint,
		&settledAt, &wait.UpdatedAt,
	); err != nil {
		return crossscope.ReadinessWait{}, false, fmt.Errorf("scan readiness wait %s/%s: %w", scopeID, domain, err)
	}
	if err := json.Unmarshal(rawKeys, &wait.MissingKeys); err != nil {
		return crossscope.ReadinessWait{}, false, fmt.Errorf("decode readiness wait keys %s/%s: %w", scopeID, domain, err)
	}
	wait.CommittedCycleStartedAt = committedCycle.Time
	wait.SettledAt = settledAt.Time
	return wait, true, rows.Err()
}

// UpsertReadinessWait inserts or updates the row, keeping the earlier
// first_deferred_at unless resetAnchor is true.
func (s Store) UpsertReadinessWait(ctx context.Context, wait crossscope.ReadinessWait, resetAnchor bool) error {
	if s.DB == nil {
		return fmt.Errorf("readiness wait store requires a database")
	}
	keys := wait.MissingKeys
	if keys == nil {
		keys = []string{}
	}
	rawKeys, err := json.Marshal(keys)
	if err != nil {
		return fmt.Errorf("encode readiness wait keys %s/%s: %w", wait.ScopeID, wait.Domain, err)
	}
	if _, err := s.DB.ExecContext(ctx, upsertReadinessWaitQuery,
		wait.ScopeID, string(wait.Domain), wait.FirstDeferredAt, string(rawKeys), wait.MissingCount,
		wait.MissingFingerprint, wait.CommittedGenerationID, nullableTime(wait.CommittedCycleStartedAt),
		wait.CommittedFingerprint, nullableTime(wait.SettledAt), wait.UpdatedAt, resetAnchor,
	); err != nil {
		return fmt.Errorf("upsert readiness wait %s/%s: %w", wait.ScopeID, wait.Domain, err)
	}
	return nil
}

// ClearReadinessWait deletes the row. Deleting an absent row is not an error.
func (s Store) ClearReadinessWait(ctx context.Context, scopeID string, domain reducercontract.Domain) error {
	if s.DB == nil {
		return fmt.Errorf("readiness wait store requires a database")
	}
	if _, err := s.DB.ExecContext(ctx, clearReadinessWaitQuery, scopeID, string(domain)); err != nil {
		return fmt.Errorf("clear readiness wait %s/%s: %w", scopeID, domain, err)
	}
	return nil
}

// nullableTime binds a zero time as SQL NULL.
func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
