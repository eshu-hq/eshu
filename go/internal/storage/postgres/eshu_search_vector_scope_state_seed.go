// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
)

const seedProjectionStateSQL = `
INSERT INTO eshu_search_document_projection_state
  (scope_id, generation_id, projection_revision, build_fence, state, document_count, updated_at)
SELECT s.scope_id,
       s.active_generation_id,
       1,
       1,
       'ready',
       (SELECT count(*)
          FROM fact_records f
         WHERE f.scope_id = s.scope_id
           AND f.generation_id = s.active_generation_id
           AND f.fact_kind = $1
           AND f.is_tombstone = FALSE),
       NOW()
FROM ingestion_scopes s
WHERE s.scope_kind = 'repository'
  AND NOT EXISTS (
    SELECT 1
    FROM eshu_search_document_projection_state ps
    WHERE ps.scope_id = s.scope_id
      AND ps.generation_id = s.active_generation_id
  )
`

const seedVectorScopeStateSQL = `
INSERT INTO eshu_search_vector_scope_state
  (scope_id, generation_id, provider_profile_id, source_class, embedding_model_id,
   vector_index_version, projection_revision, build_fence, state, updated_at)
SELECT ps.scope_id,
       ps.generation_id,
       $1,
       $2,
       $3,
       $4,
       ps.projection_revision,
       1,
       'ready',
       NOW()
FROM eshu_search_document_projection_state ps
WHERE ps.document_count > 0
  AND NOT EXISTS (
    SELECT 1
    FROM fact_records fact
    WHERE fact.scope_id = ps.scope_id
      AND fact.generation_id = ps.generation_id
      AND fact.fact_kind = $5
      AND fact.is_tombstone = FALSE
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
        WHERE meta.scope_id = fact.scope_id
          AND meta.generation_id = fact.generation_id
          AND meta.document_id = fact.payload->>'document_id'
          AND meta.provider_profile_id = $1
          AND meta.source_class = $2
          AND meta.embedding_model_id = $3
          AND meta.vector_index_version = $4
          AND meta.embedding_content_hash = fact.payload->>'content_hash'
          AND (meta.build_state = 'disabled'
               OR (meta.build_state = 'ready' AND value.document_id IS NOT NULL))
      )
  )
  AND NOT EXISTS (
    SELECT 1
    FROM eshu_search_vector_scope_state vs
    WHERE vs.scope_id = ps.scope_id
      AND vs.generation_id = ps.generation_id
      AND vs.provider_profile_id = $1
      AND vs.source_class = $2
      AND vs.embedding_model_id = $3
      AND vs.vector_index_version = $4
  )
`

// SeedSearchVectorScopeState is the one-time exact-proof migration seeder
// (#4233). It populates eshu_search_document_projection_state rows for every
// repository scope (idempotent) and then seeds eshu_search_vector_scope_state
// ready rows only for scopes the exact per-scope anti-join proves complete.
// It never backfills from row counts — every vector_scope_state row is gated
// by the exact content-hash and value-row anti-join the pending lister uses.
func SeedSearchVectorScopeState(
	ctx context.Context,
	db ExecQueryer,
	identity EshuSearchVectorIdentity,
) error {
	if db == nil {
		return fmt.Errorf("eshu search vector scope state seed requires a database")
	}
	if identity.ProviderProfileID == "" {
		return fmt.Errorf("eshu search vector scope state seed requires provider profile id")
	}
	if identity.SourceClass == "" {
		return fmt.Errorf("eshu search vector scope state seed requires source class")
	}
	if identity.EmbeddingModelID == "" {
		return fmt.Errorf("eshu search vector scope state seed requires embedding model id")
	}
	if identity.VectorIndexVersion == "" {
		return fmt.Errorf("eshu search vector scope state seed requires vector index version")
	}

	// Step 1: seed projection_state rows for every repository scope.
	if _, err := db.ExecContext(ctx, seedProjectionStateSQL, EshuSearchDocumentFactKind); err != nil {
		return fmt.Errorf("seed eshu search document projection state: %w", err)
	}

	// Step 2: seed vector_scope_state ready rows only for scopes the exact
	// per-scope anti-join proves complete.
	if _, err := db.ExecContext(
		ctx,
		seedVectorScopeStateSQL,
		identity.ProviderProfileID,
		identity.SourceClass,
		identity.EmbeddingModelID,
		identity.VectorIndexVersion,
		EshuSearchDocumentFactKind,
	); err != nil {
		return fmt.Errorf("seed eshu search vector scope state: %w", err)
	}

	return nil
}
