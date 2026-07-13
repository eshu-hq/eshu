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
  -- A scope with a failed ingestion (status='failed') never gets an active
  -- generation assigned, leaving active_generation_id NULL. generation_id is
  -- NOT NULL on eshu_search_document_projection_state, so without this guard
  -- one failed scope aborts the whole INSERT ... SELECT and blocks every
  -- other (healthy) scope from being seeded. Skip failed scopes here; they
  -- get seeded once re-ingestion assigns them a real generation.
  AND s.active_generation_id IS NOT NULL
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

const countFailedGenerationRepositoryScopesSQL = `
SELECT count(*)
FROM ingestion_scopes
WHERE scope_kind = 'repository'
  AND active_generation_id IS NULL
`

// SeedSearchVectorScopeStateResult reports how many repository scopes were
// newly seeded into eshu_search_document_projection_state this call and how
// many were skipped because they have no active generation (a failed
// ingestion). Startup logging needs both numbers: a bare "seeded" success log
// cannot distinguish a fully healthy corpus from one where N scopes are
// permanently excluded from search/vector projection until re-ingested.
type SeedSearchVectorScopeStateResult struct {
	ProjectionRowsSeeded int64
	FailedScopesSkipped  int64
}

// SeedSearchVectorScopeState is the one-time fail-closed migration seeder
// (#4233). It populates eshu_search_document_projection_state rows for every
// active repository scope (idempotent) and then records conservative building
// vector-scope rows. Startup never performs a corpus-wide exact-ready proof;
// the bounded scheduler verifies each scope and CAS-publishes ready state.
func SeedSearchVectorScopeState(
	ctx context.Context,
	db ExecQueryer,
	identity EshuSearchVectorIdentity,
) (SeedSearchVectorScopeStateResult, error) {
	if db == nil {
		return SeedSearchVectorScopeStateResult{}, fmt.Errorf("eshu search vector scope state seed requires a database")
	}
	if identity.ProviderProfileID == "" {
		return SeedSearchVectorScopeStateResult{}, fmt.Errorf("eshu search vector scope state seed requires provider profile id")
	}
	if identity.SourceClass == "" {
		return SeedSearchVectorScopeStateResult{}, fmt.Errorf("eshu search vector scope state seed requires source class")
	}
	if identity.EmbeddingModelID == "" {
		return SeedSearchVectorScopeStateResult{}, fmt.Errorf("eshu search vector scope state seed requires embedding model id")
	}
	if identity.VectorIndexVersion == "" {
		return SeedSearchVectorScopeStateResult{}, fmt.Errorf("eshu search vector scope state seed requires vector index version")
	}

	skipped, err := countFailedGenerationRepositoryScopes(ctx, db)
	if err != nil {
		return SeedSearchVectorScopeStateResult{}, fmt.Errorf("count failed-generation repository scopes: %w", err)
	}

	// Step 1: seed projection_state rows for every repository scope with a
	// real (non-failed) generation.
	res, err := db.ExecContext(ctx, seedProjectionStateSQL)
	if err != nil {
		return SeedSearchVectorScopeStateResult{}, fmt.Errorf("seed eshu search document projection state: %w", err)
	}
	seeded, err := res.RowsAffected()
	if err != nil {
		return SeedSearchVectorScopeStateResult{}, fmt.Errorf("seed eshu search document projection state: rows affected: %w", err)
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
		return SeedSearchVectorScopeStateResult{}, fmt.Errorf("seed eshu search vector scope state: %w", err)
	}

	return SeedSearchVectorScopeStateResult{
		ProjectionRowsSeeded: seeded,
		FailedScopesSkipped:  skipped,
	}, nil
}

// countFailedGenerationRepositoryScopes counts repository scopes with no
// active generation (status='failed' ingestion), the set seedProjectionStateSQL
// deliberately skips.
func countFailedGenerationRepositoryScopes(ctx context.Context, db ExecQueryer) (int64, error) {
	rows, err := db.QueryContext(ctx, countFailedGenerationRepositoryScopesSQL)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, err
		}
		return 0, nil
	}
	var count int64
	if err := rows.Scan(&count); err != nil {
		return 0, err
	}
	return count, rows.Err()
}
