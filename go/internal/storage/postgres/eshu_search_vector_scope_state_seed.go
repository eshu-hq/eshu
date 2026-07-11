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
          FROM eshu_search_index_documents doc
         WHERE doc.scope_id = s.scope_id
           AND doc.generation_id = s.active_generation_id),
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
       'building',
       NOW()
FROM eshu_search_document_projection_state ps
JOIN ingestion_scopes scope
  ON scope.scope_id = ps.scope_id
 AND scope.active_generation_id = ps.generation_id
WHERE ps.state = 'ready'
  AND ps.document_count > 0
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

// SeedSearchVectorScopeState is the one-time fail-closed migration seeder
// (#4233). It populates eshu_search_document_projection_state rows for every
// active repository scope (idempotent) and then records conservative building
// vector-scope rows. Startup never performs a corpus-wide exact-ready proof;
// the bounded scheduler verifies each scope and CAS-publishes ready state.
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
	if _, err := db.ExecContext(ctx, seedProjectionStateSQL); err != nil {
		return fmt.Errorf("seed eshu search document projection state: %w", err)
	}

	// Step 2: seed conservative building rows. Exact readiness is deliberately
	// deferred to the bounded scheduler so reducer startup stays index-bounded.
	if _, err := db.ExecContext(
		ctx,
		seedVectorScopeStateSQL,
		identity.ProviderProfileID,
		identity.SourceClass,
		identity.EmbeddingModelID,
		identity.VectorIndexVersion,
	); err != nil {
		return fmt.Errorf("seed eshu search vector scope state: %w", err)
	}

	return nil
}
