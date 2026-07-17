// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"sync"
	"testing"
)

type storyAnchorPropertyGraph struct {
	uidFound             bool
	idFound              bool
	collision            bool
	uidRelationshipFound bool
	mu                   sync.Mutex
	calls                []string
	cyphers              []string
	runs                 []string
}

func (g *storyAnchorPropertyGraph) Run(
	_ context.Context,
	cypher string,
	_ map[string]any,
) ([]map[string]any, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.runs = append(g.runs, cypher)
	if g.uidRelationshipFound && strings.Contains(cypher, "{uid: $entity_id}") {
		return []map[string]any{{"source_uid": "caller", "target_uid": "target"}}, nil
	}
	return nil, nil
}

func (g *storyAnchorPropertyGraph) RunSingle(
	_ context.Context,
	cypher string,
	_ map[string]any,
) (map[string]any, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cyphers = append(g.cyphers, cypher)
	switch {
	case strings.Contains(cypher, "(idAnchor:"):
		g.calls = append(g.calls, "collision")
		if g.collision {
			return map[string]any{"collision": true}, nil
		}
	case strings.Contains(cypher, "{uid: $entity_id}") || strings.Contains(cypher, "anchor.uid = $entity_id"):
		g.calls = append(g.calls, "uid")
		if g.uidFound {
			return map[string]any{"found": true}, nil
		}
	case strings.Contains(cypher, "{id: $entity_id}") || strings.Contains(cypher, "anchor.id = $entity_id"):
		g.calls = append(g.calls, "id")
		if g.idFound {
			return map[string]any{"found": true}, nil
		}
	}
	return nil, nil
}

func TestRelationshipStoryProductionPathHandlesMissingAndCollisionAnchors(t *testing.T) {
	t.Parallel()

	t.Run("uid anchor resolves once and all types reuse uid", func(t *testing.T) {
		graph := &storyAnchorPropertyGraph{uidFound: true}
		handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: graph}
		rows, _, _, err := handler.relationshipStoryRelationships(
			context.Background(),
			relationshipStoryRequest{
				EntityID:          "function-target",
				Direction:         "both",
				RelationshipTypes: []string{"CALLS", "IMPORTS"},
				Limit:             50,
			},
			&EntityContent{EntityID: "function-target", EntityType: "Function"},
		)
		if err != nil {
			t.Fatalf("relationshipStoryRelationships() error = %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("rows = %v, want empty fake result", rows)
		}
		if got := strings.Join(graph.calls, ","); got != "uid" {
			t.Fatalf("anchor resolution calls = %q, want one canonical uid resolution", got)
		}
		if len(graph.runs) != 4 {
			t.Fatalf("relationship reads = %d, want 2 types x 2 directions", len(graph.runs))
		}
		for _, cypher := range graph.runs {
			if !strings.Contains(cypher, "(anchor:Function {uid: $entity_id})") ||
				strings.Contains(cypher, "(anchor:Function {id: $entity_id})") {
				t.Fatalf("production relationship read did not reuse only uid: %s", cypher)
			}
		}
	})

	t.Run("missing anchor resolves once and skips all relationship reads", func(t *testing.T) {
		graph := &storyAnchorPropertyGraph{}
		handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: graph}
		rows, backend, basis, err := handler.relationshipStoryRelationships(
			context.Background(),
			relationshipStoryRequest{
				EntityID:          "missing-function",
				Direction:         "both",
				RelationshipTypes: []string{"CALLS", "IMPORTS"},
				Limit:             50,
			},
			&EntityContent{EntityID: "missing-function", EntityType: "Function"},
		)
		if err != nil {
			t.Fatalf("relationshipStoryRelationships() error = %v", err)
		}
		if len(rows) != 0 || backend != "graph" || basis != TruthBasisAuthoritativeGraph {
			t.Fatalf("rows/backend/basis = %v/%q/%q, want empty graph result", rows, backend, basis)
		}
		if got := strings.Join(graph.calls, ","); got != "uid,id" {
			t.Fatalf("anchor resolution calls = %q, want one uid/id resolution", got)
		}
		if len(graph.runs) != 0 {
			t.Fatalf("relationship reads = %d, want none for a confirmed missing anchor", len(graph.runs))
		}
	})

	t.Run("canonical uid anchor ignores unrelated legacy id collision", func(t *testing.T) {
		graph := &storyAnchorPropertyGraph{uidFound: true, collision: true}
		handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: graph}
		rows, _, _, err := handler.relationshipStoryRelationships(
			context.Background(),
			relationshipStoryRequest{
				EntityID:          "colliding-function",
				Direction:         "both",
				RelationshipTypes: []string{"CALLS", "IMPORTS"},
				Limit:             50,
			},
			&EntityContent{EntityID: "colliding-function", EntityType: "Function"},
		)
		if err != nil {
			t.Fatalf("relationshipStoryRelationships() error = %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("rows = %v, want empty fake result", rows)
		}
		if got := strings.Join(graph.calls, ","); got != "uid" {
			t.Fatalf("anchor resolution calls = %q, want canonical uid only", got)
		}
		if len(graph.runs) != 4 {
			t.Fatalf("relationship reads = %d, want 2 types x 2 directions", len(graph.runs))
		}
		uidReads, idReads := 0, 0
		for _, cypher := range graph.runs {
			if strings.Contains(cypher, "{uid: $entity_id}") {
				uidReads++
			}
			if strings.Contains(cypher, "{id: $entity_id}") {
				idReads++
			}
		}
		if uidReads != 4 || idReads != 0 {
			t.Fatalf("uid/id reads = %d/%d, want 4/0", uidReads, idReads)
		}
	})
}

