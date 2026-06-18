package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// EshuSearchVectorBuildState is the low-cardinality lifecycle state for
// persisted search vector metadata.
type EshuSearchVectorBuildState string

const (
	// EshuSearchVectorBuildStateDisabled means vector retrieval is explicitly off.
	EshuSearchVectorBuildStateDisabled EshuSearchVectorBuildState = "disabled"
	// EshuSearchVectorBuildStateQueued means vector metadata is waiting for build work.
	EshuSearchVectorBuildStateQueued EshuSearchVectorBuildState = "queued"
	// EshuSearchVectorBuildStateBuilding means vector metadata is currently building.
	EshuSearchVectorBuildStateBuilding EshuSearchVectorBuildState = "building"
	// EshuSearchVectorBuildStateReady means vector metadata is ready for retrieval.
	EshuSearchVectorBuildStateReady EshuSearchVectorBuildState = "ready"
	// EshuSearchVectorBuildStateFailed means vector metadata failed with a bounded class.
	EshuSearchVectorBuildStateFailed EshuSearchVectorBuildState = "failed"
	// EshuSearchVectorBuildStateStale means vector metadata needs a rebuild.
	EshuSearchVectorBuildStateStale EshuSearchVectorBuildState = "stale"
)

const eshuSearchVectorMetadataSchemaSQL = `
CREATE TABLE IF NOT EXISTS eshu_search_vector_metadata (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_id TEXT NOT NULL,
    provider_profile_id TEXT NOT NULL DEFAULT 'local',
    source_class TEXT NOT NULL DEFAULT 'search_documents',
    embedding_model_id TEXT NOT NULL,
    embedding_dimensions INTEGER NOT NULL CHECK (embedding_dimensions > 0),
    embedding_content_hash TEXT NOT NULL,
    vector_index_version TEXT NOT NULL,
    build_state TEXT NOT NULL CHECK (build_state IN ('disabled', 'queued', 'building', 'ready', 'failed', 'stale')),
    failure_class TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    last_success_at TIMESTAMPTZ NULL,
    PRIMARY KEY (scope_id, generation_id, document_id, provider_profile_id, source_class, embedding_model_id, vector_index_version)
);

ALTER TABLE eshu_search_vector_metadata
    ADD COLUMN IF NOT EXISTS provider_profile_id TEXT NOT NULL DEFAULT 'local';

ALTER TABLE eshu_search_vector_metadata
    ADD COLUMN IF NOT EXISTS source_class TEXT NOT NULL DEFAULT 'search_documents';

ALTER TABLE eshu_search_vector_metadata
    DROP CONSTRAINT IF EXISTS eshu_search_vector_metadata_pkey;

ALTER TABLE eshu_search_vector_metadata
    ADD CONSTRAINT eshu_search_vector_metadata_pkey
    PRIMARY KEY (scope_id, generation_id, document_id, provider_profile_id, source_class, embedding_model_id, vector_index_version);

CREATE INDEX IF NOT EXISTS eshu_search_vector_metadata_state_idx
    ON eshu_search_vector_metadata (scope_id, generation_id, build_state);

CREATE INDEX IF NOT EXISTS eshu_search_vector_metadata_model_idx
    ON eshu_search_vector_metadata (scope_id, generation_id, embedding_model_id, vector_index_version);

CREATE INDEX IF NOT EXISTS eshu_search_vector_metadata_model_v2_idx
    ON eshu_search_vector_metadata (scope_id, generation_id, provider_profile_id, source_class, embedding_model_id, vector_index_version);
`

const upsertEshuSearchVectorMetadataSQL = `
INSERT INTO eshu_search_vector_metadata (
    scope_id,
    generation_id,
    document_id,
    provider_profile_id,
    source_class,
    embedding_model_id,
    embedding_dimensions,
    embedding_content_hash,
    vector_index_version,
    build_state,
    failure_class,
    created_at,
    updated_at,
    last_success_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, ''), $12, $13, $14)
ON CONFLICT (scope_id, generation_id, document_id, provider_profile_id, source_class, embedding_model_id, vector_index_version) DO UPDATE
SET embedding_dimensions = EXCLUDED.embedding_dimensions,
    embedding_content_hash = EXCLUDED.embedding_content_hash,
    build_state = EXCLUDED.build_state,
    failure_class = EXCLUDED.failure_class,
    updated_at = EXCLUDED.updated_at,
    last_success_at = EXCLUDED.last_success_at
`

