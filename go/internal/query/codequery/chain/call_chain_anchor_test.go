// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestBuildCallChainCypherNeo4jAnchorsCodeCallLabels proves the Neo4j-compat
// call-chain builder seeds its start/end anchors with the code-call source label
// disjunction (issue #3567). The prior `MATCH (start)` / `MATCH (end)` were
// unlabeled, so the Neo4j planner had no label/index to seed from and resolved
// the id/name predicate with an all-node scan. The label disjunction matches the
// exact CALLS-source label set the canonical writer projects, so every
// CALLS-reachable endpoint still resolves and the result set is unchanged.
func TestBuildCallChainCypherNeo4jAnchorsCodeCallLabels(t *testing.T) {
	t.Parallel()

	cypher, params := BuildCallChainCypher(Request{
		StartEntityID: "fn-1",
		EndEntityID:   "fn-3",
		MaxDepth:      5,
	}, querycontract.GraphBackendNeo4j, querycontract.RepositoryAccessFilter{AllScopes: true})

	if strings.Contains(cypher, "MATCH (start)\n") || strings.Contains(cypher, "MATCH (start)\t") {
		t.Fatalf("start anchor must not be an unlabeled all-node scan: %s", cypher)
	}
	if strings.Contains(cypher, "MATCH (end)") {
		t.Fatalf("end anchor must not be an unlabeled all-node scan: %s", cypher)
	}
	// Issue #7057: an entity-id anchor seeks the per-label uid uniqueness
	// constraints. The old WHERE (start.id = x OR start.uid = x) could not use
	// them and planned as a UnionNodeByLabelsScan over all six labels.
	if !strings.Contains(cypher, "MATCH (start:Function|Class|Struct|Interface|TypeAlias|File {uid: $start_entity_id})") {
		t.Fatalf("start anchor must seek the uid constraint on the code-call labels: %s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (end:Function|Class|Struct|Interface|TypeAlias|File {uid: $end_entity_id})") {
		t.Fatalf("end anchor must seek the uid constraint on the code-call labels: %s", cypher)
	}
	if strings.Contains(cypher, ".id =") {
		t.Fatalf("entity-id anchors must not use the unindexed id predicate: %s", cypher)
	}
	// Semantics preserved: CALLS traversal, projection, LIMIT.
	if !strings.Contains(cypher, "SHORTEST 1 (start)(()-[:CALLS]->(node)){1,5}(end)") {
		t.Fatalf("CALLS shortest-path traversal must be preserved: %s", cypher)
	}
	if got := params["start_entity_id"]; got != "fn-1" {
		t.Fatalf("params[start_entity_id] = %#v, want fn-1", got)
	}
	if got := params["end_entity_id"]; got != "fn-3" {
		t.Fatalf("params[end_entity_id] = %#v, want fn-3", got)
	}
}

// TestBuildCallChainCypherNeo4jAnchorsNameLookup proves the name-based lookup
// path is label-seeded too, so a start/end resolved by name does not fall back
// to an unlabeled scan.
func TestBuildCallChainCypherNeo4jAnchorsNameLookup(t *testing.T) {
	t.Parallel()

	cypher, _ := BuildCallChainCypher(Request{
		Start:    "handler",
		End:      "writer",
		RepoID:   "repo-1",
		MaxDepth: 3,
	}, querycontract.GraphBackendNeo4j, querycontract.RepositoryAccessFilter{AllScopes: true})

	if !strings.Contains(cypher, "MATCH (start:"+AnchorLabelDisjunction+")") {
		t.Fatalf("name-lookup start anchor must be label-seeded: %s", cypher)
	}
	if !strings.Contains(cypher, "start.name = $start") || !strings.Contains(cypher, "end.name = $end") {
		t.Fatalf("name predicates must be preserved: %s", cypher)
	}
	if !strings.Contains(cypher, "start.repo_id = $repo_id") || !strings.Contains(cypher, "end.repo_id = $repo_id") {
		t.Fatalf("repo scoping must be preserved: %s", cypher)
	}
}

// TestBuildCallChainCypherNornicDBUnchanged proves the NornicDB builder is not
// touched by the Neo4j-compat fix; it keeps its inline-property anchor shape.
func TestBuildCallChainCypherNornicDBUnchanged(t *testing.T) {
	t.Parallel()

	cypher, _ := BuildCallChainCypher(Request{
		StartEntityID: "fn-1",
		EndEntityID:   "fn-3",
		MaxDepth:      5,
	}, querycontract.GraphBackendNornicDB, querycontract.RepositoryAccessFilter{AllScopes: true})

	if !strings.Contains(cypher, "(start {uid: $start_entity_id})") {
		t.Fatalf("NornicDB start anchor must keep its inline-property shape: %s", cypher)
	}
	if strings.Contains(cypher, AnchorLabelDisjunction) {
		t.Fatalf("NornicDB builder must not adopt the Neo4j label disjunction: %s", cypher)
	}
}

// TestBuildCallChainCypherNeo4jMixedAnchorsKeepPredicates proves a request
// that names one endpoint by entity id and the other by name seeks the uid
// constraint for the id side only, and keeps the name, repo, and grant
// predicates in one WHERE after both anchors (issue #7057).
func TestBuildCallChainCypherNeo4jMixedAnchorsKeepPredicates(t *testing.T) {
	t.Parallel()

	access := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-1"}}
	cypher, params := BuildCallChainCypher(Request{
		StartEntityID: "fn-1",
		End:           "writer",
		RepoID:        "repo-1",
		MaxDepth:      4,
	}, querycontract.GraphBackendNeo4j, access)

	want := "\n\t\tMATCH (start:Function|Class|Struct|Interface|TypeAlias|File {uid: $start_entity_id})" +
		"\n\t\tMATCH (end:Function|Class|Struct|Interface|TypeAlias|File)" +
		"\n\t\tWHERE end.name = $end AND start.repo_id = $repo_id AND end.repo_id = $repo_id AND "
	if !strings.HasPrefix(cypher, want) {
		t.Fatalf("cypher prefix =\n%s\nwant prefix\n%s", cypher, want)
	}
	if strings.Contains(cypher, ".id =") {
		t.Fatalf("entity-id anchor must not use the unindexed id predicate: %s", cypher)
	}
	if got := params["start_entity_id"]; got != "fn-1" {
		t.Fatalf("params[start_entity_id] = %#v, want fn-1", got)
	}
	if got := params["end"]; got != "writer" {
		t.Fatalf("params[end] = %#v, want writer", got)
	}
}
