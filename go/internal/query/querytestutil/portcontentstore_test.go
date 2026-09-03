// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestPortContentStoreListRepoEntitiesByTypeFiltersBeforeLimit pins the
// predicate that a mutation found unguarded: dropping the entity_type filter
// from ListRepoEntitiesByType failed no test in package query, because the
// tests covering that predicate build their own doubles
// (boundedK8sFakeContentStore, truncationFakeContentStore,
// entityContextFakeContentStore) rather than this one (#6060).
//
// Both halves matter and neither implies the other. Filtering alone is not
// enough, because the production ContentReader.ListRepoEntitiesByType filters
// on entity_type BEFORE applying LIMIT; a double that limited first would fill
// its page with the wrong types and return fewer matches than the real query,
// so a caller sizing a limit against it would see truncation the database
// would not produce.
func TestPortContentStoreListRepoEntitiesByTypeFiltersBeforeLimit(t *testing.T) {
	t.Parallel()

	// The two non-matching rows lead deliberately. A double that limited
	// before filtering would consume its budget on them and return nothing.
	store := querytestutil.PortContentStore{
		Entities: []querycontract.EntityContent{
			{EntityID: "svc-1", RepoID: "repo-1", EntityType: "Service"},
			{EntityID: "svc-2", RepoID: "repo-1", EntityType: "Service"},
			{EntityID: "k8s-1", RepoID: "repo-1", EntityType: "K8sResource"},
			{EntityID: "k8s-2", RepoID: "repo-1", EntityType: "K8sResource"},
		},
	}

	got, err := store.ListRepoEntitiesByType(t.Context(), "repo-1", "K8sResource", 2)
	if err != nil {
		t.Fatalf("ListRepoEntitiesByType() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListRepoEntitiesByType() returned %d rows, want 2: %+v", len(got), got)
	}
	for _, entity := range got {
		if entity.EntityType != "K8sResource" {
			t.Errorf(
				"ListRepoEntitiesByType() returned entity %q of type %q, want only K8sResource",
				entity.EntityID,
				entity.EntityType,
			)
		}
	}
}

// TestPortContentStoreListRepoEntitiesByTypeScopesToRepo pins the repository
// half of the same predicate, including the wildcard a fixture opts into by
// leaving RepoID empty. Tests that do not care about repo scoping rely on that
// wildcard, so tightening it to an exact match would break them; dropping the
// repo check entirely would leak another repository's rows.
func TestPortContentStoreListRepoEntitiesByTypeScopesToRepo(t *testing.T) {
	t.Parallel()

	store := querytestutil.PortContentStore{
		Entities: []querycontract.EntityContent{
			{EntityID: "mine", RepoID: "repo-1", EntityType: "K8sResource"},
			{EntityID: "theirs", RepoID: "repo-2", EntityType: "K8sResource"},
			{EntityID: "anyrepo", RepoID: "", EntityType: "K8sResource"},
		},
	}

	got, err := store.ListRepoEntitiesByType(t.Context(), "repo-1", "K8sResource", 0)
	if err != nil {
		t.Fatalf("ListRepoEntitiesByType() error = %v, want nil", err)
	}

	ids := make([]string, 0, len(got))
	for _, entity := range got {
		ids = append(ids, entity.EntityID)
	}
	want := []string{"mine", "anyrepo"}
	if len(ids) != len(want) {
		t.Fatalf("ListRepoEntitiesByType() ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ListRepoEntitiesByType() ids = %v, want %v", ids, want)
		}
	}
}