const listActiveEshuSearchVectorMetadataSQL = `
SELECT
    meta.scope_id,
    meta.generation_id,
    meta.document_id,
    meta.provider_profile_id,
    meta.source_class,
    meta.embedding_model_id,
    meta.embedding_dimensions,
    meta.embedding_content_hash,
    meta.vector_index_version,
    meta.build_state,
    COALESCE(meta.failure_class, ''),
    meta.created_at,
    meta.updated_at,
    meta.last_success_at
FROM eshu_search_vector_metadata meta
JOIN ingestion_scopes scope
  ON scope.scope_id = meta.scope_id
 AND scope.active_generation_id = meta.generation_id
WHERE meta.scope_id = $1
  AND meta.provider_profile_id = $2
  AND meta.source_class = $3
  AND meta.embedding_model_id = $4
  AND meta.vector_index_version = $5
ORDER BY meta.document_id ASC
LIMIT $6
`

const eshuSearchVectorStatusSQL = `
SELECT
    scope.active_generation_id,
    meta.build_state,
    COUNT(*)::bigint,
    MAX(meta.updated_at),
    MAX(meta.last_success_at)
FROM eshu_search_vector_metadata meta
JOIN ingestion_scopes scope
  ON scope.scope_id = meta.scope_id
 AND scope.active_generation_id = meta.generation_id
WHERE meta.scope_id = $1
  AND meta.provider_profile_id = $2
  AND meta.source_class = $3
  AND meta.embedding_model_id = $4
  AND meta.vector_index_version = $5
GROUP BY scope.active_generation_id, meta.build_state
ORDER BY meta.build_state ASC
`

// EshuSearchVectorMetadata records one document's vector build metadata for a
// scope generation.
type EshuSearchVectorMetadata struct {
	ScopeID              string
	GenerationID         string
	DocumentID           string
	ProviderProfileID    string
	SourceClass          string
	EmbeddingModelID     string
	EmbeddingDimensions  int
	EmbeddingContentHash string
	VectorIndexVersion   string
	BuildState           EshuSearchVectorBuildState
	FailureClass         string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	LastSuccessAt        *time.Time
}

// EshuSearchVectorMetadataFilter bounds active vector metadata reads.
type EshuSearchVectorMetadataFilter struct {
	ScopeID            string
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
	DocumentIDs        []string
	Limit              int
}

// EshuSearchVectorStatusRequest identifies the active vector state aggregate to
// load for one scope, model, and index version.
type EshuSearchVectorStatusRequest struct {
	ScopeID            string
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
}

// EshuSearchVectorStatus summarizes low-cardinality vector build state for a
// scope's active generation.
type EshuSearchVectorStatus struct {
	ActiveGenerationID string
	StateCounts        map[EshuSearchVectorBuildState]int
	VectorCount        int
	LastUpdatedAt      *time.Time
	LastSuccessAt      *time.Time
}

// EshuSearchVectorMetadataStore persists vector metadata and reads active
// generation vector state without touching API/MCP runtime behavior.
type EshuSearchVectorMetadataStore struct {
	db ExecQueryer
}

// EshuSearchVectorMetadataSchemaSQL returns the Postgres DDL for vector
// metadata/build-state rows.
func EshuSearchVectorMetadataSchemaSQL() string {
	return eshuSearchVectorMetadataSchemaSQL
}

// NewEshuSearchVectorMetadataStore constructs the vector metadata store.
func NewEshuSearchVectorMetadataStore(db ExecQueryer) EshuSearchVectorMetadataStore {
	return EshuSearchVectorMetadataStore{db: db}
}

// Upsert inserts or updates one vector metadata row by its deterministic build
// identity.
func (s EshuSearchVectorMetadataStore) Upsert(ctx context.Context, row EshuSearchVectorMetadata) error {
	if s.db == nil {
		return fmt.Errorf("eshu search vector metadata database is required")
	}
	row = normalizeEshuSearchVectorMetadata(row)
	if err := validateEshuSearchVectorMetadata(row); err != nil {
		return err
	}

	var lastSuccess any
	if row.LastSuccessAt != nil {
		lastSuccess = *row.LastSuccessAt
	}
	_, err := s.db.ExecContext(
		ctx,
		upsertEshuSearchVectorMetadataSQL,
		row.ScopeID,
		row.GenerationID,
		row.DocumentID,
		row.ProviderProfileID,
		row.SourceClass,
		row.EmbeddingModelID,
		row.EmbeddingDimensions,
		row.EmbeddingContentHash,
		row.VectorIndexVersion,
		string(row.BuildState),
		row.FailureClass,
		row.CreatedAt,
		row.UpdatedAt,
		lastSuccess,
	)
	if err != nil {
		return fmt.Errorf("upsert eshu search vector metadata: %w", err)
	}
	return nil
}

