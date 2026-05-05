package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
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

const upsertSharedProjectionAcceptanceBatchSuffix = `
ON CONFLICT (scope_id, acceptance_unit_id, source_run_id) DO UPDATE
SET generation_id = EXCLUDED.generation_id,
    accepted_at = EXCLUDED.accepted_at,
    updated_at = EXCLUDED.updated_at
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
	db ExecQueryer
}

// NewSharedProjectionAcceptanceStore creates an acceptance store backed by the
// provided database handle.
func NewSharedProjectionAcceptanceStore(db ExecQueryer) *SharedProjectionAcceptanceStore {
	return &SharedProjectionAcceptanceStore{db: db}
}

// SharedProjectionAcceptanceSchemaSQL returns the DDL for the acceptance
// table.
func SharedProjectionAcceptanceSchemaSQL() string {
	return sharedProjectionAcceptanceSchemaSQL
}

// EnsureSchema applies the acceptance DDL.
func (s *SharedProjectionAcceptanceStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, sharedProjectionAcceptanceSchemaSQL)
	return err
}

// Upsert writes bounded-unit acceptance rows in batches.
func (s *SharedProjectionAcceptanceStore) Upsert(ctx context.Context, rows []SharedProjectionAcceptance) error {
	if len(rows) == 0 {
		return nil
	}

	for i := 0; i < len(rows); i += sharedProjectionAcceptanceBatchSize {
		end := i + sharedProjectionAcceptanceBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := upsertSharedProjectionAcceptanceBatch(ctx, s.db, rows[i:end]); err != nil {
			return err
		}
	}

	return nil
}

// Lookup returns the accepted generation for one exact bounded-unit key.
func (s *SharedProjectionAcceptanceStore) Lookup(ctx context.Context, scopeID, acceptanceUnitID, sourceRunID string) (string, bool, error) {
	rows, err := s.db.QueryContext(ctx, lookupSharedProjectionAcceptanceSQL, scopeID, acceptanceUnitID, sourceRunID)
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

// LookupByAcceptanceUnit preserves the current repository/run reducer seam
// while the scope-aware bounded-unit contract is wired through the caller
// stack.
func (s *SharedProjectionAcceptanceStore) LookupByAcceptanceUnit(ctx context.Context, acceptanceUnitID, sourceRunID string) (string, bool, error) {
	rows, err := s.db.QueryContext(ctx, lookupSharedProjectionAcceptanceByUnitSQL, acceptanceUnitID, sourceRunID)
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

func upsertSharedProjectionAcceptanceBatch(ctx context.Context, db ExecQueryer, batch []SharedProjectionAcceptance) error {
	if len(batch) == 0 {
		return nil
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
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert shared projection acceptance batch (%d rows): %w", len(batch), err)
	}

	return nil
}

func sharedProjectionAcceptanceBootstrapDefinition() Definition {
	return Definition{
		Name: "shared_projection_acceptance",
		Path: "schema/data-plane/postgres/011_shared_projection_acceptance.sql",
		SQL:  sharedProjectionAcceptanceSchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, sharedProjectionAcceptanceBootstrapDefinition())
}
