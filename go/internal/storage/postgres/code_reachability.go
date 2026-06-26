// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	codeReachabilityBatchSize = 500
	codeReachabilityColumns   = 13
)

const codeReachabilitySchemaSQL = `
CREATE TABLE IF NOT EXISTS code_reachability_rows (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    repository_id TEXT NOT NULL,
    root_entity_id TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    depth INTEGER NOT NULL,
    state TEXT NOT NULL,
    confidence DOUBLE PRECISION NOT NULL,
    min_resolution_method TEXT NOT NULL,
    evidence JSONB NOT NULL,
    root_kinds JSONB NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, repository_id, root_entity_id, entity_id)
);

CREATE INDEX IF NOT EXISTS code_reachability_latest_lookup_idx
    ON code_reachability_rows (repository_id, entity_id, state, confidence DESC);

CREATE INDEX IF NOT EXISTS code_reachability_entity_lookup_idx
    ON code_reachability_rows (entity_id, state, confidence DESC);

CREATE INDEX IF NOT EXISTS code_reachability_root_idx
    ON code_reachability_rows (repository_id, root_entity_id, depth, entity_id);

CREATE TABLE IF NOT EXISTS code_reachability_repository_watermarks (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    repository_id TEXT NOT NULL,
    truncated BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, repository_id)
);

ALTER TABLE code_reachability_repository_watermarks
    ADD COLUMN IF NOT EXISTS truncated BOOLEAN NOT NULL DEFAULT FALSE;
`

const upsertCodeReachabilityBatchPrefix = `
INSERT INTO code_reachability_rows (
    scope_id, generation_id, repository_id, root_entity_id, entity_id,
    depth, state, confidence, min_resolution_method,
    evidence, root_kinds, observed_at, updated_at
) VALUES `

const upsertCodeReachabilityBatchSuffix = `
ON CONFLICT (scope_id, generation_id, repository_id, root_entity_id, entity_id) DO UPDATE
SET depth = EXCLUDED.depth,
    state = EXCLUDED.state,
    confidence = EXCLUDED.confidence,
    min_resolution_method = EXCLUDED.min_resolution_method,
    evidence = EXCLUDED.evidence,
    root_kinds = EXCLUDED.root_kinds,
    observed_at = EXCLUDED.observed_at,
    updated_at = EXCLUDED.updated_at
`

const deleteCodeReachabilityRepositoryRowsSQL = `
DELETE FROM code_reachability_rows
WHERE scope_id = $1
  AND generation_id = $2
  AND repository_id = $3
`

const upsertCodeReachabilityRepositoryWatermarkSQL = `
INSERT INTO code_reachability_repository_watermarks (
    scope_id, generation_id, repository_id, truncated, updated_at
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (scope_id, generation_id, repository_id) DO UPDATE
SET truncated = EXCLUDED.truncated,
    updated_at = EXCLUDED.updated_at
`

// CodeReachabilitySchemaSQL returns the DDL for code reachability rows and
// repository progress watermarks.
func CodeReachabilitySchemaSQL() string {
	return codeReachabilitySchemaSQL
}

// CodeReachabilityStore persists reducer-materialized code reachability rows.
type CodeReachabilityStore struct {
	db ExecQueryer
}

// NewCodeReachabilityStore creates a Postgres-backed code reachability store.
func NewCodeReachabilityStore(db ExecQueryer) *CodeReachabilityStore {
	return &CodeReachabilityStore{db: db}
}

// EnsureSchema applies the code reachability DDL.
func (s *CodeReachabilityStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, codeReachabilitySchemaSQL)
	return err
}

// Upsert writes code reachability rows in bounded batches.
func (s *CodeReachabilityStore) Upsert(ctx context.Context, rows []reducer.CodeReachabilityRow) error {
	if len(rows) == 0 {
		return nil
	}
	for i := 0; i < len(rows); i += codeReachabilityBatchSize {
		end := i + codeReachabilityBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := upsertCodeReachabilityBatch(ctx, s.db, rows[i:end]); err != nil {
			return err
		}
	}
	return nil
}

