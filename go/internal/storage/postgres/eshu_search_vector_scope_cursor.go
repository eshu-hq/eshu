// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const advanceSearchVectorDocumentCursorSQL = `
UPDATE eshu_search_vector_scope_state
SET document_cursor = GREATEST(document_cursor, $9), updated_at = $10
WHERE scope_id = $1
  AND generation_id = $2
  AND provider_profile_id = $3
  AND source_class = $4
  AND embedding_model_id = $5
  AND vector_index_version = $6
  AND projection_revision = $7
  AND build_fence = $8
  AND state = 'building'
  AND generation_id = (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1)
`

const resetSearchVectorDocumentCursorSQL = `
UPDATE eshu_search_vector_scope_state
SET document_cursor = '', updated_at = $9
WHERE scope_id = $1
  AND generation_id = $2
  AND provider_profile_id = $3
  AND source_class = $4
  AND embedding_model_id = $5
  AND vector_index_version = $6
  AND projection_revision = $7
  AND build_fence = $8
  AND state = 'building'
  AND generation_id = (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1)
`

// AdvanceDocumentCursor monotonically advances one scope's keyset cursor when
// the projection revision and build fence are still current.
func (s EshuSearchVectorScopeStateStore) AdvanceDocumentCursor(
	ctx context.Context,
	scopeID, generationID string,
	identity EshuSearchVectorIdentity,
	projectionRevision, fence int64,
	documentID string,
) (bool, error) {
	if err := validateSearchVectorCursorMutation(s.db, scopeID, generationID); err != nil {
		return false, err
	}
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return false, fmt.Errorf("advance eshu search vector document cursor requires document id")
	}
	result, err := s.db.ExecContext(ctx, advanceSearchVectorDocumentCursorSQL,
		scopeID, generationID,
		identity.ProviderProfileID, identity.SourceClass,
		identity.EmbeddingModelID, identity.VectorIndexVersion,
		projectionRevision, fence, documentID, time.Now().UTC(),
	)
	if err != nil {
		return false, fmt.Errorf("advance eshu search vector document cursor: %w", err)
	}
	return searchVectorCursorMutationApplied(result)
}

// ResetDocumentCursor wraps one scope's keyset cursor to the beginning when
// the projection revision and build fence are still current.
func (s EshuSearchVectorScopeStateStore) ResetDocumentCursor(
	ctx context.Context,
	scopeID, generationID string,
	identity EshuSearchVectorIdentity,
	projectionRevision, fence int64,
) (bool, error) {
	if err := validateSearchVectorCursorMutation(s.db, scopeID, generationID); err != nil {
		return false, err
	}
	result, err := s.db.ExecContext(ctx, resetSearchVectorDocumentCursorSQL,
		scopeID, generationID,
		identity.ProviderProfileID, identity.SourceClass,
		identity.EmbeddingModelID, identity.VectorIndexVersion,
		projectionRevision, fence, time.Now().UTC(),
	)
	if err != nil {
		return false, fmt.Errorf("reset eshu search vector document cursor: %w", err)
	}
	return searchVectorCursorMutationApplied(result)
}

func validateSearchVectorCursorMutation(db ExecQueryer, scopeID, generationID string) error {
	if db == nil {
		return fmt.Errorf("eshu search vector scope state store requires a database")
	}
	if strings.TrimSpace(scopeID) == "" {
		return fmt.Errorf("eshu search vector document cursor requires scope id")
	}
	if strings.TrimSpace(generationID) == "" {
		return fmt.Errorf("eshu search vector document cursor requires generation id")
	}
	return nil
}

func searchVectorCursorMutationApplied(result interface{ RowsAffected() (int64, error) }) (bool, error) {
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected eshu search vector document cursor: %w", err)
	}
	return affected == 1, nil
}
