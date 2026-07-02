// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
)

const upsertEshuSearchVectorMetadataBatchPrefix = `
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
) VALUES `

const upsertEshuSearchVectorMetadataBatchSuffix = `
ON CONFLICT (scope_id, generation_id, document_id, provider_profile_id, source_class, embedding_model_id, vector_index_version) DO UPDATE
SET embedding_dimensions = EXCLUDED.embedding_dimensions,
    embedding_content_hash = EXCLUDED.embedding_content_hash,
    build_state = EXCLUDED.build_state,
    failure_class = EXCLUDED.failure_class,
    updated_at = EXCLUDED.updated_at,
    last_success_at = EXCLUDED.last_success_at
`

// eshuSearchVectorMetadataBatchSize bounds one multi-row upsert statement.
// Each row binds eshuSearchVectorMetadataColumnsPerRow parameters
// (500*14 = 7000 params), well under Postgres's 65535 parameter-per-statement
// limit. See #4430: batching collapses the per-document round trips that
// dominated the search-vector build sweep in the reducer tail.
const eshuSearchVectorMetadataBatchSize = 500

const eshuSearchVectorMetadataColumnsPerRow = 14

// UpsertBatch inserts or updates many vector metadata rows in bounded
// multi-row statements instead of one round trip per row. Rows are normalized
// and validated exactly as Upsert does; a validation failure on any row
// aborts before issuing any statement, so partial batches never land.
func (s EshuSearchVectorMetadataStore) UpsertBatch(ctx context.Context, rows []EshuSearchVectorMetadata) error {
	if len(rows) == 0 {
		return nil
	}
	if s.db == nil {
		return fmt.Errorf("eshu search vector metadata database is required")
	}

	normalized := make([]EshuSearchVectorMetadata, len(rows))
	for i, row := range rows {
		row = normalizeEshuSearchVectorMetadata(row)
		if err := validateEshuSearchVectorMetadata(row); err != nil {
			return err
		}
		normalized[i] = row
	}

	for start := 0; start < len(normalized); start += eshuSearchVectorMetadataBatchSize {
		end := start + eshuSearchVectorMetadataBatchSize
		if end > len(normalized) {
			end = len(normalized)
		}
		if err := upsertEshuSearchVectorMetadataBatch(ctx, s.db, normalized[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// upsertEshuSearchVectorMetadataBatch issues one multi-row INSERT ... ON
// CONFLICT statement for a bounded slice of already-normalized,
// already-validated rows.
func upsertEshuSearchVectorMetadataBatch(ctx context.Context, db ExecQueryer, batch []EshuSearchVectorMetadata) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*eshuSearchVectorMetadataColumnsPerRow)
	var values strings.Builder
	for i, row := range batch {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * eshuSearchVectorMetadataColumnsPerRow
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, NULLIF($%d, ''), $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6,
			offset+7, offset+8, offset+9, offset+10, offset+11, offset+12,
			offset+13, offset+14,
		)
		var lastSuccess any
		if row.LastSuccessAt != nil {
			lastSuccess = *row.LastSuccessAt
		}
		args = append(
			args,
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
	}

	query := upsertEshuSearchVectorMetadataBatchPrefix + values.String() + upsertEshuSearchVectorMetadataBatchSuffix
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert eshu search vector metadata batch (%d rows): %w", len(batch), err)
	}
	return nil
}