func TestRelationshipStoryAnchorPreflightOnlyAmortizesMultiTypeRequests(t *testing.T) {
	t.Parallel()

	t.Run("single type and direction keeps the legacy direct read", func(t *testing.T) {
		graph := &storyAnchorPropertyGraph{
			uidFound:             true,
			collision:            true,
			uidRelationshipFound: true,
		}
		handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: graph}
		rows, _, _, err := handler.relationshipStoryRelationships(
			context.Background(),
			relationshipStoryRequest{
				EntityID:         "function-target",
				Direction:        "incoming",
				RelationshipType: "CALLS",
				Limit:            50,
			},
			&EntityContent{EntityID: "function-target", EntityType: "Function"},
		)
		if err != nil {
			t.Fatalf("relationshipStoryRelationships() error = %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("rows = %d, want one fake uid result", len(rows))
		}
		if len(graph.calls) != 0 {
			t.Fatalf("anchor preflight calls = %v, want none for one direct relationship read", graph.calls)
		}
		if len(graph.runs) != 1 || !strings.Contains(graph.runs[0], "{uid: $entity_id}") {
			t.Fatalf("relationship reads = %v, want one legacy uid-first read", graph.runs)
		}
	})

	t.Run("repo-scoped non-function resolves once through repository ownership", func(t *testing.T) {
		graph := &storyAnchorPropertyGraph{}
		handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: graph}
		_, _, _, err := handler.relationshipStoryRelationships(
			context.Background(),
			relationshipStoryRequest{
				EntityID:          "variable-target",
				RepoID:            "repository-1",
				Direction:         "incoming",
				RelationshipTypes: []string{"CALLS", "IMPORTS"},
				Limit:             50,
			},
			&EntityContent{EntityID: "variable-target", EntityType: "Variable"},
		)
		if err != nil {
			t.Fatalf("relationshipStoryRelationships() error = %v", err)
		}
		if got := strings.Join(graph.calls, ","); got != "uid,id" {
			t.Fatalf("anchor preflight calls = %q, want one bounded uid/id resolution", got)
		}
		if len(graph.runs) != 0 {
			t.Fatalf("relationship reads = %d, want none for a confirmed missing anchor", len(graph.runs))
		}
		if len(graph.cyphers) != 2 {
			t.Fatalf("anchor preflight queries = %d, want uid and id", len(graph.cyphers))
		}
		for _, cypher := range graph.cyphers {
			if !strings.Contains(cypher, "MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(anchor:Variable)") {
				t.Fatalf("anchor preflight is not repository bounded: %s", cypher)
			}
			if !strings.Contains(cypher, "WHERE anchor.") || strings.Contains(cypher, "), (") {
				t.Fatalf("anchor preflight does not keep one bounded ownership path: %s", cypher)
			}
		}
	})

	t.Run("multiple types retain one canonical preflight", func(t *testing.T) {
		graph := &storyAnchorPropertyGraph{uidFound: true}
		handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: graph}
		_, _, _, err := handler.relationshipStoryRelationships(
			context.Background(),
			relationshipStoryRequest{
				EntityID:          "function-target",
				Direction:         "incoming",
				RelationshipTypes: []string{"CALLS", "IMPORTS"},
				Limit:             50,
			},
			&EntityContent{EntityID: "function-target", EntityType: "Function"},
		)
		if err != nil {
			t.Fatalf("relationshipStoryRelationships() error = %v", err)
		}
		if got := strings.Join(graph.calls, ","); got != "uid" {
			t.Fatalf("anchor preflight calls = %q, want one canonical uid resolution", got)
		}
		if len(graph.runs) != 2 {
			t.Fatalf("relationship reads = %d, want one per requested type", len(graph.runs))
		}
	})
}

