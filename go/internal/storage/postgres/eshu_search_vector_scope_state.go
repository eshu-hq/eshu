// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"
)

const eshuSearchVectorScopeStateSchemaSQL = `
CREATE TABLE IF NOT EXISTS eshu_search_vector_scope_state (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    provider_profile_id TEXT NOT NULL,
    source_class TEXT NOT NULL,
    embedding_model_id TEXT NOT NULL,
    vector_index_version TEXT NOT NULL,
    projection_revision BIGINT NOT NULL,
    build_fence BIGINT NOT NULL,
	document_cursor TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK (state IN ('building','ready','failed')),
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, provider_profile_id, source_class, embedding_model_id, vector_index_version)
);
ALTER TABLE eshu_search_vector_scope_state
    ADD COLUMN IF NOT EXISTS document_cursor TEXT NOT NULL DEFAULT '';
`

const beginBuildingVectorScopeStateSQL = `
INSERT INTO eshu_search_vector_scope_state
  (scope_id, generation_id, provider_profile_id, source_class, embedding_model_id,
   vector_index_version, projection_revision, build_fence, document_cursor, state, updated_at)
SELECT $1, $2, $3, $4, $5, $6, $7, 1, '', 'building', $8
FROM eshu_search_document_projection_state projection
JOIN ingestion_scopes scope
  ON scope.scope_id = projection.scope_id
 AND scope.active_generation_id = projection.generation_id
WHERE projection.scope_id = $1
  AND projection.generation_id = $2
  AND projection.state = 'ready'
  AND projection.projection_revision = $7
ON CONFLICT (scope_id, generation_id, provider_profile_id, source_class, embedding_model_id, vector_index_version) DO UPDATE SET
  projection_revision = EXCLUDED.projection_revision,
  build_fence = COALESCE(eshu_search_vector_scope_state.build_fence, 0) + 1,
  document_cursor = CASE
    WHEN EXCLUDED.projection_revision > eshu_search_vector_scope_state.projection_revision THEN ''
    ELSE eshu_search_vector_scope_state.document_cursor
  END,
  state = 'building',
  updated_at = $8
WHERE EXCLUDED.projection_revision > eshu_search_vector_scope_state.projection_revision
   OR (
     EXCLUDED.projection_revision = eshu_search_vector_scope_state.projection_revision
     AND eshu_search_vector_scope_state.state <> 'ready'
   )
RETURNING build_fence
`

const finalizeReadyVectorScopeStateSQL = `
UPDATE eshu_search_vector_scope_state
SET state = 'ready', updated_at = $9
WHERE scope_id = $1
  AND generation_id = $2
  AND provider_profile_id = $3
  AND source_class = $4
  AND embedding_model_id = $5
  AND vector_index_version = $6
  AND generation_id = (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1)
  AND projection_revision = $7
  AND build_fence = $8
  AND EXISTS (
    SELECT 1
    FROM eshu_search_document_projection_state projection
    WHERE projection.scope_id = $1
      AND projection.generation_id = $2
      AND projection.state = 'ready'
      AND projection.projection_revision = $7
  )
`

// listPendingSearchVectorScopesScopedSQL is the #4233 replacement for the
// old corpus-wide active_docs CTE. It scans O(active scopes) by joining
// eshu_search_document_projection_state with ingestion_scopes and then
// anti-joining eshu_search_vector_scope_state to find ready projection
// scopes whose vector rows are missing, stale, or on a different revision.
const listPendingSearchVectorScopesScopedSQL = `
SELECT ps.scope_id, ps.generation_id, COALESCE(s.payload->>'repo_id','') AS repo_id, ps.projection_revision,
       CASE WHEN vs.projection_revision = ps.projection_revision THEN vs.document_cursor ELSE '' END AS document_cursor
FROM eshu_search_document_projection_state ps
JOIN ingestion_scopes s ON s.scope_id=ps.scope_id AND s.active_generation_id=ps.generation_id
LEFT JOIN eshu_search_vector_scope_state vs
  ON vs.scope_id=ps.scope_id AND vs.generation_id=ps.generation_id
 AND vs.provider_profile_id=$1 AND vs.source_class=$2 AND vs.embedding_model_id=$3 AND vs.vector_index_version=$4
WHERE s.scope_kind='repository' AND ps.state='ready' AND ps.document_count > 0
  AND (vs.state IS NULL OR vs.state <> 'ready' OR vs.projection_revision <> ps.projection_revision)
ORDER BY ps.scope_id
LIMIT $5
`

