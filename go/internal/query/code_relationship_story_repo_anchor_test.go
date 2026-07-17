// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"
)

func TestRelationshipStoryRepositoryAnchorFallbacks(t *testing.T) {
	t.Parallel()

	t.Run("unscoped non-function keeps per-type identity fallback", func(t *testing.T) {
		graph := &storyAnchorPropertyGraph{}
		handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: graph}
		_, _, _, err := handler.relationshipStoryRelationships(
			context.Background(),
			relationshipStoryRequest{
				EntityID:          "variable-target",
				Direction:         "incoming",
				RelationshipTypes: []string{"CALLS", "IMPORTS"},
				Limit:             50,
			},
			&EntityContent{EntityID: "variable-target", EntityType: "Variable"},
		)
		if err != nil {
			t.Fatalf("relationshipStoryRelationships() error = %v", err)
		}
		if len(graph.calls) != 0 || len(graph.cyphers) != 0 {
			t.Fatalf("anchor preflight calls/queries = %v/%d, want no broad Variable preflight", graph.calls, len(graph.cyphers))
		}
		if len(graph.runs) != 4 {
			t.Fatalf("relationship reads = %d, want two types with uid/id fallback", len(graph.runs))
		}
	})

	t.Run("repo-scoped legacy id anchor reuses one bounded resolution", func(t *testing.T) {
		graph := &storyAnchorPropertyGraph{idFound: true}
		handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: graph}
		_, _, _, err := handler.relationshipStoryRelationships(
			context.Background(),
			relationshipStoryRequest{
				EntityID:          "legacy-variable",
				RepoID:            "repository-1",
				Direction:         "incoming",
				RelationshipTypes: []string{"CALLS", "IMPORTS"},
				Limit:             50,
			},
			&EntityContent{EntityID: "legacy-variable", EntityType: "Variable"},
		)
		if err != nil {
			t.Fatalf("relationshipStoryRelationships() error = %v", err)
		}
		if got := strings.Join(graph.calls, ","); got != "uid,id" {
			t.Fatalf("anchor preflight calls = %q, want bounded uid then id", got)
		}
		if len(graph.runs) != 2 {
			t.Fatalf("relationship reads = %d, want one id read per type", len(graph.runs))
		}
		for _, cypher := range graph.runs {
			if !strings.Contains(cypher, "(anchor:Variable {id: $entity_id})") || strings.Contains(cypher, "{uid: $entity_id}") {
				t.Fatalf("relationship read did not reuse the resolved legacy id: %s", cypher)
			}
		}
	})
}
