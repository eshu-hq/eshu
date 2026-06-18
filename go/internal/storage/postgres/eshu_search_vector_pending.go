package postgres

import (
	"context"
	"fmt"
)

const (
	eshuSearchVectorPendingDefaultLimit = 100
	eshuSearchVectorPendingMaxLimit     = 1000
)

const listPendingEshuSearchVectorScopesSQL = `
WITH active_docs AS (
    SELECT
        fact.scope_id,
        fact.generation_id,
        COALESCE(scope.payload->>'repo_id', '') AS repo_id,
        fact.payload->'document'->>'id' AS document_id
    FROM fact_records fact
    JOIN ingestion_scopes scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    WHERE scope.scope_kind = 'repository'
      AND fact.fact_kind = $1
      AND fact.is_tombstone = FALSE
),
ready_docs AS (
    SELECT DISTINCT
        meta.scope_id,
        meta.generation_id,
        meta.document_id
    FROM eshu_search_vector_metadata meta
    JOIN eshu_search_vector_values value
      ON value.scope_id = meta.scope_id
     AND value.generation_id = meta.generation_id
     AND value.document_id = meta.document_id
     AND value.embedding_model_id = meta.embedding_model_id
     AND value.vector_index_version = meta.vector_index_version
     AND value.embedding_content_hash = meta.embedding_content_hash
    WHERE meta.embedding_model_id = $2
      AND meta.vector_index_version = $3
      AND meta.build_state = 'ready'
)
SELECT docs.scope_id, docs.generation_id, docs.repo_id
FROM active_docs docs
LEFT JOIN ready_docs ready
  ON ready.scope_id = docs.scope_id
 AND ready.generation_id = docs.generation_id
 AND ready.document_id = docs.document_id
WHERE ready.document_id IS NULL
GROUP BY docs.scope_id, docs.generation_id, docs.repo_id
ORDER BY docs.scope_id
LIMIT $4
`

// EshuSearchVectorPendingRequest bounds pending local vector build discovery.
type EshuSearchVectorPendingRequest struct {
	EmbeddingModelID   string
	VectorIndexVersion string
	Limit              int
}

// EshuSearchVectorPendingScope identifies one active scope that needs local
// vector rows for its curated search documents.
type EshuSearchVectorPendingScope struct {
	ScopeID      string
	GenerationID string
	RepoID       string
}

// EshuSearchVectorPendingStore lists active repository scopes whose curated
// search documents do not yet have complete ready local vector rows.
type EshuSearchVectorPendingStore struct {
	db ExecQueryer
}

// NewEshuSearchVectorPendingStore builds a pending local-vector lister.
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
