// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

func TestCodeownersOwnershipCypherAnchorsOnRepositoryAndOrdersDeterministically(t *testing.T) {
	t.Parallel()

	cypher, params := codeownersOwnershipCypher("repo-1", -1, "", "", 51)

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
	if got, want := params["after_order_index"], -1; got != want {
		t.Fatalf("params[after_order_index] = %#v, want %#v", got, want)
	}
	if got, want := params["limit"], 51; got != want {
		t.Fatalf("params[limit] = %#v, want %#v", got, want)
	}
}

func TestCodeownersOwnershipCypherThreadsKeysetCursorParams(t *testing.T) {
	t.Parallel()

	cypher, params := codeownersOwnershipCypher("repo-1", 3, "*.go", "@org/team-a", 10)

	if want := "$after_order_index < 0" +
		"\n       OR rel.order_index > $after_order_index" +
		"\n       OR (rel.order_index = $after_order_index AND rel.pattern > $after_pattern)" +
		"\n       OR (rel.order_index = $after_order_index AND rel.pattern = $after_pattern AND team.ref > $after_ref)"; !strings.Contains(cypher, want) {
		t.Fatalf("cypher = %q, want keyset cursor fragment %q", cypher, want)
	}
	if got, want := params["after_order_index"], 3; got != want {
		t.Fatalf("params[after_order_index] = %#v, want %#v", got, want)
	}
	if got, want := params["after_pattern"], "*.go"; got != want {
		t.Fatalf("params[after_pattern] = %#v, want %#v", got, want)
	}
	if got, want := params["after_ref"], "@org/team-a"; got != want {
		t.Fatalf("params[after_ref] = %#v, want %#v", got, want)
	}
}
