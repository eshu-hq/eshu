// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

func TestNornicDBRelationshipStoryIncomingSeedsIndexedTarget(t *testing.T) {
	t.Parallel()

	for _, property := range []string{"uid", "id"} {
		cypher, _ := nornicDBRelationshipStoryGraphCypher(
			relationshipStoryRequest{
				EntityID:         "function-target",
				RelationshipType: "CALLS",
				Limit:            50,
			},
			"function-target",
			"Function",
			property,
			"incoming",
			repositoryAccessFilter{allScopes: true},
		)

		want := "MATCH (anchor:Function {" + property + ": $entity_id})<-[rel:CALLS]-(source)"
		if !strings.Contains(cypher, want) {
			t.Fatalf("incoming cypher missing indexed target-first match %q:\n%s", want, cypher)
		}
		if strings.Contains(cypher, "MATCH (source)-[rel:CALLS]->") {
			t.Fatalf("incoming cypher retains source-first traversal:\n%s", cypher)
		}
		if !strings.Contains(cypher, "'CALLS' as type") {
			t.Fatalf("incoming cypher does not project the validated static relationship type:\n%s", cypher)
		}
		if strings.Contains(cypher, "type(rel) as type") {
			t.Fatalf("incoming cypher relies on unsupported NornicDB type(rel) projection:\n%s", cypher)
		}
	}
}
