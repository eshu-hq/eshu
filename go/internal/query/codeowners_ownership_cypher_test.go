// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

func TestCodeownersOwnershipCypherAnchorsOnRepositoryAndOrdersDeterministically(t *testing.T) {
	t.Parallel()

	queries := codeownersOwnershipCyphers("repo-1", -1, "", "", 51)
	if got, want := len(queries), 1; got != want {
		t.Fatalf("query count = %d, want %d without a cursor", got, want)
	}
	cypher, params := queries[0].cypher, queries[0].params

	for _, fragment := range []string{
		"MATCH (repo:Repository {id: $repo_id})-[rel:DECLARES_CODEOWNER]->(team:CodeownerTeam)",
		"team.ref IS NOT NULL AND team.ref <> ''",
		"rel.pattern IS NOT NULL AND rel.pattern <> ''",
		"rel.source_path IS NOT NULL AND rel.source_path <> ''",
		"RETURN rel.pattern AS pattern",
		"rel.source_path AS source_path",
		"rel.order_index AS order_index",
		"team.ref AS owner_ref",
		"ORDER BY rel.order_index, rel.pattern, team.ref",
		"LIMIT $limit",
	} {
		if !strings.Contains(cypher, fragment) {
			t.Fatalf("cypher = %q, want fragment %q", cypher, fragment)
		}
	}

	if got, want := params["repo_id"], "repo-1"; got != want {
		t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
	}
	if _, ok := params["after_order_index"]; ok {
		t.Fatalf("params contains after_order_index without a cursor: %#v", params)
	}
	if strings.Contains(cypher, "$after_") {
		t.Fatalf("cypher contains cursor predicate without a cursor: %q", cypher)
	}
	if got, want := params["limit"], 51; got != want {
		t.Fatalf("params[limit] = %#v, want %#v", got, want)
	}
}

func TestCodeownersOwnershipCypherThreadsKeysetCursorParams(t *testing.T) {
	t.Parallel()

	queries := codeownersOwnershipCyphers("repo-1", 3, "*.go", "@org/team-a", 10)
	if got, want := len(queries), 3; got != want {
		t.Fatalf("query count = %d, want %d with a cursor", got, want)
	}
	wantPredicates := []string{
		"rel.order_index > $after_order_index",
		"rel.order_index = $after_order_index AND rel.pattern > $after_pattern",
		"rel.order_index = $after_order_index AND rel.pattern = $after_pattern AND team.ref > $after_ref",
	}
	for i, query := range queries {
		if !strings.Contains(query.cypher, wantPredicates[i]) {
			t.Fatalf("query %d = %q, want predicate %q", i, query.cypher, wantPredicates[i])
		}
		if strings.Contains(query.cypher, " OR ") || strings.Contains(query.cypher, "\n  OR ") {
			t.Fatalf("query %d contains NornicDB-incompatible cursor OR: %q", i, query.cypher)
		}
		if got, want := query.params["after_order_index"], 3; got != want {
			t.Fatalf("query %d after_order_index = %#v, want %#v", i, got, want)
		}
		if got, want := query.params["limit"], 10; got != want {
			t.Fatalf("query %d limit = %#v, want %#v", i, got, want)
		}
	}
	if got, want := queries[1].params["after_pattern"], "*.go"; got != want {
		t.Fatalf("query 1 after_pattern = %#v, want %#v", got, want)
	}
	if got, want := queries[2].params["after_ref"], "@org/team-a"; got != want {
		t.Fatalf("query 2 after_ref = %#v, want %#v", got, want)
	}
}
