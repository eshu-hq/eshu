// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestBuildScopeRepositorySourceKeyMatchesMetadataRepoID binds the
// producer-side half of the #5192 contract: the console operations-board
// link (repositorySourceHref in apps/console/src/api/operationsBoard.ts)
// relies on ingestion_scopes.source_key equalling the repository catalog id
// -- coalesce(payload->>'repo_id', payload->>'id', scope_id) in
// repositoryCatalogIDExpr (go/internal/query/content_reader_repository_catalog.go,
// pinned by TestRepositoryCatalogIDExprCoalesceOrder). Because
// upsertIngestionScope (go/internal/storage/postgres/ingestion.go) marshals
// scope.Metadata directly into the payload column and reads source_key from
// scope.Metadata["source_key"] (scopeSourceKey), payload->>'repo_id' at
// storage time equals whatever buildScope wrote to Metadata["repo_id"] here.
// This test asserts buildScope keeps Metadata["source_key"] and
// Metadata["repo_id"] mirrored to the same non-empty repo.ID for
// repository-kind scopes. If a future buildScope change stops mirroring
// repo.ID into either field, this test fails before the divergence ever
// reaches Postgres.
func TestBuildScopeRepositorySourceKeyMatchesMetadataRepoID(t *testing.T) {
	t.Parallel()

	repo := repositoryidentity.Metadata{
		ID:        "r_example1234",
		Name:      "example-repo",
		RepoSlug:  "example-org/example-repo",
		RemoteURL: "https://github.com/example-org/example-repo.git",
		LocalPath: "/repos/example-repo",
	}

	got := buildScope(repo)

	if got.ScopeKind != scope.KindRepository {
		t.Fatalf("ScopeKind = %q, want %q", got.ScopeKind, scope.KindRepository)
	}

	sourceKey := got.Metadata["source_key"]
	repoID := got.Metadata["repo_id"]

	if sourceKey == "" {
		t.Fatal("Metadata[\"source_key\"] is empty, want non-empty")
	}
	if repoID == "" {
		t.Fatal("Metadata[\"repo_id\"] is empty, want non-empty")
	}
	if sourceKey != repoID {
		t.Fatalf("Metadata[\"source_key\"] = %q, Metadata[\"repo_id\"] = %q; want equal", sourceKey, repoID)
	}
	if sourceKey != repo.ID {
		t.Fatalf("Metadata[\"source_key\"] = %q, want repo.ID %q", sourceKey, repo.ID)
	}
}
