// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/workloadid"
)

// TestListCatalogWorkloadIdentitiesSynthesizeScopedIDs is the T5 table for
// the catalog identity seam (compatibility doc section 1.4 sites table):
// identity-store rows synthesize their ids from (repo_id, name) through
// workloadid.NewWorkloadID, and catalogWorkloadKey keys by (repo_id, name) so
// same-named split siblings in two repositories stop merging into one row.
// A graph row and an identity row for the SAME pair still dedupe, and a
// blank name still yields no row.
func TestListCatalogWorkloadIdentitiesSynthesizeScopedIDs(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: content.FakePortContentStore{
			WorkloadIdentities: []repository.CatalogWorkloadIdentityEntry{
				{Name: "checkout", RepoID: "repository:r_a", RepoName: "alpha"},
				{Name: "checkout", RepoID: "repository:r_b", RepoName: "beta"},
				{Name: "workload:legacy-prefixed", RepoID: "repository:r_a", RepoName: "alpha"},
				{Name: "   ", RepoID: "repository:r_a", RepoName: "alpha"},
				{Name: "checkout", RepoID: "   ", RepoName: "alpha"},
			},
		},
	}
	rows, _, err := handler.listCatalogWorkloadIdentitiesFromContent(context.Background(), 10)
	if err != nil {
		t.Fatalf("listCatalogWorkloadIdentitiesFromContent() error = %v, want nil", err)
	}
	// Every row's id is what the constructor builds for its own pair, so the
	// step-6 re-key flip propagates here with no further edit; the sibling
	// rows below prove the merge no longer needs distinct ids to stay split.
	seenPairs := map[string]bool{}
	for _, row := range rows {
		if got, want := row.ID, workloadid.NewWorkloadID(row.RepoID, row.Name).String(); got != want {
			t.Fatalf("row id = %q, want constructor output %q for (%q, %q)", got, want, row.RepoID, row.Name)
		}
		seenPairs[row.RepoID+"\x00"+row.Name] = true
	}
	for _, want := range []string{
		"repository:r_a\x00checkout",
		"repository:r_b\x00checkout",
		"repository:r_a\x00legacy-prefixed",
	} {
		if !seenPairs[want] {
			t.Fatalf("no identity row for pair %q (rows = %#v)", want, rows)
		}
	}
	if len(rows) != 3 {
		t.Fatalf("identity rows = %d, want 3 (blank name and blank repo yield no row)", len(rows))
	}

	// Split siblings must not merge: two repositories, one shared name.
	merged, _ := mergeCatalogWorkloads(nil, rows, 10)
	checkouts := 0
	for _, row := range merged {
		if row.Name == "checkout" {
			checkouts++
		}
	}
	if checkouts != 2 {
		t.Fatalf("merged checkout rows = %d, want 2 (siblings keyed by repo, not merged)", checkouts)
	}

	// The same (repo_id, name) pair from graph and identity still dedupes,
	// while the other repository's sibling survives: graph r_a/checkout +
	// identity r_b/checkout + identity r_a/legacy-prefixed.
	graphRows := []catalogWorkload{{
		ID:     workloadid.NewWorkloadID("repository:r_a", "checkout").String(),
		Name:   "checkout",
		Kind:   "service",
		RepoID: "repository:r_a",
	}}
	merged, _ = mergeCatalogWorkloads(graphRows, rows, 10)
	if len(merged) != 3 {
		t.Fatalf("merged rows = %d, want 3 (same pair dedupes, sibling survives): %#v", len(merged), merged)
	}
	alphaCheckouts := 0
	for _, row := range merged {
		if row.Name == "checkout" && row.RepoID == "repository:r_a" {
			alphaCheckouts++
		}
	}
	if alphaCheckouts != 1 {
		t.Fatalf("merged r_a checkout rows = %d, want 1: %#v", alphaCheckouts, merged)
	}
}

// A graph row missing Name falls back to its id for the merge key. Post-flip
// that id is scoped, so the fallback must derive the bare last segment:
// otherwise the nameless graph twin keys differently from its named identity
// twin and the same pair stops deduping (#5385 T4 review).
func TestMergeCatalogWorkloadsDedupesNamelessScopedGraphRow(t *testing.T) {
	t.Parallel()

	scoped := workloadid.NewWorkloadID("repository:r_a", "checkout").String()
	graphRows := []catalogWorkload{{
		ID:     scoped,
		RepoID: "repository:r_a",
	}}
	identityRows := []catalogWorkload{{
		ID:     scoped,
		Name:   "checkout",
		RepoID: "repository:r_a",
	}}
	merged, _ := mergeCatalogWorkloads(graphRows, identityRows, 10)
	if len(merged) != 1 {
		t.Fatalf("merged rows = %d, want 1 (nameless scoped graph row dedupes with its named twin): %#v", len(merged), merged)
	}
}
