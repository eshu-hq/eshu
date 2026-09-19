// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wait

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// getReadinessWaitQuery reads one ledger row by primary key.
const getReadinessWaitQuery = `
SELECT first_deferred_at, missing_keys, missing_count, missing_fingerprint,
       committed_generation_id, committed_cycle_started_at, committed_fingerprint,
       settled_at, updated_at, anchor_epoch, cleared_at, row_version
FROM reducer_readiness_waits
WHERE scope_id = $1 AND domain = $2
`

// upsertReadinessWaitQuery writes one ledger row in a single statement,
// fenced by anchor_epoch. A write from a lower epoch than the stored row is
// dropped by the ON CONFLICT WHERE, so a straggler that read the row before
// an anchor reset or a clear cannot restore the older wait. A higher epoch
// replaces the anchor; an equal epoch keeps the earlier anchor, so two racing
// first-defer writers keep the earlier one whichever commits last, and a
// replayed write leaves the row as it was. ON CONFLICT DO UPDATE re-evaluates
// the WHERE and SET against the latest committed row version after waiting
// on its row lock, so the fence holds under READ COMMITTED.
const upsertReadinessWaitQuery = `
INSERT INTO reducer_readiness_waits AS wait (
    scope_id, domain, first_deferred_at, missing_keys, missing_count,
    missing_fingerprint, committed_generation_id, committed_cycle_started_at,
    committed_fingerprint, settled_at, updated_at, anchor_epoch, cleared_at
) VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9, $10, $11, $12, NULL)
ON CONFLICT (scope_id, domain) DO UPDATE SET
    first_deferred_at = CASE
        WHEN EXCLUDED.anchor_epoch > wait.anchor_epoch THEN EXCLUDED.first_deferred_at
        ELSE LEAST(wait.first_deferred_at, EXCLUDED.first_deferred_at)
    END,
    missing_keys = EXCLUDED.missing_keys,
    missing_count = EXCLUDED.missing_count,
    missing_fingerprint = EXCLUDED.missing_fingerprint,
    committed_generation_id = EXCLUDED.committed_generation_id,
    committed_cycle_started_at = EXCLUDED.committed_cycle_started_at,
    committed_fingerprint = EXCLUDED.committed_fingerprint,
    settled_at = EXCLUDED.settled_at,
    updated_at = EXCLUDED.updated_at,
    anchor_epoch = EXCLUDED.anchor_epoch,
    cleared_at = NULL,
    row_version = wait.row_version + 1
WHERE EXCLUDED.anchor_epoch >= wait.anchor_epoch
`

