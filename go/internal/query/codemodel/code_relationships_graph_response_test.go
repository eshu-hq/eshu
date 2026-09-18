// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"strings"
	"testing"
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
