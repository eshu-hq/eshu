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
// search_vector_build_materialization. The row is upserted only when
// SearchVectorBuildRunner.RunOnce's post-build re-check finds zero pending
// scopes for that identity (see
// go/internal/reducer/search_vector_build_runner.go), so its absence means
// the sweep has never caught up for this identity and its age means how long
// ago it last did. The row is keyed by the full identity tuple (not a
// singleton), so a ready publish for one identity never satisfies a
// freshness probe for a different identity — required during a provider,
// model, or vector-index-version rollout, or when two reducer/API configs
// share one Postgres.
const selectSearchVectorReadyWatermarkQuery = `
SELECT materialized_at
FROM search_vector_build_materialization
WHERE provider_profile_id = $1 AND source_class = $2
  AND embedding_model_id = $3 AND vector_index_version = $4
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
