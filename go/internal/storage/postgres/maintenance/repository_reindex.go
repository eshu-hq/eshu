// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenancestore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// repositoryReindexSchemaSQL is the repository_reindex_requests DDL that
// migration 162 applies. A test keeps the two byte-identical.
const repositoryReindexSchemaSQL = `
CREATE TABLE IF NOT EXISTS repository_reindex_requests (
    scope_id TEXT PRIMARY KEY,
    requested_at TIMESTAMPTZ NOT NULL
);
`

// requestRepositoryReindexQuery stamps every named scope's watermark from the
// database clock and never moves one backward. The input is deduplicated and
// locked in scope_id order: a duplicate inside one statement fails with
// SQLSTATE 21000, and two requests that lock overlapping rows in different
// orders deadlock (SQLSTATE 40P01).
const requestRepositoryReindexQuery = `
INSERT INTO repository_reindex_requests AS request (scope_id, requested_at)
SELECT scope_id, now()
FROM (SELECT DISTINCT unnest($1::text[]) AS scope_id ORDER BY 1) AS requested
ON CONFLICT (scope_id) DO UPDATE
SET requested_at = GREATEST(request.requested_at, EXCLUDED.requested_at)
RETURNING scope_id, requested_at
`

// repositoryReindexWatermarksQuery reads the per-repository watermarks newer
// than the fleet watermark; an older row is already subsumed by it.
const repositoryReindexWatermarksQuery = `
SELECT scope_id, requested_at
FROM repository_reindex_requests
WHERE requested_at > $1
`

// RepositoryReindexStore persists per-repository reindex watermarks in
// repository_reindex_requests (#7620).
type RepositoryReindexStore struct {
	database db.ExecQueryer
}

// NewRepositoryReindexStore constructs a Postgres-backed per-repository
// reindex store.
func NewRepositoryReindexStore(database db.ExecQueryer) RepositoryReindexStore {
	return RepositoryReindexStore{database: database}
}

// RequestRepositoryReindex records a reindex request for every scope in one
// statement and returns each scope's stored watermark in UTC, ordered by scope
// ID. Scope IDs are trimmed, sorted, and deduplicated; an empty list or a
// blank scope ID is rejected before anything is written.
func (s RepositoryReindexStore) RequestRepositoryReindex(
	ctx context.Context,
	scopeIDs []string,
) ([]runtime.RepositoryReindexRequest, error) {
	if s.database == nil {
		return nil, errors.New("repository reindex store database is required")
	}
	normalized, err := normalizeReindexScopeIDs(scopeIDs)
	if err != nil {
		return nil, err
	}
	rows, err := s.database.QueryContext(ctx, requestRepositoryReindexQuery, normalized)
	if err != nil {
		return nil, fmt.Errorf("request repository reindex: %w", err)
	}
	defer func() { _ = rows.Close() }()

	requests := make([]runtime.RepositoryReindexRequest, 0, len(normalized))
	for rows.Next() {
		var request runtime.RepositoryReindexRequest
		if err := rows.Scan(&request.ScopeID, &request.RequestedAt); err != nil {
			return nil, fmt.Errorf("request repository reindex: %w", err)
		}
		request.RequestedAt = request.RequestedAt.UTC()
		requests = append(requests, request)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("request repository reindex: %w", err)
	}
	if len(requests) != len(normalized) {
		return nil, fmt.Errorf("request repository reindex: %d watermarks returned for %d scopes", len(requests), len(normalized))
	}
	slices.SortFunc(requests, func(a, b runtime.RepositoryReindexRequest) int {
		return strings.Compare(a.ScopeID, b.ScopeID)
	})
	return requests, nil
}

// RepositoryReindexWatermarks returns every per-repository watermark newer
// than after, keyed by scope ID, in UTC. Pass the fleet watermark, or the zero
// time to read every row.
func (s RepositoryReindexStore) RepositoryReindexWatermarks(
	ctx context.Context,
	after time.Time,
) (map[string]time.Time, error) {
	if s.database == nil {
		return nil, errors.New("repository reindex store database is required")
	}
	rows, err := s.database.QueryContext(ctx, repositoryReindexWatermarksQuery, after.UTC())
	if err != nil {
		return nil, fmt.Errorf("read repository reindex watermarks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	watermarks := make(map[string]time.Time)
	for rows.Next() {
		var scopeID string
		var requestedAt time.Time
		if err := rows.Scan(&scopeID, &requestedAt); err != nil {
			return nil, fmt.Errorf("read repository reindex watermarks: %w", err)
		}
		watermarks[scopeID] = requestedAt.UTC()
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read repository reindex watermarks: %w", err)
	}
	return watermarks, nil
}

func normalizeReindexScopeIDs(scopeIDs []string) ([]string, error) {
	if len(scopeIDs) == 0 {
		return nil, errors.New("request repository reindex: at least one scope id is required")
	}
	normalized := make([]string, 0, len(scopeIDs))
	for _, scopeID := range scopeIDs {
		trimmed := strings.TrimSpace(scopeID)
		if trimmed == "" {
			return nil, errors.New("request repository reindex: scope id must not be blank")
		}
		normalized = append(normalized, trimmed)
	}
	slices.Sort(normalized)
	return slices.Compact(normalized), nil
}
