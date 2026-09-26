// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Hermetic #5167 grant-statement proofs for the Neo4j one-statement
// relationships read and the transitive builder. Clause position is what the
// live Neo4j proof measured; these pin it on every PR run.

func relGrantScoped() querycontract.RepositoryAccessFilter {
	return querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo://tenant-a/granted-service"}}
}

func relGrantCond(alias string) string {
	return "(" + alias + ".repo_id IN $allowed_repository_ids OR " + alias + ".repo_id IN $allowed_scope_ids)"
}

// TestRelationshipGraphRowCypherBindsAnchorAndNeighbours checks the three
// grant clauses of the Neo4j read, for every anchor the handler uses: the
// anchor in a required WITH directly after the anchor clause, and each
// neighbour in the WHERE of the OPTIONAL MATCH that binds it -- all ahead of
// RETURN and LIMIT.
func TestRelationshipGraphRowCypherBindsAnchorAndNeighbours(t *testing.T) {
	t.Parallel()
	scoped := relGrantScoped()
	for name, cypher := range map[string]string{
		"entity_id": RelationshipGraphRowCypherFromAnchor(Neo4jEntityIDAnchor("e", "$entity_id"), scoped),
		"name":      RelationshipGraphRowCypher("e.name = $name", scoped),
		"name_repo": RelationshipGraphRowCypherAnchored("MATCH (anchorRepo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(anchorFile:File)-[:CONTAINS]->(e)", "e.name = $name", scoped),
	} {
		norm := querycontract.NormalizeCypherWhitespace(cypher)
		// The entity-id anchor is a CALL () { ... RETURN e } subquery; the
		// read's own RETURN is the one that projects the row.
		ret, limit := strings.Index(norm, " RETURN coalesce(e.id, e.uid) as id"), strings.Index(norm, " LIMIT ")
		for _, clause := range []string{
			"WITH e WHERE " + relGrantCond("e") + " OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)",
			"OPTIONAL MATCH (e)-[outgoingRel]->(target) WHERE " + relGrantCond("target") + " OPTIONAL MATCH",
			"OPTIONAL MATCH (source)-[incomingRel]->(e) WHERE " + relGrantCond("source") + " OPTIONAL MATCH",
		} {
			at := strings.Index(norm, clause)
			if at < 0 || at > ret || at > limit {
				t.Fatalf("%s: clause %q missing or after RETURN/LIMIT:\n%s", name, clause, norm)
			}
		}
		if unscoped := RelationshipGraphRowCypherFromAnchor(Neo4jEntityIDAnchor("e", "$entity_id"), querycontract.RepositoryAccessFilter{AllScopes: true}); strings.Contains(unscoped, "allowed_") {
			t.Fatalf("unscoped Neo4j read renders grant text:\n%s", unscoped)
		}
	}
}

// TestBuildTransitiveRelationshipRowsCypherGrant: the Neo4j statement binds
// every path node and its arrays; the NornicDB branch fails closed for a scoped
// caller, because all(...) filters nothing on the pinned NornicDB.
func TestBuildTransitiveRelationshipRowsCypherGrant(t *testing.T) {
	t.Parallel()
	for _, direction := range []string{"outgoing", "incoming"} {
		cypher, params := BuildTransitiveRelationshipRowsCypher("e1", direction, 3, querycontract.GraphBackendNeo4j, relGrantScoped())
		if !strings.Contains(cypher, " AND all(node IN nodes(path) WHERE "+relGrantCond("node")+")") || params["allowed_repository_ids"] == nil || params["allowed_scope_ids"] == nil {
			t.Fatalf("%s: Neo4j transitive statement lost its path-wide grant: %v\n%s", direction, params, cypher)
		}
		if cypher, params := BuildTransitiveRelationshipRowsCypher("e1", direction, 3, querycontract.GraphBackendNornicDB, relGrantScoped()); cypher != "" || params != nil {
			t.Fatalf("%s: scoped NornicDB transitive builder must fail closed, got:\n%s", direction, cypher)
		}
		if cypher, _ := BuildTransitiveRelationshipRowsCypher("e1", direction, 3, querycontract.GraphBackendNeo4j, querycontract.RepositoryAccessFilter{AllScopes: true}); strings.Contains(cypher, "allowed_") {
			t.Fatalf("%s: unscoped transitive statement renders grant text:\n%s", direction, cypher)
		}
	}
}
