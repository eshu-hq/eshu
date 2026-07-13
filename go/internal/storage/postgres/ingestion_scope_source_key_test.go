// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestScopeSourceKeyUsesMetadataSourceKeyForRepositoryScope pins the storage
// bridge in the #5192 contract chain: buildScope
// (go/internal/collector/git_source_processing.go) writes repo.ID into both
// Metadata["source_key"] and Metadata["repo_id"] (proven by
// TestBuildScopeRepositorySourceKeyMatchesMetadataRepoID in
// git_source_processing_test.go), and upsertIngestionScope (ingestion.go:451)
// calls scopeSourceKey to compute the ingestion_scopes.source_key column it
// persists -- the same column the console operations-board link
// (repositorySourceHref in apps/console/src/api/operationsBoard.ts) expects
// to equal repositoryCatalogIDExpr's
// coalesce(payload->>'repo_id', payload->>'id', scope_id) (pinned by
// TestRepositoryCatalogIDExprCoalesceOrder in
// go/internal/query/content_reader_repository_catalog_test.go).
//
// The fixture intentionally sets ScopeID and Metadata["repo_id"] to values
// DIFFERENT from Metadata["source_key"], so a scopeSourceKey regression that
// reads a different key -- ScopeID, "repo_id", or anything other than
// "source_key" -- fails this assertion instead of accidentally matching by
// coincidence.
func TestScopeSourceKeyUsesMetadataSourceKeyForRepositoryScope(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:       "git-repository-scope:distinct-from-source-key",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "distinct-from-source-key",
		Metadata: map[string]string{
			"repo_id":    "distinct-repo-id-value",
			"repo_name":  "example-repo",
			"source_key": "the-real-source-key-value",
		},
	}

	got := scopeSourceKey(scopeValue)
	const want = "the-real-source-key-value"
	if got != want {
		t.Fatalf("scopeSourceKey() = %q, want %q (Metadata[\"source_key\"])", got, want)
	}
	if got == scopeValue.ScopeID {
		t.Fatalf("scopeSourceKey() = %q equals ScopeID; the ScopeID fallback must not fire when Metadata[\"source_key\"] is set", got)
	}
	if got == scopeValue.Metadata["repo_id"] {
		t.Fatalf("scopeSourceKey() = %q equals Metadata[\"repo_id\"]; want it read from Metadata[\"source_key\"] specifically, not repo_id", got)
	}
}

// TestScopeSourceKeyFallsBackToScopeIDWhenMetadataSourceKeyMissing documents
// and pins the ONLY conditions under which scopeSourceKey may fall back to
// ScopeID: a nil Metadata map, a missing "source_key" entry, or a
// whitespace-only value. buildScope always populates a non-empty
// Metadata["source_key"] for repository scopes (see
// TestBuildScopeRepositorySourceKeyMatchesMetadataRepoID in
// git_source_processing_test.go), so in production this fallback never fires
// for repository-kind scopes today. This test exists so a future change
// cannot silently widen the fallback condition -- for example treating a
// present-but-unexpected key as "missing" -- without an explicit test update
// here, which would otherwise let the fallback quietly start covering
// repository scopes too.
func TestScopeSourceKeyFallsBackToScopeIDWhenMetadataSourceKeyMissing(t *testing.T) {
	t.Parallel()

	tests := map[string]scope.IngestionScope{
		"nil metadata": {
			ScopeID:  "scope-nil-metadata",
			Metadata: nil,
		},
		"missing source_key entry": {
			ScopeID:  "scope-missing-key",
			Metadata: map[string]string{"repo_id": "some-repo-id"},
		},
		"whitespace-only source_key": {
			ScopeID:  "scope-whitespace-key",
			Metadata: map[string]string{"source_key": "   "},
		},
	}

	for name, scopeValue := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := scopeSourceKey(scopeValue)
			if got != scopeValue.ScopeID {
				t.Fatalf("scopeSourceKey() = %q, want fallback to ScopeID %q", got, scopeValue.ScopeID)
			}
		})
	}
}
