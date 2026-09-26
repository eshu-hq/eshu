// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package quality

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestBuildCypherAnchorsOnRepositoryWhenRepoIDKnown pins the #7006 fix: a
// known repo_id must seed the walk from the small, id-indexed Repository
// label instead of the broad Function label. The prior shape anchored on
// Function unconditionally, so a repo_id-scoped call still paid a
// whole-corpus Function scan -- proven live on ops-qa to blow the 10s
// bounded-read deadline. See docs/public/reference/cypher-performance.md.
func TestBuildCypherAnchorsOnRepositoryWhenRepoIDKnown(t *testing.T) {
	t.Parallel()

	req := Request{Check: CheckRefactor, RepoID: "repo-payments", Limit: 10, MinLines: 20, MinArguments: 5, MinComplexity: 10}
	cypher, params := BuildCypher(req, querycontract.RepositoryAccessFilter{AllScopes: true})

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

// TestBuildCypherAnchorsOnRepositoryWhenScopedWithNoRepoID pins the same fix
// for a scoped caller with a grant but no explicit repo_id: the caller's
// grant is still a Repository-side predicate, so it seeds the same
// Repository-first anchor as an explicit repo_id.
func TestBuildCypherAnchorsOnRepositoryWhenScopedWithNoRepoID(t *testing.T) {
	t.Parallel()

	access := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-payments"}}
	req := Request{Check: CheckRefactor, Limit: 10, MinLines: 20, MinArguments: 5, MinComplexity: 10}
	cypher, params := BuildCypher(req, access)

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

// TestBuildCypherKeepsFunctionFirstAnchorWhenFullyUnscoped pins the
// unchanged half: an unscoped caller with no repo_id keeps the corpus-wide
// Function-first shape, since there is no Repository identity to seed the
// walk from.
func TestBuildCypherKeepsFunctionFirstAnchorWhenFullyUnscoped(t *testing.T) {
	t.Parallel()

	req := Request{Check: CheckRefactor, Limit: 10, MinLines: 20, MinArguments: 5, MinComplexity: 10}
	cypher, _ := BuildCypher(req, querycontract.RepositoryAccessFilter{AllScopes: true})

	if !strings.Contains(cypher, "MATCH (e:Function)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)") {
		t.Fatalf("fully unscoped cypher lost its Function-first anchor:\n%s", cypher)
	}
}
