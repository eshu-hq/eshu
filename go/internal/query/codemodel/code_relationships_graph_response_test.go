// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestRelationshipGraphRowCypherUsesBareEntityScan proves
// RelationshipGraphRowCypher keeps its original bare "MATCH (e)" scan
// behavior for callers with no known repository (the entity_id and
// name-only branches in relationship_handlers.go), unchanged by the
// RelationshipGraphRowCypherAnchored split (issue #6786 defect 2).
func TestRelationshipGraphRowCypherUsesBareEntityScan(t *testing.T) {
	t.Parallel()

	cypher := RelationshipGraphRowCypher("e.id = $entity_id")
	if got, want := cypher, RelationshipGraphRowCypherAnchored("MATCH (e)", "e.id = $entity_id"); got != want {
		t.Fatalf("RelationshipGraphRowCypher diverged from RelationshipGraphRowCypherAnchored(\"MATCH (e)\", ...):\ngot:\n%s\nwant:\n%s", got, want)
	}
	if got := cypher; !strings.Contains(got, "MATCH (e) WHERE e.id = $entity_id") {
		t.Fatalf("cypher does not open with the bare entity scan:\n%s", got)
	}
}

// TestBuildTransitiveRelationshipRowsCypherMatchesBFSRule proves the
// Neo4j-compat transitive traversal emits the BFS contract the NornicDB
// route implements in Go (issue #6849): each reachable node once, at its
// shortest depth, with the start node excluded. The old statement returned
// every matching path, so a node appeared at several depths and a cycle
// returned the start node as its own callee. Both directions on both
// backend spellings must aggregate to the minimum path length per node
// and filter the anchor out.
func TestBuildTransitiveRelationshipRowsCypherMatchesBFSRule(t *testing.T) {
	t.Parallel()

	backends := []querycontract.GraphBackend{
		querycontract.GraphBackendNeo4j,
		querycontract.GraphBackendNornicDB,
	}
	for _, backend := range backends {
		for _, direction := range []string{"outgoing", "incoming"} {
			name := fmt.Sprintf("%s/%s", backend, direction)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				cypher, _ := BuildTransitiveRelationshipRowsCypher("entity-1", direction, 4, backend)
				if !strings.Contains(cypher, "min(length(path))") {
					t.Fatalf("%s: cypher does not aggregate to the shortest depth:\n%s", name, cypher)
				}
				if strings.Contains(cypher, "length(path) as depth") {
					t.Fatalf("%s: cypher still returns every path length as a row:\n%s", name, cypher)
				}
			})
		}
	}

	outgoing, _ := BuildTransitiveRelationshipRowsCypher("entity-1", "outgoing", 4, querycontract.GraphBackendNeo4j)
	if !strings.Contains(outgoing, "WHERE target <> e") {
		t.Fatalf("outgoing cypher does not exclude the start node:\n%s", outgoing)
	}
	if !strings.Contains(outgoing, "(e)-[:CALLS*1..4]->(target)") {
		t.Fatalf("outgoing cypher lost its directed traversal:\n%s", outgoing)
	}

	incoming, _ := BuildTransitiveRelationshipRowsCypher("entity-1", "incoming", 4, querycontract.GraphBackendNeo4j)
	if !strings.Contains(incoming, "WHERE source <> e") {
		t.Fatalf("incoming cypher does not exclude the start node:\n%s", incoming)
	}
	// The walk is directed every hop like the BFS one-hop read: callers
	// reach the anchor through directed CALLS, never an undirected tail.
	if !strings.Contains(incoming, "(source)-[:CALLS*1..4]->(e)") {
		t.Fatalf("incoming cypher is not a directed walk into the anchor:\n%s", incoming)
	}
}

// TestRelationshipGraphRowCypherAnchoredUsesCallerMatchClause proves
// RelationshipGraphRowCypherAnchored splices the caller's MATCH clause in
// place of the bare scan and does not fall back to a backward multi-hop
// EXISTS -- the exact shape #6786 defect 2 proved NornicDB v1.3.3 silently
// ignores.
func TestRelationshipGraphRowCypherAnchoredUsesCallerMatchClause(t *testing.T) {
	t.Parallel()

	anchor := "MATCH (anchorRepo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(anchorFile:File)-[:CONTAINS]->(e)"
	cypher := RelationshipGraphRowCypherAnchored(anchor, "e.name = $name")

	if !strings.Contains(cypher, anchor+" WHERE e.name = $name") {
		t.Fatalf("anchored cypher does not open with the caller's MATCH clause and predicate:\n%s", cypher)
	}
	if strings.Contains(cypher, "EXISTS") {
		t.Fatalf("anchored cypher must not fall back to an EXISTS filter:\n%s", cypher)
	}
	// The OPTIONAL MATCH body (file/repo enrichment, outgoing/incoming
	// collection) must still be present and unchanged.
	if !strings.Contains(cypher, "OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)") {
		t.Fatalf("anchored cypher is missing the shared file/repo enrichment OPTIONAL MATCH:\n%s", cypher)
	}
}

// TestBuildTransitiveRelationshipRowsCypherNeo4jSeeksUID pins the Neo4j
// anchor of the transitive CALLS walk to the uid uniqueness constraints of
// the code-call endpoint labels (issue #7057). The old unlabeled
// MATCH (e) WHERE (e.id = x OR e.uid = x) planned as an AllNodesScan on every
// request. Only those six labels carry CALLS edges, so a node outside them
// could never produce a row. The NornicDB branch keeps its own shape: a
// label disjunction matches zero rows on the pinned NornicDB build.
func TestBuildTransitiveRelationshipRowsCypherNeo4jSeeksUID(t *testing.T) {
	t.Parallel()

	const anchor = "MATCH (e:Function|Class|Struct|Interface|TypeAlias|File {uid: $entity_id})"
	for _, direction := range []string{"outgoing", "incoming"} {
		cypher, params := BuildTransitiveRelationshipRowsCypher("entity-1", direction, 4, querycontract.GraphBackendNeo4j)
		if !strings.Contains(cypher, anchor) {
			t.Errorf("%s: Neo4j anchor missing %q:\n%s", direction, anchor, cypher)
		}
		if strings.Contains(cypher, ".id =") || strings.Contains(cypher, "MATCH (e)\n") {
			t.Errorf("%s: Neo4j anchor still scans on the unindexed id predicate:\n%s", direction, cypher)
		}
		if params["entity_id"] != "entity-1" {
			t.Errorf("%s: params[entity_id] = %#v, want entity-1", direction, params["entity_id"])
		}

		nornic, _ := BuildTransitiveRelationshipRowsCypher("entity-1", direction, 4, querycontract.GraphBackendNornicDB)
		if strings.Contains(nornic, anchor) || !strings.Contains(nornic, GraphEntityIDPredicate("e", "$entity_id")) {
			t.Errorf("%s: NornicDB branch must keep its unlabeled anchor:\n%s", direction, nornic)
		}
	}
}
