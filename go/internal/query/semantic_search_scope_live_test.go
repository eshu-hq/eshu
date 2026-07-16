// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestSemanticSearchScopeKnownTermPGXLive proves a canonical authorized
// repository id resolves to the active scope whose BM25 index contains a
// retained known term. It is read-only and skips unless explicitly enabled.
func TestSemanticSearchScopeKnownTermPGXLive(t *testing.T) {
	if os.Getenv("ESHU_SEMANTIC_SEARCH_SCOPE_LIVE") != "1" {
		t.Skip("set ESHU_SEMANTIC_SEARCH_SCOPE_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var expectedScopeID, repoID, knownTerm string
	err = db.QueryRowContext(ctx, `
WITH retained_scope AS (
    SELECT stats.scope_id, stats.generation_id
    FROM eshu_search_index_stats stats
    JOIN ingestion_scopes scope
      ON scope.scope_id = stats.scope_id
     AND scope.active_generation_id = stats.generation_id
    WHERE stats.document_count > 0
      AND NULLIF(scope.payload->>'repo_id', '') IS NOT NULL
    ORDER BY stats.document_count DESC, stats.scope_id ASC
    LIMIT 1
)
SELECT retained.scope_id, scope.payload->>'repo_id', term.term
FROM retained_scope retained
JOIN ingestion_scopes scope ON scope.scope_id = retained.scope_id
JOIN LATERAL (
    SELECT indexed.term
    FROM eshu_search_index_terms indexed
    WHERE indexed.scope_id = retained.scope_id
      AND indexed.generation_id = retained.generation_id
      AND length(indexed.term) >= 4
    LIMIT 1
) term ON true
`).Scan(&expectedScopeID, &repoID, &knownTerm)
	if err != nil {
		t.Fatalf("select retained semantic-search proof term: %v", err)
	}

	resolver := NewPostgresSemanticSearchScopeResolver(pgstatus.SQLDB{DB: db})
	resolvedScopeID, err := resolver.ResolveSemanticSearchScope(ctx, repoID)
	if err != nil {
		t.Fatalf("ResolveSemanticSearchScope() error = %v", err)
	}
	if got, want := resolvedScopeID, expectedScopeID; got != want {
		t.Fatalf("resolved scope = %q, want retained active scope %q", got, want)
	}
	resolvedRepoID, err := resolver.ResolveSemanticSearchRepositoryForScope(ctx, expectedScopeID)
	if err != nil {
		t.Fatalf("ResolveSemanticSearchRepositoryForScope() error = %v", err)
	}
	if got, want := resolvedRepoID, repoID; got != want {
		t.Fatalf("resolved repository = %q, want retained canonical repository %q", got, want)
	}

	result, err := NewPostgresSemanticSearchIndexStore(db).Search(ctx, semanticSearchIndexQuery{
		Request: searchretrieval.Request{
			Query:   knownTerm,
			Scope:   searchretrieval.Scope{RepoID: repoID},
			Mode:    searchbench.ModeKeyword,
			Limit:   5,
			Timeout: 5 * time.Second,
		},
		ScopeID: resolvedScopeID,
		RepoID:  repoID,
	})
	if err != nil {
		t.Fatalf("Search() known retained term error = %v", err)
	}
	if result.IndexedDocumentCount == 0 {
		t.Fatal("Search() indexed document count = 0, want retained active index")
	}
	if len(result.Candidates) == 0 {
		t.Fatal("Search() candidates = 0, want known retained term hit")
	}
}
