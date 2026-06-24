// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/lib/pq"
)

const maxEshuSearchVectorValues = 4096

const eshuSearchVectorValuesSchemaSQL = `
CREATE TABLE IF NOT EXISTS eshu_search_vector_values (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_id TEXT NOT NULL,
    provider_profile_id TEXT NOT NULL DEFAULT 'local',
    source_class TEXT NOT NULL DEFAULT 'search_documents',
    embedding_model_id TEXT NOT NULL,
    embedding_dimensions INTEGER NOT NULL CHECK (embedding_dimensions > 0 AND embedding_dimensions <= 4096),
    embedding_content_hash TEXT NOT NULL,
    vector_index_version TEXT NOT NULL,
    vector_values DOUBLE PRECISION[] NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, document_id, provider_profile_id, source_class, embedding_model_id, vector_index_version),
    CHECK (cardinality(vector_values) = embedding_dimensions)
);

ALTER TABLE eshu_search_vector_values
    ADD COLUMN IF NOT EXISTS provider_profile_id TEXT NOT NULL DEFAULT 'local';

ALTER TABLE eshu_search_vector_values
    ADD COLUMN IF NOT EXISTS source_class TEXT NOT NULL DEFAULT 'search_documents';

ALTER TABLE eshu_search_vector_values
    DROP CONSTRAINT IF EXISTS eshu_search_vector_values_pkey;

ALTER TABLE eshu_search_vector_values
    ADD CONSTRAINT eshu_search_vector_values_pkey
    PRIMARY KEY (scope_id, generation_id, document_id, provider_profile_id, source_class, embedding_model_id, vector_index_version);

CREATE INDEX IF NOT EXISTS eshu_search_vector_values_model_idx
    ON eshu_search_vector_values (scope_id, generation_id, embedding_model_id, vector_index_version, document_id);

CREATE INDEX IF NOT EXISTS eshu_search_vector_values_model_v2_idx
    ON eshu_search_vector_values (scope_id, generation_id, provider_profile_id, source_class, embedding_model_id, vector_index_version, document_id);
`

const upsertEshuSearchVectorValueSQL = `
INSERT INTO eshu_search_vector_values (
    scope_id,
    generation_id,
    document_id,
    provider_profile_id,
    source_class,
    embedding_model_id,
    embedding_dimensions,
    embedding_content_hash,
    vector_index_version,
    vector_values,
    created_at,
    updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (scope_id, generation_id, document_id, provider_profile_id, source_class, embedding_model_id, vector_index_version) DO UPDATE
SET embedding_dimensions = EXCLUDED.embedding_dimensions,
    embedding_content_hash = EXCLUDED.embedding_content_hash,
    vector_values = EXCLUDED.vector_values,
    updated_at = EXCLUDED.updated_at
`

const listActiveEshuSearchVectorValuesSQL = `
SELECT
    vec.scope_id,
    vec.generation_id,
    vec.document_id,
    vec.provider_profile_id,
    vec.source_class,
    vec.embedding_model_id,
    vec.embedding_dimensions,
    vec.embedding_content_hash,
    vec.vector_index_version,
    vec.vector_values,
    vec.created_at,
    vec.updated_at
FROM eshu_search_vector_values vec
JOIN ingestion_scopes scope
  ON scope.scope_id = vec.scope_id
 AND scope.active_generation_id = vec.generation_id
JOIN eshu_search_vector_metadata meta
  ON meta.scope_id = vec.scope_id
 AND meta.generation_id = vec.generation_id
 AND meta.document_id = vec.document_id
 AND meta.provider_profile_id = vec.provider_profile_id
 AND meta.source_class = vec.source_class
 AND meta.embedding_model_id = vec.embedding_model_id
 AND meta.vector_index_version = vec.vector_index_version
 AND meta.embedding_content_hash = vec.embedding_content_hash
 AND meta.build_state = 'ready'
WHERE vec.scope_id = $1
  AND vec.provider_profile_id = $2
  AND vec.source_class = $3
  AND vec.embedding_model_id = $4
  AND vec.vector_index_version = $5
ORDER BY vec.document_id ASC
LIMIT $6
`