// scopeVectorCompleteSQL checks whether every persisted search-index document
// for one active scope+generation already has a
// complete vector row (metadata with matching hash plus a value row, or
// metadata in disabled state). It returns true when no incomplete document
// remains — the per-scope gate the reducer calls before publishing ready.
//
// A cheap indexed count gate returns false while terminal metadata is still
// below the projection count. Once the count can be complete, the exact branch
// reuses the indexed pending-document predicate and stops on the first gap.
// This preserves stale-hash, ready-without-value, disabled, and retired-extra
// correctness without materializing and sorting three scope relations (#5063).
const scopeVectorCompleteSQL = `
WITH completion_gate AS MATERIALIZED (
    SELECT
        ps.document_count,
        (
            SELECT COUNT(*)
            FROM eshu_search_vector_metadata meta
            WHERE meta.scope_id = $1
              AND meta.generation_id = $2
              AND meta.provider_profile_id = $3
              AND meta.source_class = $4
              AND meta.embedding_model_id = $5
              AND meta.vector_index_version = $6
              AND meta.build_state IN ('ready', 'disabled')
        ) AS terminal_count
    FROM eshu_search_document_projection_state ps
    WHERE ps.scope_id = $1
      AND ps.generation_id = $2
      AND ps.state = 'ready'
)
SELECT CASE
    WHEN gate.terminal_count < gate.document_count THEN FALSE
    ELSE NOT EXISTS (
        SELECT 1
        FROM eshu_search_index_documents doc
        JOIN ingestion_scopes scope
          ON scope.scope_id = doc.scope_id
         AND scope.active_generation_id = doc.generation_id
        WHERE doc.scope_id = $1
          AND doc.generation_id = $2
          AND NOT EXISTS (
              SELECT 1
              FROM eshu_search_vector_metadata meta
              LEFT JOIN eshu_search_vector_values value
                ON value.scope_id = meta.scope_id
               AND value.generation_id = meta.generation_id
               AND value.document_id = meta.document_id
               AND value.provider_profile_id = meta.provider_profile_id
               AND value.source_class = meta.source_class
               AND value.embedding_model_id = meta.embedding_model_id
               AND value.vector_index_version = meta.vector_index_version
               AND value.embedding_content_hash = meta.embedding_content_hash
              WHERE meta.scope_id = doc.scope_id
                AND meta.generation_id = doc.generation_id
                AND meta.document_id = doc.document_id
                AND meta.provider_profile_id = $3
                AND meta.source_class = $4
                AND meta.embedding_model_id = $5
                AND meta.vector_index_version = $6
                AND meta.embedding_content_hash = doc.content_hash
                AND (meta.build_state = 'disabled'
                     OR (meta.build_state = 'ready' AND value.document_id IS NOT NULL))
              -- OFFSET 0 prevents the planner from un-nesting this NOT EXISTS into a full-scope anti-join (#5063).
              OFFSET 0
          )
    )
END AS complete
FROM completion_gate gate
`

// EshuSearchVectorIdentity groups the four non-scope attributes that
// identify one vector build identity: provider profile, source class,
// embedding model, and vector index version.
type EshuSearchVectorIdentity struct {
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
}

// EshuSearchVectorScopeState records the vector build lifecycle for one
// scope+generation+identity tuple. It is the per-scope versioned state
// introduced in #4233 that lets the scheduler skip corpus-wide fact
// enumeration.
type EshuSearchVectorScopeState struct {
	ScopeID            string
	GenerationID       string
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
	ProjectionRevision int64
	BuildFence         int64
	DocumentCursor     string
	State              string
	UpdatedAt          time.Time
}

// EshuSearchVectorScopeStateStore persists per-scope vector build state and
// provides the #4233 bounded ListPendingSearchVectorScopes query.
type EshuSearchVectorScopeStateStore struct {
	db ExecQueryer
}

// EshuSearchVectorScopeStateSchemaSQL returns the Postgres DDL for the
// eshu_search_vector_scope_state table.
func EshuSearchVectorScopeStateSchemaSQL() string {
	return eshuSearchVectorScopeStateSchemaSQL
}

// NewEshuSearchVectorScopeStateStore constructs the vector scope state store.
func NewEshuSearchVectorScopeStateStore(db ExecQueryer) EshuSearchVectorScopeStateStore {
	return EshuSearchVectorScopeStateStore{db: db}
}

