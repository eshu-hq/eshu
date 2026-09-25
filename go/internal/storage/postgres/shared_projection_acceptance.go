// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const (
	sharedProjectionAcceptanceBatchSize = 500
	acceptanceColumnsPerRow             = 6
)

const sharedProjectionAcceptanceSchemaSQL = `
CREATE TABLE IF NOT EXISTS shared_projection_acceptance (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    acceptance_unit_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    accepted_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, acceptance_unit_id, source_run_id)
);
CREATE INDEX IF NOT EXISTS shared_projection_acceptance_scope_idx
    ON shared_projection_acceptance (scope_id, generation_id);
CREATE INDEX IF NOT EXISTS shared_projection_acceptance_updated_idx
    ON shared_projection_acceptance (updated_at DESC);
`

const upsertSharedProjectionAcceptanceBatchPrefix = `
INSERT INTO shared_projection_acceptance (
    scope_id, acceptance_unit_id, source_run_id, generation_id, accepted_at, updated_at
) VALUES `

// upsertSharedProjectionAcceptanceBatchSuffix makes the acceptance row
// advance-only (#6679). The DO UPDATE fires only when the incoming generation
// equals the stored one (an idempotent retry that refreshes accepted_at and
// updated_at) or sorts strictly after it by scope_generations
// (observed_at, generation_id) — the same total order priorGenerationIDSQL
// uses. A stale write, including one whose stored or incoming generation row
// is not visible to the statement snapshot, leaves the row untouched and is
// omitted from RETURNING, which is how callers count and log stale writes.
//
// Under READ COMMITTED a conflicting writer blocks on the row lock and then
// evaluates this WHERE against the newest committed row version, so the guard
// needs no extra locking to hold across concurrent out-of-order writers.
const upsertSharedProjectionAcceptanceBatchSuffix = `
ON CONFLICT (scope_id, acceptance_unit_id, source_run_id) DO UPDATE
SET generation_id = EXCLUDED.generation_id,
    accepted_at = EXCLUDED.accepted_at,
    updated_at = EXCLUDED.updated_at
WHERE shared_projection_acceptance.generation_id = EXCLUDED.generation_id
   OR EXISTS (
       SELECT 1
       FROM scope_generations AS incoming
       JOIN scope_generations AS stored
         ON stored.generation_id = shared_projection_acceptance.generation_id
       WHERE incoming.generation_id = EXCLUDED.generation_id
         AND (incoming.observed_at, incoming.generation_id)
             > (stored.observed_at, stored.generation_id)
   )
RETURNING scope_id, acceptance_unit_id, source_run_id
`

const lookupSharedProjectionAcceptanceSQL = `
SELECT generation_id
FROM shared_projection_acceptance
WHERE scope_id = $1
  AND acceptance_unit_id = $2
  AND source_run_id = $3
LIMIT 1
`

const lookupSharedProjectionAcceptanceByUnitSQL = `
SELECT generation_id
FROM shared_projection_acceptance
WHERE acceptance_unit_id = $1
  AND source_run_id = $2
ORDER BY updated_at DESC, scope_id DESC
LIMIT 1
`

// SharedProjectionAcceptance is one durable bounded-unit acceptance row.
type SharedProjectionAcceptance struct {
	ScopeID          string
	AcceptanceUnitID string
	SourceRunID      string
	GenerationID     string
	AcceptedAt       time.Time
	UpdatedAt        time.Time
}

// SharedProjectionAcceptanceStore persists shared projection acceptance rows in
// PostgreSQL.
type SharedProjectionAcceptanceStore struct {
	database db.ExecQueryer
}

// NewSharedProjectionAcceptanceStore creates an acceptance store backed by the
// provided database handle.
func NewSharedProjectionAcceptanceStore(database db.ExecQueryer) *SharedProjectionAcceptanceStore {
	return &SharedProjectionAcceptanceStore{database: database}
}

// SharedProjectionAcceptanceSchemaSQL returns the DDL for the acceptance
// table.
func SharedProjectionAcceptanceSchemaSQL() string {
	return sharedProjectionAcceptanceSchemaSQL
}

// EnsureSchema applies the acceptance DDL.
func (s *SharedProjectionAcceptanceStore) EnsureSchema(ctx context.Context) error {
	_, err := s.database.ExecContext(ctx, sharedProjectionAcceptanceSchemaSQL)
	return err
}

// Upsert writes bounded-unit acceptance rows in batches. Rows whose
// generation sorts before the stored generation are skipped (see
// UpsertReportingStale); callers that need that signal use UpsertReportingStale.
func (s *SharedProjectionAcceptanceStore) Upsert(ctx context.Context, rows []SharedProjectionAcceptance) error {
	_, err := s.UpsertReportingStale(ctx, rows)
	return err
}

// UpsertReportingStale writes bounded-unit acceptance rows in batches and
// returns the rows the advance-only guard rejected because the stored row
// already carries a newer generation. A same-generation retry is applied (it
// refreshes accepted_at and updated_at) and is never reported as stale.
// Input rows must carry unique (scope, unit, run) keys, which
// buildSharedProjectionAcceptanceRows guarantees; a duplicate key inside one
// batch is rejected by PostgreSQL (SQLSTATE 21000).
func (s *SharedProjectionAcceptanceStore) UpsertReportingStale(
	ctx context.Context,
	rows []SharedProjectionAcceptance,
) ([]SharedProjectionAcceptance, error) {
	var stale []SharedProjectionAcceptance
	for i := 0; i < len(rows); i += sharedProjectionAcceptanceBatchSize {
		end := min(i+sharedProjectionAcceptanceBatchSize, len(rows))
		batchStale, err := upsertSharedProjectionAcceptanceBatch(ctx, s.database, rows[i:end])
		if err != nil {
			return stale, err
		}
		stale = append(stale, batchStale...)
	}
	return stale, nil
}

