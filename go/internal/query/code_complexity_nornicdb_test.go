// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"
)

func TestLookupComplexityByNameUsesConnectedAnchorBeforeDegreeReads(t *testing.T) {
	t.Parallel()

	const entityID = "content-entity:e_complexity"
	var calls int
	handler := &CodeHandler{Neo4j: fakeGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			calls++
			switch calls {
			case 1:
				if !strings.Contains(cypher, "MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e)") {
					t.Fatalf("candidate cypher is not a connected repository anchor: %s", cypher)
				}
				if !strings.Contains(cypher, "WHERE e.name = $entity_name AND repo.id = $repo_id") {
					t.Fatalf("candidate cypher is not constrained to the requested function and repository: %s", cypher)
				}
				if got, want := params["repo_id"], "repository:r_fixture"; got != want {
					t.Fatalf("repo_id = %#v, want %#v", got, want)
				}
				if !strings.Contains(cypher, "OPTIONAL MATCH (e)-[outgoingRel]->()") {
					t.Fatalf("candidate cypher omits outgoing degree read: %s", cypher)
				}
				if !strings.Contains(cypher, "OPTIONAL MATCH ()-[incomingRel]->(e)") {
					t.Fatalf("candidate cypher omits incoming degree read: %s", cypher)
				}
				return []map[string]any{{
					"id": entityID, "name": "GoldenDataflowHandler",
					"repo_id": "repository:r_fixture", "complexity": int64(2),
					"outgoing_count": int64(3), "incoming_count": int64(2),
					"total_relationships": int64(5),
				}}, nil
			default:
				t.Fatalf("unexpected graph call %d", calls)
				return nil, nil
			}
		},
	}}

	row, err := handler.lookupComplexityRowByName(
		context.Background(), "GoldenDataflowHandler", "repository:r_fixture",
		repositoryAccessFilter{AllScopes: true},
	)
	if err != nil {
		t.Fatalf("lookupComplexityRowByName() error = %v", err)
	}
	if got, want := IntVal(row, "complexity"), 2; got != want {
		t.Fatalf("complexity = %d, want %d", got, want)
	}
	if got, want := IntVal(row, "outgoing_count"), 3; got != want {
		t.Fatalf("outgoing_count = %d, want %d", got, want)
	}
	if got, want := IntVal(row, "incoming_count"), 2; got != want {
		t.Fatalf("incoming_count = %d, want %d", got, want)
	}
	if got, want := IntVal(row, "total_relationships"), 5; got != want {
		t.Fatalf("total_relationships = %d, want %d", got, want)
	}
}