// BeginBuilding starts or re-starts a vector build for the given
// scope+generation+identity. It accepts only the active ready projection,
// rejects older or already-ready revisions, bumps build_fence for a current
// retry, and returns the resulting fence value.
func (s EshuSearchVectorScopeStateStore) BeginBuilding(
	ctx context.Context,
	scopeID, generationID string,
	identity EshuSearchVectorIdentity,
	projectionRevision int64,
) (fence int64, err error) {
	if s.db == nil {
		return 0, fmt.Errorf("eshu search vector scope state store requires a database")
	}
	if scopeID == "" {
		return 0, fmt.Errorf("eshu search vector scope state begin building requires scope id")
	}
	if generationID == "" {
		return 0, fmt.Errorf("eshu search vector scope state begin building requires generation id")
	}

	now := time.Now().UTC()
	rows, err := s.db.QueryContext(
		ctx,
		beginBuildingVectorScopeStateSQL,
		scopeID,
		generationID,
		identity.ProviderProfileID,
		identity.SourceClass,
		identity.EmbeddingModelID,
		identity.VectorIndexVersion,
		projectionRevision,
		now,
	)
	if err != nil {
		return 0, fmt.Errorf("begin building eshu search vector scope state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, fmt.Errorf("begin building eshu search vector scope state: no row returned")
	}
	if err := rows.Scan(&fence); err != nil {
		return 0, fmt.Errorf("begin building eshu search vector scope state: %w", err)
	}
	return fence, rows.Err()
}

// FinalizeReady publishes the vector scope state as ready when the build
// fence and projection revision are still current. It returns true iff
// exactly one row was updated (the CAS succeeded).
func (s EshuSearchVectorScopeStateStore) FinalizeReady(
	ctx context.Context,
	scopeID, generationID string,
	identity EshuSearchVectorIdentity,
	projectionRevision, fence int64,
) (bool, error) {
	if s.db == nil {
		return false, fmt.Errorf("eshu search vector scope state store requires a database")
	}
	if scopeID == "" {
		return false, fmt.Errorf("eshu search vector scope state finalize ready requires scope id")
	}
	if generationID == "" {
		return false, fmt.Errorf("eshu search vector scope state finalize ready requires generation id")
	}

	now := time.Now().UTC()
	result, err := s.db.ExecContext(
		ctx,
		finalizeReadyVectorScopeStateSQL,
		scopeID,
		generationID,
		identity.ProviderProfileID,
		identity.SourceClass,
		identity.EmbeddingModelID,
		identity.VectorIndexVersion,
		projectionRevision,
		fence,
		now,
	)
	if err != nil {
		return false, fmt.Errorf("finalize ready eshu search vector scope state: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected eshu search vector scope state finalize ready: %w", err)
	}
	return affected == 1, nil
}

// ListPendingSearchVectorScopes returns active repository scopes that have
// ready projection_state rows but whose vector_scope_state is missing,
// not ready, or on a stale projection revision. This is the #4233
// O(active scopes) replacement for the old corpus-wide active_docs CTE.
func (s EshuSearchVectorScopeStateStore) ListPendingSearchVectorScopes(
	ctx context.Context,
	req EshuSearchVectorPendingRequest,
) ([]EshuSearchVectorPendingScope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("eshu search vector scope state store requires a database")
	}
	if req.EmbeddingModelID == "" {
		return nil, fmt.Errorf("eshu search vector pending request requires embedding model id")
	}
	if req.ProviderProfileID == "" {
		return nil, fmt.Errorf("eshu search vector pending request requires provider profile id")
	}
	if req.SourceClass == "" {
		return nil, fmt.Errorf("eshu search vector pending request requires source class")
	}
	if req.VectorIndexVersion == "" {
		return nil, fmt.Errorf("eshu search vector pending request requires vector index version")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = eshuSearchVectorPendingDefaultLimit
	}
	if limit > eshuSearchVectorPendingMaxLimit {
		limit = eshuSearchVectorPendingMaxLimit
	}

	rows, err := s.db.QueryContext(
		ctx,
		listPendingSearchVectorScopesScopedSQL,
		req.ProviderProfileID,
		req.SourceClass,
		req.EmbeddingModelID,
		req.VectorIndexVersion,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending eshu search vector scopes (scoped): %w", err)
	}
	defer func() { _ = rows.Close() }()

	var scopes []EshuSearchVectorPendingScope
	for rows.Next() {
		var pending EshuSearchVectorPendingScope
		if err := rows.Scan(&pending.ScopeID, &pending.GenerationID, &pending.RepoID, &pending.ProjectionRevision, &pending.DocumentCursor); err != nil {
			return nil, fmt.Errorf("scan pending eshu search vector scope: %w", err)
		}
		scopes = append(scopes, pending)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending eshu search vector scopes: %w", err)
	}
	return scopes, nil
}

// ScopeVectorComplete returns true iff every active persisted search document
// for the given scope+generation already has a
// complete vector row: metadata with matching content hash and either
// build_state='disabled' or ('ready' with a matching value row). This is
// the per-scope correctness gate the reducer calls before publishing ready.
func (s EshuSearchVectorScopeStateStore) ScopeVectorComplete(
	ctx context.Context,
	scopeID, generationID string,
	identity EshuSearchVectorIdentity,
) (bool, error) {
	if s.db == nil {
		return false, fmt.Errorf("eshu search vector scope state store requires a database")
	}
	if scopeID == "" {
		return false, fmt.Errorf("eshu search vector scope state scope vector complete requires scope id")
	}
	if generationID == "" {
		return false, fmt.Errorf("eshu search vector scope state scope vector complete requires generation id")
	}

	rows, err := beginSearchVectorDocumentQuery(
		ctx,
		s.db,
		scopeVectorCompleteSQL,
		scopeID,
		generationID,
		identity.ProviderProfileID,
		identity.SourceClass,
		identity.EmbeddingModelID,
		identity.VectorIndexVersion,
	)
	if err != nil {
		return false, fmt.Errorf("scope vector complete: %w", err)
	}
	defer rows.Rollback()
	if !rows.Next() {
		return false, rows.Err()
	}
	var complete bool
	if err := rows.Scan(&complete); err != nil {
		return false, fmt.Errorf("scope vector complete: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if err := rows.Commit(); err != nil {
		return false, fmt.Errorf("commit scope vector complete read: %w", err)
	}
	return complete, nil
}