// EshuSearchVectorValue records one derived embedding vector for a search
// document and embedding index version.
type EshuSearchVectorValue struct {
	ScopeID              string
	GenerationID         string
	DocumentID           string
	ProviderProfileID    string
	SourceClass          string
	EmbeddingModelID     string
	EmbeddingDimensions  int
	EmbeddingContentHash string
	VectorIndexVersion   string
	VectorValues         []float64
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// EshuSearchVectorValueFilter bounds active vector value reads.
type EshuSearchVectorValueFilter struct {
	ScopeID            string
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
	DocumentIDs        []string
	Limit              int
}

// EshuSearchVectorValueStore persists derived search-document vectors and reads
// active-generation vectors for one scope, model, and index version.
type EshuSearchVectorValueStore struct {
	db ExecQueryer
}

// EshuSearchVectorValuesSchemaSQL returns the Postgres DDL for vector payloads.
func EshuSearchVectorValuesSchemaSQL() string {
	return eshuSearchVectorValuesSchemaSQL
}

// NewEshuSearchVectorValueStore constructs the vector value store.
func NewEshuSearchVectorValueStore(db ExecQueryer) EshuSearchVectorValueStore {
	return EshuSearchVectorValueStore{db: db}
}

// Upsert inserts or updates one derived vector payload by deterministic build
// identity.
func (s EshuSearchVectorValueStore) Upsert(ctx context.Context, row EshuSearchVectorValue) error {
	if s.db == nil {
		return fmt.Errorf("eshu search vector value database is required")
	}
	row = normalizeEshuSearchVectorValue(row)
	if err := validateEshuSearchVectorValue(row); err != nil {
		return err
	}
	_, err := s.db.ExecContext(
		ctx,
		upsertEshuSearchVectorValueSQL,
		row.ScopeID,
		row.GenerationID,
		row.DocumentID,
		row.ProviderProfileID,
		row.SourceClass,
		row.EmbeddingModelID,
		row.EmbeddingDimensions,
		row.EmbeddingContentHash,
		row.VectorIndexVersion,
		row.VectorValues,
		row.CreatedAt,
		row.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert eshu search vector value: %w", err)
	}
	return nil
}

// ListActive returns vector payloads for the requested scope's active
// generation only.
func (s EshuSearchVectorValueStore) ListActive(
	ctx context.Context,
	filter EshuSearchVectorValueFilter,
) ([]EshuSearchVectorValue, error) {
	if s.db == nil {
		return nil, fmt.Errorf("eshu search vector value database is required")
	}
	filter = normalizeEshuSearchVectorValueFilter(filter)
	if err := validateEshuSearchVectorValueFilter(filter); err != nil {
		return nil, err
	}

	query := listActiveEshuSearchVectorValuesSQL
	args := []any{filter.ScopeID, filter.ProviderProfileID, filter.SourceClass, filter.EmbeddingModelID, filter.VectorIndexVersion}
	if len(filter.DocumentIDs) > 0 {
		query = strings.Replace(query, "\nORDER BY vec.document_id ASC", "\n  AND vec.document_id = ANY($6)\nORDER BY vec.document_id ASC", 1)
		args = append(args, pq.Array(filter.DocumentIDs))
		query = strings.Replace(query, "LIMIT $6", "LIMIT $7", 1)
	}
	args = append(args, filter.Limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list active eshu search vector values: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []EshuSearchVectorValue
	for rows.Next() {
		row, err := scanEshuSearchVectorValue(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active eshu search vector values: %w", err)
	}
	return results, nil
}

func scanEshuSearchVectorValue(rows Rows) (EshuSearchVectorValue, error) {
	var row EshuSearchVectorValue
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
		&row.VectorValues,
		&row.CreatedAt,
		&row.UpdatedAt,
	); err != nil {
		return EshuSearchVectorValue{}, fmt.Errorf("scan eshu search vector value: %w", err)
	}
	row.EmbeddingDimensions = int(dimensions)
	if err := validateEshuSearchVectorValue(row); err != nil {
		return EshuSearchVectorValue{}, fmt.Errorf("invalid eshu search vector value row: %w", err)
	}
	return row, nil
}

func normalizeEshuSearchVectorValue(row EshuSearchVectorValue) EshuSearchVectorValue {
	row.ScopeID = strings.TrimSpace(row.ScopeID)
	row.GenerationID = strings.TrimSpace(row.GenerationID)
	row.DocumentID = strings.TrimSpace(row.DocumentID)
	row.ProviderProfileID = strings.TrimSpace(row.ProviderProfileID)
	row.SourceClass = strings.TrimSpace(row.SourceClass)
	row.EmbeddingModelID = strings.TrimSpace(row.EmbeddingModelID)
	row.EmbeddingContentHash = strings.TrimSpace(row.EmbeddingContentHash)
	row.VectorIndexVersion = strings.TrimSpace(row.VectorIndexVersion)
	return row
}

func validateEshuSearchVectorValue(row EshuSearchVectorValue) error {
	var problems []error
	if row.ScopeID == "" {
		problems = append(problems, errors.New("eshu search vector value requires scope id"))
	}
	if row.GenerationID == "" {
		problems = append(problems, errors.New("eshu search vector value requires generation id"))
	}
	if row.DocumentID == "" {
		problems = append(problems, errors.New("eshu search vector value requires document id"))
	}
	if row.ProviderProfileID == "" {
		problems = append(problems, errors.New("eshu search vector value requires provider profile id"))
	}
	if row.SourceClass == "" {
		problems = append(problems, errors.New("eshu search vector value requires source class"))
	}
	if row.EmbeddingModelID == "" {
		problems = append(problems, errors.New("eshu search vector value requires embedding model id"))
	}
	if row.EmbeddingDimensions <= 0 {
		problems = append(problems, errors.New("eshu search vector value requires positive embedding dimensions"))
	}
	if row.EmbeddingDimensions > maxEshuSearchVectorValues {
		problems = append(problems, fmt.Errorf("eshu search vector value dimensions exceed %d", maxEshuSearchVectorValues))
	}
	if row.EmbeddingContentHash == "" {
		problems = append(problems, errors.New("eshu search vector value requires embedding content hash"))
	}
	if row.VectorIndexVersion == "" {
		problems = append(problems, errors.New("eshu search vector value requires vector index version"))
	}
	if len(row.VectorValues) != row.EmbeddingDimensions {
		problems = append(problems, errors.New("eshu search vector value vector length must match embedding dimensions"))
	}
	for i, value := range row.VectorValues {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			problems = append(problems, fmt.Errorf("eshu search vector value requires finite vector value at index %d", i))
			break
		}
	}
	if row.CreatedAt.IsZero() {
		problems = append(problems, errors.New("eshu search vector value requires created_at"))
	}
	if row.UpdatedAt.IsZero() {
		problems = append(problems, errors.New("eshu search vector value requires updated_at"))
	}
	return errors.Join(problems...)
}

func normalizeEshuSearchVectorValueFilter(filter EshuSearchVectorValueFilter) EshuSearchVectorValueFilter {
	filter.ScopeID = strings.TrimSpace(filter.ScopeID)
	filter.ProviderProfileID = strings.TrimSpace(filter.ProviderProfileID)
	filter.SourceClass = strings.TrimSpace(filter.SourceClass)
	filter.EmbeddingModelID = strings.TrimSpace(filter.EmbeddingModelID)
	filter.VectorIndexVersion = strings.TrimSpace(filter.VectorIndexVersion)
	filter.DocumentIDs = cleanStringFilterValues(filter.DocumentIDs)
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 500 {
		filter.Limit = 500
	}
	return filter
}

func validateEshuSearchVectorValueFilter(filter EshuSearchVectorValueFilter) error {
	var problems []error
	if filter.ScopeID == "" {
		problems = append(problems, errors.New("eshu search vector value filter requires scope id"))
	}
	if filter.ProviderProfileID == "" {
		problems = append(problems, errors.New("eshu search vector value filter requires provider profile id"))
	}
	if filter.SourceClass == "" {
		problems = append(problems, errors.New("eshu search vector value filter requires source class"))
	}
	if filter.EmbeddingModelID == "" {
		problems = append(problems, errors.New("eshu search vector value filter requires embedding model id"))
	}
	if filter.VectorIndexVersion == "" {
		problems = append(problems, errors.New("eshu search vector value filter requires vector index version"))
	}
	return errors.Join(problems...)
}
