// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"
)

const eshuSearchDocumentProjectionStateSchemaSQL = `
CREATE TABLE IF NOT EXISTS eshu_search_document_projection_state (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    projection_revision BIGINT NOT NULL,
    build_fence BIGINT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('building','ready','failed')),
    document_count BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);
`

const beginBuildingProjectionStateSQL = `
INSERT INTO eshu_search_document_projection_state
  (scope_id, generation_id, projection_revision, build_fence, state, document_count, updated_at)
VALUES ($1, $2, 1, 1, 'building', 0, $3)
ON CONFLICT (scope_id, generation_id) DO UPDATE SET
  projection_revision = eshu_search_document_projection_state.projection_revision + 1,
  build_fence = eshu_search_document_projection_state.build_fence + 1,
  state = 'building',
  updated_at = $3
RETURNING projection_revision, build_fence
`

const finalizeReadyProjectionStateSQL = `
UPDATE eshu_search_document_projection_state
SET state = 'ready', document_count = $4, updated_at = $5
WHERE scope_id = $1
  AND generation_id = (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1)
  AND projection_revision = $2
  AND build_fence <= $3
`

const markFailedProjectionStateSQL = `
UPDATE eshu_search_document_projection_state
SET state = 'failed', updated_at = $4
WHERE scope_id = $1
  AND generation_id = (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1)
  AND projection_revision = $2
  AND build_fence <= $3
`

// EshuSearchDocumentProjectionState records the projection lifecycle of
// eshu_search_document facts for one scope generation. It is the per-scope
// scheduler gate introduced in #4233 that replaces the O(corpus) active_docs
// CTE with an O(active scopes) scope-state scan.
type EshuSearchDocumentProjectionState struct {
	ScopeID            string
	GenerationID       string
	ProjectionRevision int64
	BuildFence         int64
	State              string
	DocumentCount      int64
	UpdatedAt          time.Time
}

// EshuSearchDocumentProjectionStateStore persists projection-state rows and
// provides the BeginBuilding / FinalizeReady / MarkFailed CAS lifecycle.
type EshuSearchDocumentProjectionStateStore struct {
	db ExecQueryer
}

// EshuSearchDocumentProjectionStateSchemaSQL returns the Postgres DDL for
// the eshu_search_document_projection_state table.
func EshuSearchDocumentProjectionStateSchemaSQL() string {
	return eshuSearchDocumentProjectionStateSchemaSQL
}

// NewEshuSearchDocumentProjectionStateStore constructs the projection-state store.
func NewEshuSearchDocumentProjectionStateStore(db ExecQueryer) EshuSearchDocumentProjectionStateStore {
	return EshuSearchDocumentProjectionStateStore{db: db}
}

// BeginBuilding starts (or re-starts) a projection build for the given
// scope+generation. It bumps projection_revision and build_fence on every call
// and returns the resulting revision and fence values.
func (s EshuSearchDocumentProjectionStateStore) BeginBuilding(
	ctx context.Context,
	scopeID, generationID string,
) (revision, fence int64, err error) {
	if s.db == nil {
		return 0, 0, fmt.Errorf("eshu search document projection state store requires a database")
	}
	if scopeID == "" {
		return 0, 0, fmt.Errorf("eshu search document projection state begin building requires scope id")
	}
	if generationID == "" {
		return 0, 0, fmt.Errorf("eshu search document projection state begin building requires generation id")
	}

	now := time.Now().UTC()
	rows, err := s.db.QueryContext(
		ctx,
		beginBuildingProjectionStateSQL,
		scopeID,
		generationID,
		now,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("begin building eshu search document projection state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, 0, fmt.Errorf("begin building eshu search document projection state: no row returned")
	}
	if err := rows.Scan(&revision, &fence); err != nil {
		return 0, 0, fmt.Errorf("begin building eshu search document projection state: %w", err)
	}
	return revision, fence, rows.Err()
}

// FinalizeReady publishes the projection state as ready for a scope+generation
// when the build fence is still current. It returns true iff exactly one row
// was updated (the CAS succeeded). A false result means the fence or revision
// was stale/superseded.
func (s EshuSearchDocumentProjectionStateStore) FinalizeReady(
	ctx context.Context,
	scopeID, generationID string,
	revision, fence, documentCount int64,
) (bool, error) {
	if s.db == nil {
		return false, fmt.Errorf("eshu search document projection state store requires a database")
	}
	if scopeID == "" {
		return false, fmt.Errorf("eshu search document projection state finalize ready requires scope id")
	}
	if generationID == "" {
		return false, fmt.Errorf("eshu search document projection state finalize ready requires generation id")
	}

	now := time.Now().UTC()
	result, err := s.db.ExecContext(
		ctx,
		finalizeReadyProjectionStateSQL,
		scopeID,
		revision,
		fence,
		documentCount,
		now,
	)
	if err != nil {
		return false, fmt.Errorf("finalize ready eshu search document projection state: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected eshu search document projection state finalize ready: %w", err)
	}
	return affected == 1, nil
}

// MarkFailed marks the projection state as failed when the build fence is
// still current. It returns true iff exactly one row was updated (the CAS
// succeeded).
func (s EshuSearchDocumentProjectionStateStore) MarkFailed(
	ctx context.Context,
	scopeID, generationID string,
	revision, fence int64,
) (bool, error) {
	if s.db == nil {
		return false, fmt.Errorf("eshu search document projection state store requires a database")
	}
	if scopeID == "" {
		return false, fmt.Errorf("eshu search document projection state mark failed requires scope id")
	}
	if generationID == "" {
		return false, fmt.Errorf("eshu search document projection state mark failed requires generation id")
	}

	now := time.Now().UTC()
	result, err := s.db.ExecContext(
		ctx,
		markFailedProjectionStateSQL,
		scopeID,
		revision,
		fence,
		now,
	)
	if err != nil {
		return false, fmt.Errorf("mark failed eshu search document projection state: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected eshu search document projection state mark failed: %w", err)
	}
	return affected == 1, nil
}
