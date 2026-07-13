// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestRepositoryCatalogIDExprCoalesceOrder pins the catalog-side half of the
// #5192 contract: the console operations-board link
// (repositorySourceHref in apps/console/src/api/operationsBoard.ts) reads
// ingestion_scopes.source_key as a repository's catalog id, and
// repositoryCatalogIDExpr is what ListRepositories and MatchRepositories
// select as that same id from the relational catalog (both interpolate this
// exact constant into their SQL text, so pinning the constant pins the
// queries). buildScope (go/internal/collector/git_source_processing.go)
// mirrors repo.ID into payload->>'repo_id' -- see
// TestBuildScopeRepositorySourceKeyMatchesMetadataRepoID in
// go/internal/collector/git_source_processing_test.go -- so this coalesce
// must keep payload->>'repo_id' as its first, highest-priority branch. If a
// future change reorders the coalesce (for example preferring
// payload->>'id') or drops a branch, the catalog id can diverge from
// source_key even though buildScope never changed, and the board's links
// silently point at empty freshness pages. This test fails the moment that
// order changes.
func TestRepositoryCatalogIDExprCoalesceOrder(t *testing.T) {
	t.Parallel()

	const want = "coalesce(payload->>'repo_id', payload->>'id', scope_id)"
	if repositoryCatalogIDExpr != want {
		t.Fatalf("repositoryCatalogIDExpr = %q, want %q", repositoryCatalogIDExpr, want)
	}
}