// ListActive returns vector metadata rows for the requested scope's active
// generation only.
func (s EshuSearchVectorMetadataStore) ListActive(
	ctx context.Context,
	filter EshuSearchVectorMetadataFilter,
) ([]EshuSearchVectorMetadata, error) {
	if s.db == nil {
		return nil, fmt.Errorf("eshu search vector metadata database is required")
	}
	filter = normalizeEshuSearchVectorMetadataFilter(filter)
	if err := validateEshuSearchVectorMetadataFilter(filter); err != nil {
		return nil, err
	}

	query := listActiveEshuSearchVectorMetadataSQL
	args := []any{filter.ScopeID, filter.ProviderProfileID, filter.SourceClass, filter.EmbeddingModelID, filter.VectorIndexVersion}
	if len(filter.DocumentIDs) > 0 {
		query = strings.Replace(query, "\nORDER BY meta.document_id ASC", "\n  AND meta.document_id = ANY($6)\nORDER BY meta.document_id ASC", 1)
		args = append(args, pq.Array(filter.DocumentIDs))
		query = strings.Replace(query, "LIMIT $6", "LIMIT $7", 1)
	}
	args = append(args, filter.Limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list active eshu search vector metadata: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []EshuSearchVectorMetadata
	for rows.Next() {
		row, err := scanEshuSearchVectorMetadata(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active eshu search vector metadata: %w", err)
	}
	return results, nil
}

// Status returns low-cardinality vector build-state counts for the active
// generation.
func (s EshuSearchVectorMetadataStore) Status(
	ctx context.Context,
	req EshuSearchVectorStatusRequest,
) (EshuSearchVectorStatus, error) {
	if s.db == nil {
		return EshuSearchVectorStatus{}, fmt.Errorf("eshu search vector metadata database is required")
	}
	req = normalizeEshuSearchVectorStatusRequest(req)
	if err := validateEshuSearchVectorStatusRequest(req); err != nil {
		return EshuSearchVectorStatus{}, err
	}

	rows, err := s.db.QueryContext(
		ctx,
		eshuSearchVectorStatusSQL,
		req.ScopeID,
		req.ProviderProfileID,
		req.SourceClass,
		req.EmbeddingModelID,
		req.VectorIndexVersion,
	)
	if err != nil {
		return EshuSearchVectorStatus{}, fmt.Errorf("load eshu search vector status: %w", err)
	}
	defer func() { _ = rows.Close() }()

	status := EshuSearchVectorStatus{StateCounts: map[EshuSearchVectorBuildState]int{}}
	for rows.Next() {
		var generationID string
		var stateText string
		var count int64
		var updatedAt time.Time
		var lastSuccess sql.NullTime
		if err := rows.Scan(&generationID, &stateText, &count, &updatedAt, &lastSuccess); err != nil {
			return EshuSearchVectorStatus{}, fmt.Errorf("scan eshu search vector status: %w", err)
		}
		state := EshuSearchVectorBuildState(stateText)
		status.ActiveGenerationID = generationID
		status.StateCounts[state] += int(count)
		status.VectorCount += int(count)
		if status.LastUpdatedAt == nil || updatedAt.After(*status.LastUpdatedAt) {
			candidate := updatedAt
			status.LastUpdatedAt = &candidate
		}
		if lastSuccess.Valid && (status.LastSuccessAt == nil || lastSuccess.Time.After(*status.LastSuccessAt)) {
			candidate := lastSuccess.Time
			status.LastSuccessAt = &candidate
		}
	}
	if err := rows.Err(); err != nil {
		return EshuSearchVectorStatus{}, fmt.Errorf("iterate eshu search vector status: %w", err)
	}
	return status, nil
}

func scanEshuSearchVectorMetadata(rows Rows) (EshuSearchVectorMetadata, error) {
	var row EshuSearchVectorMetadata
	var stateText string
	var failureClass string
	var lastSuccess sql.NullTime
	var dimensions int64
	if err := rows.Scan(
		&row.ScopeID,
		&row.GenerationID,
		&row.DocumentID,
		&row.ProviderProfileID,
		&row.SourceClass,
		&row.EmbeddingModelID,
		&dimensions,
		&row.EmbeddingContentHash,
		&row.VectorIndexVersion,
		&stateText,
		&failureClass,
		&row.CreatedAt,
		&row.UpdatedAt,
		&lastSuccess,
	); err != nil {
		return EshuSearchVectorMetadata{}, fmt.Errorf("scan eshu search vector metadata: %w", err)
	}
	row.EmbeddingDimensions = int(dimensions)
	row.BuildState = EshuSearchVectorBuildState(stateText)
	row.FailureClass = failureClass
	if lastSuccess.Valid {
		row.LastSuccessAt = &lastSuccess.Time
	}
	return row, nil
}
