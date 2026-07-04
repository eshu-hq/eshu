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
// sweep's watermark from the singleton search_vector_build_materialization
// row. The row is upserted only when SearchVectorBuildRunner.RunOnce
// completes a bounded sweep with zero pending scopes (see
// go/internal/reducer/search_vector_build_runner.go), so its absence means
// the sweep has never caught up and its age means how long ago it last did.
const selectSearchVectorReadyWatermarkQuery = `
SELECT materialized_at
FROM search_vector_build_materialization
LIMIT 1`

// PostgresSearchVectorReadyStore reads the search-vector build sweep's
// search_vector_ready watermark for freshness reporting on the semantic
// search read path.
type PostgresSearchVectorReadyStore struct {
	db searchVectorReadyQueryer
}

// NewPostgresSearchVectorReadyStore builds a search-vector-ready watermark
// reader.
func NewPostgresSearchVectorReadyStore(db searchVectorReadyQueryer) PostgresSearchVectorReadyStore {
	return PostgresSearchVectorReadyStore{db: db}
}

// SearchVectorReadyWatermark probes the search_vector_build_materialization
// watermark. Signaled is always true (this store is only wired in when the
// operator has configured the signal), so a nil database or a probe failure
// is reported as an error with Signaled=true — the caller downgrades the
// truth envelope to unavailable rather than falsely reporting fresh.
func (s PostgresSearchVectorReadyStore) SearchVectorReadyWatermark(
	ctx context.Context,
) (SearchVectorReadyFreshness, error) {
	if s.db == nil {
		return SearchVectorReadyFreshness{Signaled: true}, fmt.Errorf("search vector ready store requires a database")
	}
	rows, err := s.db.QueryContext(ctx, selectSearchVectorReadyWatermarkQuery)
	if err != nil {
		return SearchVectorReadyFreshness{Signaled: true}, fmt.Errorf("read search vector ready watermark: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return SearchVectorReadyFreshness{Signaled: true}, fmt.Errorf("read search vector ready watermark: %w", err)
		}
		// No rows: the search-vector build sweep has never caught up.
		return SearchVectorReadyFreshness{Signaled: true, Present: false}, nil
	}
	var materializedAt time.Time
	if err := rows.Scan(&materializedAt); err != nil {
		return SearchVectorReadyFreshness{Signaled: true}, fmt.Errorf("scan search vector ready watermark: %w", err)
	}
	return SearchVectorReadyFreshness{Signaled: true, Present: true, MaterializedAt: materializedAt}, nil
}