// Lookup returns the accepted generation for one exact bounded-unit key.
func (s *SharedProjectionAcceptanceStore) Lookup(ctx context.Context, scopeID, acceptanceUnitID, sourceRunID string) (string, bool, error) {
	rows, err := s.database.QueryContext(ctx, lookupSharedProjectionAcceptanceSQL, scopeID, acceptanceUnitID, sourceRunID)
	if err != nil {
		return "", false, fmt.Errorf("query shared projection acceptance: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return "", false, nil
	}

	var generationID string
	if err := rows.Scan(&generationID); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("scan shared projection acceptance: %w", err)
	}

	return generationID, true, rows.Err()
}

// sharedProjectionAcceptanceRowEstimateSQL reads the PostgreSQL planner row
// estimate for the acceptance table. It is an O(1) catalog lookup rather than a
// COUNT(*), so the observable gauge backed by it stays cheap to evaluate on
// every metrics scrape regardless of table size.
const sharedProjectionAcceptanceRowEstimateSQL = `
SELECT GREATEST(reltuples, 0)::bigint
FROM pg_class
WHERE relname = 'shared_projection_acceptance' AND relkind = 'r'
LIMIT 1
`

// AcceptanceRowCount returns an approximate count of durable shared acceptance
// rows using the PostgreSQL planner estimate (pg_class.reltuples). It is
// deliberately an estimate, not an exact COUNT(*), so the observable gauge it
// backs (eshu_dp_shared_acceptance_rows) costs O(1) per metrics scrape rather
// than a full table scan; the value tracks the true row count within
// autovacuum/ANALYZE freshness. A table that has never been analyzed reports 0.
// It satisfies telemetry.AcceptanceObserver.
func (s *SharedProjectionAcceptanceStore) AcceptanceRowCount(ctx context.Context) (int64, error) {
	rows, err := s.database.QueryContext(ctx, sharedProjectionAcceptanceRowEstimateSQL)
	if err != nil {
		return 0, fmt.Errorf("query shared projection acceptance row estimate: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return 0, rows.Err()
	}
	var count int64
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("scan shared projection acceptance row estimate: %w", err)
	}
	if count < 0 {
		count = 0
	}
	return count, rows.Err()
}

// LookupByAcceptanceUnit preserves the current repository/run reducer seam
// while the scope-aware bounded-unit contract is wired through the caller
// stack.
func (s *SharedProjectionAcceptanceStore) LookupByAcceptanceUnit(ctx context.Context, acceptanceUnitID, sourceRunID string) (string, bool, error) {
	rows, err := s.database.QueryContext(ctx, lookupSharedProjectionAcceptanceByUnitSQL, acceptanceUnitID, sourceRunID)
	if err != nil {
		return "", false, fmt.Errorf("query shared projection acceptance by unit: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return "", false, nil
	}

	var generationID string
	if err := rows.Scan(&generationID); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("scan shared projection acceptance by unit: %w", err)
	}

	return generationID, true, rows.Err()
}

// upsertSharedProjectionAcceptanceBatch writes one batch and returns the
// submitted rows the advance-only guard skipped, derived by subtracting the
// RETURNING keys (inserted or updated rows) from the submitted keys.
func upsertSharedProjectionAcceptanceBatch(
	ctx context.Context,
	database db.ExecQueryer,
	batch []SharedProjectionAcceptance,
) ([]SharedProjectionAcceptance, error) {
	if len(batch) == 0 {
		return nil, nil
	}

	args := make([]any, 0, len(batch)*acceptanceColumnsPerRow)
	var values strings.Builder

	for i, row := range batch {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * acceptanceColumnsPerRow
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6,
		)
		args = append(
			args,
			row.ScopeID,
			row.AcceptanceUnitID,
			row.SourceRunID,
			row.GenerationID,
			row.AcceptedAt,
			row.UpdatedAt,
		)
	}

	query := upsertSharedProjectionAcceptanceBatchPrefix + values.String() + upsertSharedProjectionAcceptanceBatchSuffix
	result, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("upsert shared projection acceptance batch (%d rows): %w", len(batch), err)
	}
	defer func() { _ = result.Close() }()

	applied := make(map[sharedProjectionAcceptanceRowKey]struct{}, len(batch))
	for result.Next() {
		var key sharedProjectionAcceptanceRowKey
		if err := result.Scan(&key.scopeID, &key.acceptanceUnitID, &key.sourceRunID); err != nil {
			return nil, fmt.Errorf("scan shared projection acceptance upsert result: %w", err)
		}
		applied[key] = struct{}{}
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("iterate shared projection acceptance upsert result (%d rows): %w", len(batch), err)
	}

	var stale []SharedProjectionAcceptance
	for _, row := range batch {
		key := sharedProjectionAcceptanceRowKey{
			scopeID:          row.ScopeID,
			acceptanceUnitID: row.AcceptanceUnitID,
			sourceRunID:      row.SourceRunID,
		}
		if _, ok := applied[key]; !ok {
			stale = append(stale, row)
		}
	}
	return stale, nil
}

// sharedProjectionAcceptanceRowKey is the acceptance primary key.
type sharedProjectionAcceptanceRowKey struct {
	scopeID          string
	acceptanceUnitID string
	sourceRunID      string
}