// ReplaceRepositoryRows replaces all reachability rows for one active
// repository generation with a freshly rebuilt deterministic snapshot and
// records the source-intent completion watermark that the snapshot covers.
func (s *CodeReachabilityStore) ReplaceRepositoryRows(
	ctx context.Context,
	scopeID string,
	generationID string,
	repositoryID string,
	rows []reducer.CodeReachabilityRow,
	watermark time.Time,
	truncated bool,
) error {
	scopeID = strings.TrimSpace(scopeID)
	generationID = strings.TrimSpace(generationID)
	repositoryID = strings.TrimSpace(repositoryID)
	if scopeID == "" || generationID == "" || repositoryID == "" {
		return fmt.Errorf("code reachability replacement requires scope_id, generation_id, and repository_id")
	}
	if watermark.IsZero() {
		return fmt.Errorf("code reachability replacement requires a non-zero watermark")
	}
	if beginner, ok := s.db.(Beginner); ok {
		tx, err := beginner.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin code reachability replacement: %w", err)
		}
		if err := replaceCodeReachabilityRepositoryRows(ctx, tx, scopeID, generationID, repositoryID, rows, watermark.UTC(), truncated); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit code reachability replacement: %w", err)
		}
		return nil
	}
	return replaceCodeReachabilityRepositoryRows(ctx, s.db, scopeID, generationID, repositoryID, rows, watermark.UTC(), truncated)
}

// ListLatestByEntities returns the strongest active-generation reachability row
// for each requested entity in one repository.
func (s *CodeReachabilityStore) ListLatestByEntities(
	ctx context.Context,
	repositoryID string,
	entityIDs []string,
) (map[string]reducer.CodeReachabilityRow, error) {
	repositoryID = strings.TrimSpace(repositoryID)
	entityIDs = cleanCodeReachabilityEntityIDs(entityIDs)
	if repositoryID == "" || len(entityIDs) == 0 {
		return map[string]reducer.CodeReachabilityRow{}, nil
	}

	query, args := buildListLatestCodeReachabilityByEntitiesQuery(repositoryID, entityIDs)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query latest code reachability rows: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]reducer.CodeReachabilityRow, len(entityIDs))
	for rows.Next() {
		row, err := scanCodeReachabilityRow(rows)
		if err != nil {
			return nil, err
		}
		if existing, ok := result[row.EntityID]; ok && strongerCodeReachabilityRow(existing, row) {
			continue
		}
		result[row.EntityID] = row
	}
	return result, rows.Err()
}

func upsertCodeReachabilityBatch(ctx context.Context, db ExecQueryer, rows []reducer.CodeReachabilityRow) error {
	values := make([]string, 0, len(rows))
	args := make([]any, 0, len(rows)*codeReachabilityColumns)
	for _, row := range rows {
		evidence, err := json.Marshal(row.Evidence)
		if err != nil {
			return fmt.Errorf("marshal code reachability evidence: %w", err)
		}
		rootKinds, err := json.Marshal(row.RootKinds)
		if err != nil {
			return fmt.Errorf("marshal code reachability root kinds: %w", err)
		}
		base := len(args)
		placeholders := make([]string, 0, codeReachabilityColumns)
		for i := 1; i <= codeReachabilityColumns; i++ {
			placeholders = append(placeholders, fmt.Sprintf("$%d", base+i))
		}
		values = append(values, "("+strings.Join(placeholders, ", ")+")")
		args = append(
			args,
			row.ScopeID,
			row.GenerationID,
			row.RepositoryID,
			row.RootEntityID,
			row.EntityID,
			row.Depth,
			row.State,
			row.Confidence,
			row.MinResolutionMethod,
			evidence,
			rootKinds,
			row.ObservedAt,
			row.UpdatedAt,
		)
	}

	query := upsertCodeReachabilityBatchPrefix + strings.Join(values, ", ") + upsertCodeReachabilityBatchSuffix
	_, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("upsert code reachability rows: %w", err)
	}
	return nil
}

