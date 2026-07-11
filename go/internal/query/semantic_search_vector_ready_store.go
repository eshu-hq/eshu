// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// searchVectorReadyQueryer is the narrow read-only surface required to probe
// the search_vector_build_materialization watermark.
type searchVectorReadyQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// selectSearchVectorReadyWatermarkQuery reads the search-vector build
// sweep's watermark for one vector-identity tuple from
// search_vector_build_materialization. A prior ready row is necessary but not
// sufficient: document projection can create a newer revision after the
// watermark was published. The NOT EXISTS guard rechecks the versioned
// per-scope readiness state so an old row fails closed while any active scope
// is missing, building, or ready for an older projection revision. The row and
// readiness check are keyed by the full identity tuple (not a singleton), so
// one provider/model/version cannot satisfy another identity's probe.
const selectSearchVectorReadyWatermarkQuery = `
SELECT materialization.materialized_at
FROM search_vector_build_materialization materialization
WHERE materialization.provider_profile_id = $1
  AND materialization.source_class = $2
  AND materialization.embedding_model_id = $3
  AND materialization.vector_index_version = $4
  AND NOT EXISTS (
    SELECT 1
    FROM eshu_search_document_projection_state projection
    JOIN ingestion_scopes scope
      ON scope.scope_id = projection.scope_id
     AND scope.active_generation_id = projection.generation_id
    LEFT JOIN eshu_search_vector_scope_state vector_scope
      ON vector_scope.scope_id = projection.scope_id
     AND vector_scope.generation_id = projection.generation_id
     AND vector_scope.provider_profile_id = materialization.provider_profile_id
     AND vector_scope.source_class = materialization.source_class
     AND vector_scope.embedding_model_id = materialization.embedding_model_id
     AND vector_scope.vector_index_version = materialization.vector_index_version
    WHERE scope.scope_kind = 'repository'
      AND projection.state = 'ready'
      AND projection.document_count > 0
      AND (
        vector_scope.state IS NULL
        OR vector_scope.state <> 'ready'
        OR vector_scope.projection_revision <> projection.projection_revision
      )
  )
LIMIT 1`

// SearchVectorBuildIdentity names the vector-identity tuple the query side's
// search_vector_ready probe is scoped to: the same
// (provider profile, source class, embedding model, vector index version)
// tuple the reducer's SearchVectorBuildRunner and its ListPendingSearchVectorScopes
// port key their work by. It mirrors reducer.SearchVectorBuildIdentity; the
// query package cannot import the reducer package for this small struct (see
// package ownership boundaries), so the tuple shape is duplicated
// deliberately, the same way other reducer/query port pairs mirror their
// request shapes across the package boundary.
type SearchVectorBuildIdentity struct {
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
}

// PostgresSearchVectorReadyStore reads the search-vector build sweep's
// search_vector_ready watermark for freshness reporting on the semantic
// search read path, scoped to one configured vector-identity tuple.
type PostgresSearchVectorReadyStore struct {
	db       searchVectorReadyQueryer
	identity SearchVectorBuildIdentity
}

// NewPostgresSearchVectorReadyStore builds a search-vector-ready watermark
// reader scoped to identity — the same vector-identity tuple the process's
// semantic-search embedding configuration resolves to (provider profile,
// source class, embedding model, vector index version).
func NewPostgresSearchVectorReadyStore(db searchVectorReadyQueryer, identity SearchVectorBuildIdentity) PostgresSearchVectorReadyStore {
	return PostgresSearchVectorReadyStore{db: db, identity: identity}
}

// SearchVectorReadyWatermark probes the search_vector_build_materialization
// watermark for the store's configured identity tuple. Signaled is always
// true (this store is only wired in when the operator has configured the
// signal), so a nil database or a probe failure is reported as an error with
// Signaled=true — the caller downgrades the truth envelope to unavailable
// rather than falsely reporting fresh.
func (s PostgresSearchVectorReadyStore) SearchVectorReadyWatermark(
	ctx context.Context,
) (SearchVectorReadyFreshness, error) {
	if s.db == nil {
		return SearchVectorReadyFreshness{Signaled: true}, fmt.Errorf("search vector ready store requires a database")
	}
	rows, err := s.db.QueryContext(
		ctx,
		selectSearchVectorReadyWatermarkQuery,
		s.identity.ProviderProfileID,
		s.identity.SourceClass,
		s.identity.EmbeddingModelID,
		s.identity.VectorIndexVersion,
	)
	if err != nil {
		return SearchVectorReadyFreshness{Signaled: true}, fmt.Errorf("read search vector ready watermark: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return SearchVectorReadyFreshness{Signaled: true}, fmt.Errorf("read search vector ready watermark: %w", err)
		}
		// No rows: the search-vector build sweep has never caught up for
		// this identity.
		return SearchVectorReadyFreshness{Signaled: true, Present: false}, nil
	}
	var materializedAt time.Time
	if err := rows.Scan(&materializedAt); err != nil {
		return SearchVectorReadyFreshness{Signaled: true}, fmt.Errorf("scan search vector ready watermark: %w", err)
	}
	return SearchVectorReadyFreshness{Signaled: true, Present: true, MaterializedAt: materializedAt}, nil
}
