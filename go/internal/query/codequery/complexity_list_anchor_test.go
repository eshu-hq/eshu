// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestComplexityListAnchorSeedsFromRepositoryWhenRepoIDKnown pins the #7006
// fix: a known repo_id must seed the complexity-list walk from the small,
// id-indexed Repository label instead of the broad Function label. The
// prior shape kept the required MATCH direction Function-first even once a
// repo_id or scoped grant was known, so it paid a whole-corpus Function scan
// on every repo-scoped call -- proven live on ops-qa to blow the 10s
// bounded-read deadline on a repository with zero matching rows, while a
// Repository-first anchor on the same repository returned in under 3s. See
// docs/public/reference/cypher-performance.md.
func TestComplexityListAnchorSeedsFromRepositoryWhenRepoIDKnown(t *testing.T) {
	t.Parallel()

	cypher, params := complexityListAnchor(querycontract.RepositoryAccessFilter{AllScopes: true}, "repo-payments")

	if !strings.HasPrefix(strings.TrimSpace(cypher), "MATCH (repo:Repository)") {
		t.Fatalf("cypher does not open on a Repository anchor:\n%s", cypher)
	}
	if !strings.Contains(cypher, "repo.id = $repo_id") {
		t.Fatalf("cypher missing repo_id predicate on the Repository anchor:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (repo)-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e:Function)") {
		t.Fatalf("cypher does not walk forward from Repository to Function:\n%s", cypher)
	}
	if strings.Contains(cypher, "MATCH (e:Function)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)") {
		t.Fatalf("cypher still anchors on the broad Function label for a repo_id-scoped call:\n%s", cypher)
	}
	if got, want := params["repo_id"], "repo-payments"; got != want {
		t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
	}
}

// TestComplexityListAnchorSeedsFromRepositoryWhenScopedWithNoRepoID pins the
// same fix for a scoped caller with a grant but no explicit repo_id.
func TestComplexityListAnchorSeedsFromRepositoryWhenScopedWithNoRepoID(t *testing.T) {
	t.Parallel()

	access := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-payments"}}
	cypher, params := complexityListAnchor(access, "")

	if !strings.HasPrefix(strings.TrimSpace(cypher), "MATCH (repo:Repository)") {
		t.Fatalf("cypher does not open on a Repository anchor:\n%s", cypher)
	}
	if !strings.Contains(cypher, "(repo.id IN $allowed_repository_ids OR repo.id IN $allowed_scope_ids)") {
		t.Fatalf("cypher missing the scoped grant predicate on the Repository anchor:\n%s", cypher)
	}
	if _, ok := params["allowed_repository_ids"]; !ok {
		t.Fatalf("params = %#v, want allowed_repository_ids bound", params)
	}
}

// TestComplexityListAnchorKeepsFunctionFirstWhenFullyUnscoped pins the
// unchanged half: no repo_id and no scope keeps the corpus-wide
// Function-first, OPTIONAL-MATCH-attributed shape.
func TestComplexityListAnchorKeepsFunctionFirstWhenFullyUnscoped(t *testing.T) {
	t.Parallel()

	cypher, params := complexityListAnchor(querycontract.RepositoryAccessFilter{AllScopes: true}, "")

	if !strings.Contains(cypher, "MATCH (e:Function)") || !strings.Contains(cypher, "OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)") {
		t.Fatalf("fully unscoped cypher lost its Function-first, OPTIONAL-attributed shape:\n%s", cypher)
	}
	if params != nil {
		t.Fatalf("params = %#v, want nil for the fully unscoped shape", params)
	}
}
