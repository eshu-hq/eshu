// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

const fencedVectorWriteJoins = `
FROM incoming row
JOIN ingestion_scopes scope
  ON scope.scope_id = row.scope_id
 AND scope.active_generation_id = row.generation_id
JOIN eshu_search_document_projection_state projection
  ON projection.scope_id = row.scope_id
 AND projection.generation_id = row.generation_id
 AND projection.state = 'ready'
 AND projection.projection_revision = row.projection_revision
JOIN eshu_search_vector_scope_state vector_scope
  ON vector_scope.scope_id = row.scope_id
 AND vector_scope.generation_id = row.generation_id
 AND vector_scope.provider_profile_id = row.provider_profile_id
 AND vector_scope.source_class = row.source_class
 AND vector_scope.embedding_model_id = row.embedding_model_id
 AND vector_scope.vector_index_version = row.vector_index_version
 AND vector_scope.projection_revision = row.projection_revision
 AND vector_scope.build_fence = row.build_fence
 AND vector_scope.state = 'building'
JOIN eshu_search_index_documents document
  ON document.scope_id = row.scope_id
 AND document.generation_id = row.generation_id
 AND document.document_id = row.document_id
 AND document.content_hash = row.embedding_content_hash
`

func metadataBatchFenceMode(rows []EshuSearchVectorMetadata) (bool, error) {
	mode := false
	for i, row := range rows {
		hasRevision := row.ProjectionRevision > 0
		hasFence := row.BuildFence > 0
		if hasRevision != hasFence {
			return false, fmt.Errorf("eshu search vector metadata row %d requires projection revision and build fence together", i)
		}
		if i == 0 {
			mode = hasRevision
			continue
		}
		if hasRevision != mode {
			return false, fmt.Errorf("eshu search vector metadata batch cannot mix fenced and unfenced rows")
		}
	}
	return mode, nil
}

func valueBatchFenceMode(rows []EshuSearchVectorValue) (bool, error) {
	mode := false
	for i, row := range rows {
		hasRevision := row.ProjectionRevision > 0
		hasFence := row.BuildFence > 0
		if hasRevision != hasFence {
			return false, fmt.Errorf("eshu search vector value row %d requires projection revision and build fence together", i)
		}
		if i == 0 {
			mode = hasRevision
			continue
		}
		if hasRevision != mode {
			return false, fmt.Errorf("eshu search vector value batch cannot mix fenced and unfenced rows")
		}
	}
	return mode, nil
}

func upsertEshuSearchVectorMetadataBatchFenced(
	ctx context.Context,
	db ExecQueryer,
	batch []EshuSearchVectorMetadata,
) error {
	const columnsPerRow = eshuSearchVectorMetadataColumnsPerRow + 2
	args := make([]any, 0, len(batch)*columnsPerRow)
	var values strings.Builder
	for i, row := range batch {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * columnsPerRow
		fmt.Fprintf(&values,
			"($%d::text,$%d::text,$%d::text,$%d::text,$%d::text,$%d::text,$%d::integer,$%d::text,$%d::text,$%d::text,NULLIF($%d::text,''),$%d::timestamptz,$%d::timestamptz,$%d::timestamptz,$%d::bigint,$%d::bigint)",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6,
			offset+7, offset+8, offset+9, offset+10, offset+11, offset+12,
			offset+13, offset+14, offset+15, offset+16)
		var lastSuccess any
		if row.LastSuccessAt != nil {
			lastSuccess = *row.LastSuccessAt
		}
		args = append(args, row.ScopeID, row.GenerationID, row.DocumentID,
			row.ProviderProfileID, row.SourceClass, row.EmbeddingModelID,
			row.EmbeddingDimensions, row.EmbeddingContentHash, row.VectorIndexVersion,
			string(row.BuildState), row.FailureClass, row.CreatedAt, row.UpdatedAt,
			lastSuccess, row.ProjectionRevision, row.BuildFence)
	}
	query := `WITH incoming (
scope_id,generation_id,document_id,provider_profile_id,source_class,
embedding_model_id,embedding_dimensions,embedding_content_hash,
vector_index_version,build_state,failure_class,created_at,updated_at,
last_success_at,projection_revision,build_fence
) AS (VALUES ` + values.String() + `)
INSERT INTO eshu_search_vector_metadata (
scope_id,generation_id,document_id,provider_profile_id,source_class,
embedding_model_id,embedding_dimensions,embedding_content_hash,
vector_index_version,build_state,failure_class,created_at,updated_at,last_success_at
)
SELECT row.scope_id,row.generation_id,row.document_id,row.provider_profile_id,
row.source_class,row.embedding_model_id,row.embedding_dimensions,
row.embedding_content_hash,row.vector_index_version,row.build_state,
row.failure_class,row.created_at,row.updated_at,row.last_success_at
` + fencedVectorWriteJoins + upsertEshuSearchVectorMetadataBatchSuffix
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert fenced eshu search vector metadata batch (%d rows): %w", len(batch), err)
	}
	return nil
}

func upsertEshuSearchVectorValueBatchFenced(
	ctx context.Context,
	db ExecQueryer,
	batch []EshuSearchVectorValue,
) error {
	const columnsPerRow = eshuSearchVectorValueColumnsPerRow + 2
	args := make([]any, 0, len(batch)*columnsPerRow)
	var values strings.Builder
	for i, row := range batch {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * columnsPerRow
		fmt.Fprintf(&values,
			"($%d::text,$%d::text,$%d::text,$%d::text,$%d::text,$%d::text,$%d::integer,$%d::text,$%d::text,$%d::double precision[],$%d::timestamptz,$%d::timestamptz,$%d::bigint,$%d::bigint)",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6,
			offset+7, offset+8, offset+9, offset+10, offset+11, offset+12,
			offset+13, offset+14)
		args = append(args, row.ScopeID, row.GenerationID, row.DocumentID,
			row.ProviderProfileID, row.SourceClass, row.EmbeddingModelID,
			row.EmbeddingDimensions, row.EmbeddingContentHash, row.VectorIndexVersion,
			pq.Array(row.VectorValues), row.CreatedAt, row.UpdatedAt,
			row.ProjectionRevision, row.BuildFence)
	}
	query := `WITH incoming (
scope_id,generation_id,document_id,provider_profile_id,source_class,
embedding_model_id,embedding_dimensions,embedding_content_hash,
vector_index_version,vector_values,created_at,updated_at,
projection_revision,build_fence
) AS (VALUES ` + values.String() + `)
INSERT INTO eshu_search_vector_values (
scope_id,generation_id,document_id,provider_profile_id,source_class,
embedding_model_id,embedding_dimensions,embedding_content_hash,
vector_index_version,vector_values,created_at,updated_at
)
SELECT row.scope_id,row.generation_id,row.document_id,row.provider_profile_id,
row.source_class,row.embedding_model_id,row.embedding_dimensions,
row.embedding_content_hash,row.vector_index_version,row.vector_values,
row.created_at,row.updated_at
` + fencedVectorWriteJoins + upsertEshuSearchVectorValueBatchSuffix
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert fenced eshu search vector value batch (%d rows): %w", len(batch), err)
	}
	return nil
}