func replaceCodeReachabilityRepositoryRows(
	ctx context.Context,
	db ExecQueryer,
	scopeID string,
	generationID string,
	repositoryID string,
	rows []reducer.CodeReachabilityRow,
	watermark time.Time,
	truncated bool,
) error {
	if _, err := db.ExecContext(ctx, deleteCodeReachabilityRepositoryRowsSQL, scopeID, generationID, repositoryID); err != nil {
		return fmt.Errorf("delete code reachability rows: %w", err)
	}
	if len(rows) > 0 {
		for i := 0; i < len(rows); i += codeReachabilityBatchSize {
			end := i + codeReachabilityBatchSize
			if end > len(rows) {
				end = len(rows)
			}
			if err := upsertCodeReachabilityBatch(ctx, db, rows[i:end]); err != nil {
				return err
			}
		}
	}
	if _, err := db.ExecContext(ctx, upsertCodeReachabilityRepositoryWatermarkSQL, scopeID, generationID, repositoryID, truncated, watermark); err != nil {
		return fmt.Errorf("upsert code reachability watermark: %w", err)
	}
	return nil
}

func buildListLatestCodeReachabilityByEntitiesQuery(repositoryID string, entityIDs []string) (string, []any) {
	args := make([]any, 0, len(entityIDs)+1)
	args = append(args, repositoryID)
	placeholders := make([]string, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		args = append(args, entityID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	query := `
SELECT row.scope_id, row.generation_id, row.repository_id, row.root_entity_id,
       row.entity_id, row.depth, row.state, row.confidence,
       row.min_resolution_method, row.evidence, row.root_kinds,
       row.observed_at, row.updated_at
FROM code_reachability_rows AS row
JOIN ingestion_scopes AS scope
  ON scope.scope_id = row.scope_id
 AND scope.active_generation_id = row.generation_id
JOIN scope_generations AS generation
  ON generation.generation_id = row.generation_id
 AND generation.status = 'active'
WHERE row.repository_id = $1
  AND row.entity_id IN (` + strings.Join(placeholders, ", ") + `)
ORDER BY row.entity_id ASC, row.confidence DESC, row.depth ASC, row.root_entity_id ASC
`
	return query, args
}

func scanCodeReachabilityRow(rows Rows) (reducer.CodeReachabilityRow, error) {
	var row reducer.CodeReachabilityRow
	var evidence []byte
	var rootKinds []byte
	if err := rows.Scan(
		&row.ScopeID,
		&row.GenerationID,
		&row.RepositoryID,
		&row.RootEntityID,
		&row.EntityID,
		&row.Depth,
		&row.State,
		&row.Confidence,
		&row.MinResolutionMethod,
		&evidence,
		&rootKinds,
		&row.ObservedAt,
		&row.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return reducer.CodeReachabilityRow{}, err
		}
		return reducer.CodeReachabilityRow{}, fmt.Errorf("scan code reachability row: %w", err)
	}
	if err := json.Unmarshal(evidence, &row.Evidence); err != nil {
		return reducer.CodeReachabilityRow{}, fmt.Errorf("unmarshal code reachability evidence: %w", err)
	}
	if err := json.Unmarshal(rootKinds, &row.RootKinds); err != nil {
		return reducer.CodeReachabilityRow{}, fmt.Errorf("unmarshal code reachability root kinds: %w", err)
	}
	return row, nil
}

func cleanCodeReachabilityEntityIDs(entityIDs []string) []string {
	seen := make(map[string]struct{}, len(entityIDs))
	cleaned := make([]string, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		entityID = strings.TrimSpace(entityID)
		if entityID == "" {
			continue
		}
		if _, ok := seen[entityID]; ok {
			continue
		}
		seen[entityID] = struct{}{}
		cleaned = append(cleaned, entityID)
	}
	return cleaned
}

func strongerCodeReachabilityRow(left, right reducer.CodeReachabilityRow) bool {
	if left.Confidence != right.Confidence {
		return left.Confidence > right.Confidence
	}
	if left.Depth != right.Depth {
		return left.Depth < right.Depth
	}
	return left.RootEntityID < right.RootEntityID
}

func codeReachabilityBootstrapDefinition() Definition {
	return Definition{
		Name: "code_reachability",
		Path: "schema/data-plane/postgres/027_code_reachability.sql",
		SQL:  codeReachabilitySchemaSQL,
	}
}
