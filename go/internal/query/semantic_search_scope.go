// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"strings"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// ErrSemanticSearchScopeAmbiguous means one canonical repository id maps to
// multiple active ingestion scopes. Search must fail closed rather than read an
// arbitrary scope.
var ErrSemanticSearchScopeAmbiguous = errors.New("semantic search repository scope is ambiguous")

// SemanticSearchScopeResolver maps the authorized canonical repository id used
// by HTTP and MCP callers to the active ingestion scope that owns the search
// index generation.
type SemanticSearchScopeResolver interface {
	ResolveSemanticSearchScope(context.Context, string) (string, error)
	ResolveSemanticSearchRepositoryForScope(context.Context, string) (string, error)
}

const resolveSemanticSearchScopeQuery = `
SELECT scope_id
FROM ingestion_scopes
WHERE scope_kind = 'repository'
  AND active_generation_id IS NOT NULL
  AND payload->>'repo_id' = $1
ORDER BY observed_at DESC, scope_id ASC
LIMIT 2
`

const resolveSemanticSearchRepositoryForScopeQuery = `
SELECT COALESCE(payload->>'repo_id', '')
FROM ingestion_scopes
WHERE scope_kind = 'repository'
  AND active_generation_id IS NOT NULL
  AND scope_id = $1
LIMIT 1
`

// PostgresSemanticSearchScopeResolver resolves canonical repository ids from
// the relational repository catalog without exposing scope ids to callers.
type PostgresSemanticSearchScopeResolver struct {
	db pgstatus.Queryer
}

// NewPostgresSemanticSearchScopeResolver constructs the production resolver.
func NewPostgresSemanticSearchScopeResolver(db pgstatus.Queryer) PostgresSemanticSearchScopeResolver {
	if db == nil {
		return PostgresSemanticSearchScopeResolver{}
	}
	return PostgresSemanticSearchScopeResolver{db: db}
}

// ResolveSemanticSearchScope returns the sole active scope for repoID. An
// unindexed repository returns an empty scope; duplicate active mappings fail
// closed with ErrSemanticSearchScopeAmbiguous.
func (r PostgresSemanticSearchScopeResolver) ResolveSemanticSearchScope(
	ctx context.Context,
	repoID string,
) (string, error) {
	if r.db == nil {
		return "", fmt.Errorf("semantic search scope resolver requires a database")
	}
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return "", fmt.Errorf("semantic search scope resolver requires a repository id")
	}
	rows, err := r.db.QueryContext(ctx, resolveSemanticSearchScopeQuery, repoID)
	if err != nil {
		return "", fmt.Errorf("resolve semantic search repository scope: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var scopes []string
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			return "", fmt.Errorf("scan semantic search repository scope: %w", err)
		}
		if scopeID = strings.TrimSpace(scopeID); scopeID != "" {
			scopes = append(scopes, scopeID)
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate semantic search repository scopes: %w", err)
	}
	switch len(scopes) {
	case 0:
		return "", nil
	case 1:
		return scopes[0], nil
	default:
		return "", ErrSemanticSearchScopeAmbiguous
	}
}

// ResolveSemanticSearchRepositoryForScope validates that scopeID is an active
// repository scope and returns the canonical repository id stored with it. A
// missing or stale scope returns an empty id; malformed active scope metadata
// fails closed rather than weakening the repository filter on index reads.
func (r PostgresSemanticSearchScopeResolver) ResolveSemanticSearchRepositoryForScope(
	ctx context.Context,
	scopeID string,
) (string, error) {
	if r.db == nil {
		return "", fmt.Errorf("semantic search scope resolver requires a database")
	}
	scopeID = strings.TrimSpace(scopeID)
	if scopeID == "" {
		return "", fmt.Errorf("semantic search scope resolver requires a scope id")
	}
	rows, err := r.db.QueryContext(ctx, resolveSemanticSearchRepositoryForScopeQuery, scopeID)
	if err != nil {
		return "", fmt.Errorf("resolve semantic search scope repository: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", fmt.Errorf("iterate semantic search scope repository: %w", err)
		}
		return "", nil
	}
	var repoID string
	if err := rows.Scan(&repoID); err != nil {
		return "", fmt.Errorf("scan semantic search scope repository: %w", err)
	}
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return "", fmt.Errorf("semantic search active scope %q has no canonical repository id", scopeID)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate semantic search scope repository: %w", err)
	}
	return repoID, nil
}
