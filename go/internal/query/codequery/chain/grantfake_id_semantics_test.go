// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"testing"
)

// The fake must not turn `node.id IN $allowed_repository_ids` into a check on
// the node's repository. #7220: a grant predicate that compares a
// non-Repository node's own id with a granted repository id admits any node
// whose id collides with the grant, and a fake that read it as a repo_id test
// would keep passing on that statement.

func collisionGrantParams() map[string]any {
	return map[string]any{
		"allowed_repository_ids": []string{"repo-00"},
		"allowed_scope_ids":      []string{},
	}
}

// TestCallChainHopAdmitsJudgesNodeIDPredicateOnTheNodeID: a hop predicate on
// `node.id` is evaluated against the node's own id, so a Function whose id
// equals a granted repository id is admitted (the leak) rather than judged by
// its repo_id (the mask).
func TestCallChainHopAdmitsJudgesNodeIDPredicateOnTheNodeID(t *testing.T) {
	t.Parallel()

	node := GrantEntity{UID: "repo-00", RepoID: "repo-29"}
	params := collisionGrantParams()

	if !callChainHopAdmits("node.id IN $allowed_repository_ids", node, params) {
		t.Fatal("node.id term must admit a node whose own id is granted, so a bare-id predicate on a non-Repository hop surfaces as a leak")
	}
	if callChainHopAdmits("node.repo_id IN $allowed_repository_ids", node, params) {
		t.Fatal("node.repo_id term must judge the owning repository (repo-29), which is not granted")
	}
	owned := GrantEntity{UID: "fn-1", RepoID: "repo-00"}
	if !callChainHopAdmits("node.repo_id IN $allowed_repository_ids", owned, params) {
		t.Fatal("node.repo_id term must admit a node owned by a granted repository")
	}
	if callChainHopAdmits("node.id IN $allowed_repository_ids", owned, params) {
		t.Fatal("node.id term must not admit a node whose own id is not granted, even when its repository is")
	}
}

// TestGrantGraphMetadataRowsRejectsRepoIDPredicateWithoutRepositoryBinding: the
// anchor predicate `repo.id IN $allowed_repository_ids` is only sound when the
// statement binds repo as :Repository. A statement that drops the label is
// recorded as unread rather than answered.
func TestGrantGraphMetadataRowsRejectsRepoIDPredicateWithoutRepositoryBinding(t *testing.T) {
	t.Parallel()

	bound := "MATCH (e {uid: $entity_id})<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)\n" +
		"WHERE (repo.id IN $allowed_repository_ids OR repo.id IN $allowed_scope_ids)\nRETURN e.uid AS id"
	unbound := "MATCH (e {uid: $entity_id})<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo)\n" +
		"WHERE (repo.id IN $allowed_repository_ids OR repo.id IN $allowed_scope_ids)\nRETURN e.uid AS id"

	params := collisionGrantParams()
	params["entity_id"] = "fn-1"
	entities := []GrantEntity{{UID: "fn-1", RepoID: "repo-00"}}

	good := &GrantGraph{Entities: entities}
	if rows, err := good.Run(context.Background(), bound, params); err != nil || len(rows) != 1 {
		t.Fatalf("bound statement: rows = %v err = %v, want one granted row", rows, err)
	}
	if len(good.ParseFailures) != 0 {
		t.Fatalf("bound statement recorded parse failures: %v", good.ParseFailures)
	}

	bad := &GrantGraph{Entities: entities}
	rows, _ := bad.Run(context.Background(), unbound, params)
	if len(rows) != 0 {
		t.Fatalf("unbound statement answered %v, want no rows", rows)
	}
	if len(bad.ParseFailures) != 1 {
		t.Fatalf("unbound statement parse failures = %v, want it recorded", bad.ParseFailures)
	}
}

// TestRepoIDPredicateMatchesTheAliasExactly: a predicate on `source_repo.id` is
// not a predicate on `repo.id`, so it cannot make an unbound `repo` alias look
// unread.
func TestRepoIDPredicateMatchesTheAliasExactly(t *testing.T) {
	t.Parallel()

	if !repoIDPredicate([]string{"(repo.id IN $allowed_repository_ids OR repo.id IN $allowed_scope_ids)"}, "repo") {
		t.Fatal("repo.id predicate not recognised")
	}
	if repoIDPredicate([]string{"(source_repo.id IN $allowed_repository_ids)"}, "repo") {
		t.Fatal("source_repo.id predicate read as repo.id")
	}
}
