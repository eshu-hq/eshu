// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// grantTestContentStore serves fixed cross-repo rows from the indexed
// reference path regardless of kind, so grant tests control exactly which
// content-only repositories the consumer search merges.
type grantTestContentStore struct {
	querycontract.ContentStore
	rows []querycontract.FileContent
}

func (s grantTestContentStore) SearchFileReferenceAnyRepo(_ context.Context, _, _ string, _ int) ([]querycontract.FileContent, bool, error) {
	return s.rows, true, nil
}

func grantTestScopedContext(granted ...string) context.Context {
	return auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		AllowedRepositoryIDs: granted,
	})
}

func grantTestContentRows() []querycontract.FileContent {
	return []querycontract.FileContent{
		{RepoID: "repository:granted-content", RelativePath: "deploy/values.yaml"},
		{RepoID: "repository:foreign-content", RelativePath: "deploy/values.yaml"},
	}
}

// TestConsumerEnrichmentDropsUngrantedContentRepos proves the story/context
// path (#10560): candidates arrive grant-filtered from query_enrichment.go,
// but content-search rows name arbitrary repositories, so the merge must bind
// content-derived rows to the caller's grant pre-merge -- the same predicate
// the paged list tool applies post-merge
// (FilterRowsByRepoIDForAccess). A content-only repository outside the grant
// must be absent from the merged set.
func TestConsumerEnrichmentDropsUngrantedContentRepos(t *testing.T) {
	t.Parallel()

	ctx := grantTestScopedContext("repository:granted-candidate", "repository:granted-content")
	candidates := []ProvisioningRepositoryCandidate{{
		RepoID: "repository:granted-candidate", RepoName: "org/granted",
		RelationshipTypes: []string{"USES_MODULE"},
	}}
	consumers, sources, err := LoadConsumerRepositoryEnrichmentWithSources(
		ctx, nil, grantTestContentStore{rows: grantTestContentRows()},
		"repository:svc", "svc", []string{"shop.example.test"}, 10,
		candidates, false, false,
	)
	if err != nil {
		t.Fatalf("LoadConsumerRepositoryEnrichmentWithSources() error = %v, want nil", err)
	}
	for _, entry := range consumers {
		if id := querycontract.StringVal(entry, "repo_id"); id == "repository:foreign-content" {
			t.Fatalf("merged ungranted content-only repo %q for a scoped caller", id)
		}
	}
	if got, want := len(consumers), 2; got != want {
		t.Fatalf("len(consumers) = %d, want %d (granted candidate + granted content repo)", got, want)
	}
	if sources.Any() {
		t.Fatal("sources.Any() = true, want false (no bound fired on this read)")
	}
}

// TestConsumerEnrichmentEmptiedSetStillDisclosesTruncation proves the
// grant filter never converts a bound disclosure into a silent complete
// answer: when the upstream candidate read hit its bound but every surviving
// row falls outside the caller's grant, the set is empty and the truncation
// signal stays set -- the same emptied-by-filter reasoning that keeps
// dependents_truncated set on the story path.
func TestConsumerEnrichmentEmptiedSetStillDisclosesTruncation(t *testing.T) {
	t.Parallel()

	ctx := grantTestScopedContext("repository:elsewhere")
	consumers, sources, err := LoadConsumerRepositoryEnrichmentWithSources(
		ctx, nil, grantTestContentStore{rows: grantTestContentRows()},
		"repository:svc", "svc", []string{"shop.example.test"}, 10,
		nil, true, false,
	)
	if err != nil {
		t.Fatalf("LoadConsumerRepositoryEnrichmentWithSources() error = %v, want nil", err)
	}
	if len(consumers) != 0 {
		t.Fatalf("len(consumers) = %d, want 0 (every content row falls outside the grant)", len(consumers))
	}
	if !sources.Any() {
		t.Fatal("sources.Any() = false, want true (the upstream candidate bound fired)")
	}
}
