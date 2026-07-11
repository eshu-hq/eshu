// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
)

const (
	eshuSearchVectorPendingDefaultLimit = 100
	eshuSearchVectorPendingMaxLimit     = 1000
)

// listPendingEshuSearchVectorScopesSQL lists active repository scopes whose
// curated search documents do not yet have a complete ready vector row for the
// requested provider/model/version tuple.
//
// Performance design (#4233): the previous implementation materialised the
// entire corpus-wide eshu_search_vector_metadata table via a SELECT DISTINCT
// ready_docs CTE (cost ~225k rows at full corpus) and Merge Anti Joined it
// against active_docs regardless of how many active scopes needed checking
// (~2000). The NOT EXISTS rewrite drives the readiness probe per active_doc
// row using a covering metadata index (the primary key or
// eshu_search_vector_metadata_model_v2_idx, both keyed by scope/generation +
// the provider tuple); the planner observed using model_v2_idx on the 43 GB
// corpus. The planner emits a Nested Loop Anti Join / Index Scan bounded by
// the active_docs cardinality (~17 at LIMIT / ~14 399 full) instead of
// materialising the whole table. No schema change is required.
const listPendingEshuSearchVectorScopesSQL = `
WITH active_docs AS (
    SELECT fact.scope_id, fact.generation_id,
        COALESCE(scope.payload->>'repo_id', '') AS repo_id,
        fact.payload->>'document_id' AS document_id,
        fact.payload->>'content_hash' AS content_hash
    FROM fact_records fact
    JOIN ingestion_scopes scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    WHERE scope.scope_kind = 'repository'
      AND fact.fact_kind = $1
      AND fact.is_tombstone = FALSE
)
SELECT docs.scope_id, docs.generation_id, docs.repo_id
FROM active_docs docs
WHERE NOT EXISTS (
    SELECT 1
    FROM eshu_search_vector_metadata meta
    LEFT JOIN eshu_search_vector_values value
      ON value.scope_id = meta.scope_id AND value.generation_id = meta.generation_id
     AND value.document_id = meta.document_id AND value.provider_profile_id = meta.provider_profile_id
     AND value.source_class = meta.source_class AND value.embedding_model_id = meta.embedding_model_id
     AND value.vector_index_version = meta.vector_index_version
     AND value.embedding_content_hash = meta.embedding_content_hash
    WHERE meta.scope_id = docs.scope_id
      AND meta.generation_id = docs.generation_id
      AND meta.document_id = docs.document_id
      AND meta.provider_profile_id = $2
      AND meta.source_class = $3
      AND meta.embedding_model_id = $4
      AND meta.vector_index_version = $5
      AND meta.embedding_content_hash = docs.content_hash
      AND (meta.build_state = 'disabled' OR (meta.build_state = 'ready' AND value.document_id IS NOT NULL))
)
GROUP BY docs.scope_id, docs.generation_id, docs.repo_id
ORDER BY docs.scope_id
LIMIT $6
`

// EshuSearchVectorPendingRequest bounds pending vector build discovery.
type EshuSearchVectorPendingRequest struct {
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
	Limit              int
}

// EshuSearchVectorPendingScope identifies one active scope that needs
// vector rows for its curated search documents. ProjectionRevision carries the
// document-projection revision the scheduler observed (0 from the retired
// corpus-wide store, which does not track scope state); the versioned
// scope-state store (#4233) populates it so the build runner can finalize
// vector readiness with a revision/fence CAS.
type EshuSearchVectorPendingScope struct {
	ScopeID            string
	GenerationID       string
	RepoID             string
	ProjectionRevision int64
	DocumentCursor     string
}

// EshuSearchVectorPendingStore is the RETIRED corpus-wide pending lister. It is
// no longer wired into any runtime path (#4233 replaced it with
// EshuSearchVectorScopeStateStore.ListPendingSearchVectorScopes, an O(active
// scopes) versioned scope-state scan). It is retained ONLY as the equivalence
// reference the live regression test compares the new scheduler against, so the
// two must return an identical pending set. Do not re-wire it into production.
type EshuSearchVectorPendingStore struct {
	db ExecQueryer
}

// NewEshuSearchVectorPendingStore builds the retired reference pending lister
// used only by the #4233 equivalence regression test.
func NewEshuSearchVectorPendingStore(db ExecQueryer) EshuSearchVectorPendingStore {
	return EshuSearchVectorPendingStore{db: db}
}

// ListPendingSearchVectorScopes returns active scopes with missing ready vector
// rows for the requested embedding model and vector-index version.
func (s EshuSearchVectorPendingStore) ListPendingSearchVectorScopes(
	ctx context.Context,
	req EshuSearchVectorPendingRequest,
) ([]EshuSearchVectorPendingScope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("eshu search vector pending store requires a database")
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
		listPendingEshuSearchVectorScopesSQL,
		EshuSearchDocumentFactKind,
		req.ProviderProfileID,
		req.SourceClass,
		req.EmbeddingModelID,
		req.VectorIndexVersion,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending eshu search vector scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var scopes []EshuSearchVectorPendingScope
	for rows.Next() {
		var pending EshuSearchVectorPendingScope
		if err := rows.Scan(&pending.ScopeID, &pending.GenerationID, &pending.RepoID); err != nil {
			return nil, fmt.Errorf("scan pending eshu search vector scope: %w", err)
		}
		scopes = append(scopes, pending)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending eshu search vector scopes: %w", err)
	}
	return scopes, nil
}
