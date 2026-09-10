// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
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
	if !strings.Contains(cypher, "MATCH (start:"+AnchorLabelDisjunction+")") {
		t.Fatalf("start anchor must be label-seeded with the code-call source disjunction: %s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (end:"+AnchorLabelDisjunction+")") {
		t.Fatalf("end anchor must be label-seeded with the code-call source disjunction: %s", cypher)
	}
	// Semantics preserved: id/uid predicates, CALLS traversal, projection, LIMIT.
	if !strings.Contains(cypher, codemodel.GraphEntityIDPredicate("start", "$start_entity_id")) {
		t.Fatalf("start entity-id predicate must be preserved: %s", cypher)
	}
	if !strings.Contains(cypher, codemodel.GraphEntityIDPredicate("end", "$end_entity_id")) {
		t.Fatalf("end entity-id predicate must be preserved: %s", cypher)
	}
	if !strings.Contains(cypher, "shortestPath(") || !strings.Contains(cypher, "(start)-[:CALLS*1..5]->(end)") {
		t.Fatalf("CALLS shortestPath traversal must be preserved: %s", cypher)
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