// clearReadinessWaitQuery turns one ledger row into a tombstone at the next
// epoch, only when the stored epoch and row_version are still the ones the
// clearing evaluation read. The row_version compare-and-set means a clear
// never lands over a write that followed its read: a straggler that saw the
// set empty cannot tombstone a wait a live worker rewrote at the same epoch
// (review P3-b). The tombstone keeps the clearing commit's marker and no
// missing set.
const clearReadinessWaitQuery = `
UPDATE reducer_readiness_waits SET
    first_deferred_at = $6,
    missing_keys = '[]'::jsonb,
    missing_count = 0,
    missing_fingerprint = '',
    committed_generation_id = $4,
    committed_cycle_started_at = $5,
    committed_fingerprint = '',
    settled_at = NULL,
    anchor_epoch = anchor_epoch + 1,
    cleared_at = $6,
    updated_at = $6,
    row_version = row_version + 1
WHERE scope_id = $1 AND domain = $2 AND anchor_epoch = $3 AND row_version = $7
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
	var committedCycle, settledAt, clearedAt sql.NullTime
	if err := rows.Scan(
		&wait.FirstDeferredAt, &rawKeys, &wait.MissingCount, &wait.MissingFingerprint,
		&wait.CommittedGenerationID, &committedCycle, &wait.CommittedFingerprint,
		&settledAt, &wait.UpdatedAt, &wait.AnchorEpoch, &clearedAt, &wait.RowVersion,
	); err != nil {
		return crossscope.ReadinessWait{}, false, fmt.Errorf("scan readiness wait %s/%s: %w", scopeID, domain, err)
	}
	if err := json.Unmarshal(rawKeys, &wait.MissingKeys); err != nil {
		return crossscope.ReadinessWait{}, false, fmt.Errorf("decode readiness wait keys %s/%s: %w", scopeID, domain, err)
	}
	wait.CommittedCycleStartedAt = committedCycle.Time
	wait.SettledAt = settledAt.Time
	wait.ClearedAt = clearedAt.Time
	return wait, true, rows.Err()
}

// UpsertReadinessWait inserts or updates the row under the anchor_epoch
// fence. A write dropped by the fence is logged, not returned as an error: the
// row already holds a newer wait.
func (s Store) UpsertReadinessWait(ctx context.Context, wait crossscope.ReadinessWait) error {
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
	result, err := s.DB.ExecContext(ctx, upsertReadinessWaitQuery,
		wait.ScopeID, string(wait.Domain), wait.FirstDeferredAt, string(rawKeys), wait.MissingCount,
		wait.MissingFingerprint, wait.CommittedGenerationID, nullableTime(wait.CommittedCycleStartedAt),
		wait.CommittedFingerprint, nullableTime(wait.SettledAt), wait.UpdatedAt, wait.AnchorEpoch,
	)
	if err != nil {
		return fmt.Errorf("upsert readiness wait %s/%s: %w", wait.ScopeID, wait.Domain, err)
	}
	logFenced(ctx, result, "upsert", wait)
	return nil
}

// ClearReadinessWait turns the row into a tombstone at the next epoch when the
// stored epoch and row version equal wait.AnchorEpoch and wait.RowVersion.
// Clearing an absent row, or one another writer changed after the read,
// changes nothing and is not an error.
func (s Store) ClearReadinessWait(ctx context.Context, wait crossscope.ReadinessWait) error {
	if s.DB == nil {
		return fmt.Errorf("readiness wait store requires a database")
	}
	clearedAt := wait.ClearedAt
	if clearedAt.IsZero() {
		clearedAt = wait.UpdatedAt
	}
	if clearedAt.IsZero() {
		return fmt.Errorf("clear readiness wait %s/%s: a cleared_at time is required", wait.ScopeID, wait.Domain)
	}
	result, err := s.DB.ExecContext(ctx, clearReadinessWaitQuery,
		wait.ScopeID, string(wait.Domain), wait.AnchorEpoch, wait.CommittedGenerationID,
		nullableTime(wait.CommittedCycleStartedAt), clearedAt, wait.RowVersion,
	)
	if err != nil {
		return fmt.Errorf("clear readiness wait %s/%s: %w", wait.ScopeID, wait.Domain, err)
	}
	logFenced(ctx, result, "clear", wait)
	return nil
}

// logFenced reports a write that changed no row: the anchor_epoch fence
// dropped it because a newer reset, settle, or clear already advanced the row,
// or, for a clear, the row is absent or its row_version moved after the read.
// Operators see it as a lease-expired straggler.
func logFenced(ctx context.Context, result sql.Result, operation string, wait crossscope.ReadinessWait) {
	if result == nil {
		return
	}
	affected, err := result.RowsAffected()
	if err != nil || affected > 0 {
		return
	}
	slog.InfoContext(ctx, "readiness wait write dropped by the anchor epoch fence",
		log.ScopeID(wait.ScopeID),
		slog.String("domain", string(wait.Domain)),
		slog.String("readiness_wait_operation", operation),
		slog.Int64("anchor_epoch", wait.AnchorEpoch),
		slog.Int64("row_version", wait.RowVersion),
	)
}

// nullableTime binds a zero time as SQL NULL.
func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