func TestNornicDBRelationshipStoryResolvedAnchorPropertyControlsGraphReads(t *testing.T) {
	t.Parallel()

	t.Run("missing anchor short-circuits all relationship reads", func(t *testing.T) {
		graph := &storyAnchorPropertyGraph{}
		handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: graph}
		rows, err := handler.nornicDBRelationshipStoryGraphRows(
			context.Background(),
			relationshipStoryRequest{
				EntityID:                    "missing",
				RelationshipType:            "CALLS",
				graphAnchorPropertyResolved: true,
			},
			&EntityContent{EntityID: "missing", EntityType: "Function"},
			"incoming",
		)
		if err != nil {
			t.Fatalf("relationship rows: %v", err)
		}
		if len(rows) != 0 || len(graph.runs) != 0 {
			t.Fatalf("rows = %v, graph reads = %d; want empty without graph reads", rows, len(graph.runs))
		}
	})

	t.Run("selected uid property performs one relationship read", func(t *testing.T) {
		graph := &storyAnchorPropertyGraph{}
		handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: graph}
		_, err := handler.nornicDBRelationshipStoryGraphRows(
			context.Background(),
			relationshipStoryRequest{
				EntityID:                    "function-target",
				RelationshipType:            "CALLS",
				graphAnchorProperty:         "uid",
				graphAnchorPropertyResolved: true,
			},
			&EntityContent{EntityID: "function-target", EntityType: "Function"},
			"incoming",
		)
		if err != nil {
			t.Fatalf("relationship rows: %v", err)
		}
		if len(graph.runs) != 1 || !strings.Contains(graph.runs[0], "{uid: $entity_id}") {
			t.Fatalf("graph reads = %v, want one uid-anchored read", graph.runs)
		}
	})
}

func TestRelationshipStoryTransitivePathKeepsPerHopIdentityFallback(t *testing.T) {
	t.Parallel()

	graph := &storyAnchorPropertyGraph{uidFound: true}
	handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: graph}
	_, _, _, err := handler.relationshipStoryRelationships(
		context.Background(),
		relationshipStoryRequest{
			EntityID:          "function-root",
			RelationshipType:  "CALLS",
			Direction:         "outgoing",
			IncludeTransitive: true,
			MaxDepth:          2,
		},
		&EntityContent{EntityID: "function-root", EntityType: "Function"},
	)
	if err != nil {
		t.Fatalf("relationship story: %v", err)
	}
	if len(graph.calls) != 0 {
		t.Fatalf("anchor resolution calls = %v, want none so every hop keeps uid-to-id fallback", graph.calls)
	}
}

func TestRelationshipStoryRejectsInvalidTypeBeforeGraphAnchorResolution(t *testing.T) {
	t.Parallel()

	graph := &storyAnchorPropertyGraph{uidFound: true}
	handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: graph}
	_, _, _, err := handler.relationshipStoryRelationships(
		context.Background(),
		relationshipStoryRequest{
			EntityID:          "function-target",
			RelationshipTypes: []string{"NOT_A_RELATIONSHIP"},
		},
		&EntityContent{EntityID: "function-target", EntityType: "Function"},
	)
	if err == nil {
		t.Fatal("relationship story error = nil, want invalid relationship type")
	}
	if len(graph.calls) != 0 {
		t.Fatalf("anchor resolution calls = %v, want none before request validation", graph.calls)
	}
}

func TestResolveNornicDBRelationshipStoryAnchorProperty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		graph        *storyAnchorPropertyGraph
		wantResolved bool
		wantProperty string
		wantCalls    []string
	}{
		{
			name:         "uid anchor",
			graph:        &storyAnchorPropertyGraph{uidFound: true},
			wantResolved: true,
			wantProperty: "uid",
			wantCalls:    []string{"uid"},
		},
		{
			name:         "same node has both identity properties",
			graph:        &storyAnchorPropertyGraph{uidFound: true, idFound: true},
			wantResolved: true,
			wantProperty: "uid",
			wantCalls:    []string{"uid"},
		},
		{
			name:         "legacy id-only anchor",
			graph:        &storyAnchorPropertyGraph{idFound: true},
			wantResolved: true,
			wantProperty: "id",
			wantCalls:    []string{"uid", "id"},
		},
		{
			name:         "missing graph anchor",
			graph:        &storyAnchorPropertyGraph{},
			wantResolved: true,
			wantProperty: "",
			wantCalls:    []string{"uid", "id"},
		},
		{
			name:         "separate legacy id collision cannot override canonical uid",
			graph:        &storyAnchorPropertyGraph{uidFound: true, idFound: true, collision: true},
			wantResolved: true,
			wantProperty: "uid",
			wantCalls:    []string{"uid"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: tt.graph}
			req, err := handler.resolveNornicDBRelationshipStoryAnchorProperty(
				context.Background(),
				relationshipStoryRequest{EntityID: "function-target"},
				&EntityContent{EntityID: "function-target", EntityType: "Function"},
			)
			if err != nil {
				t.Fatalf("resolve anchor property: %v", err)
			}
			if req.graphAnchorPropertyResolved != tt.wantResolved {
				t.Fatalf("resolved = %t, want %t", req.graphAnchorPropertyResolved, tt.wantResolved)
			}
			if req.graphAnchorProperty != tt.wantProperty {
				t.Fatalf("property = %q, want %q", req.graphAnchorProperty, tt.wantProperty)
			}
			if strings.Join(tt.graph.calls, ",") != strings.Join(tt.wantCalls, ",") {
				t.Fatalf("calls = %v, want %v", tt.graph.calls, tt.wantCalls)
			}
			for _, cypher := range tt.graph.cyphers {
				if strings.Contains(cypher, "), (") {
					t.Fatalf("anchor property query contains a Cartesian match: %s", cypher)
				}
			}
		})
	}
}
