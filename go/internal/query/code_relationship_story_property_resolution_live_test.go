// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_story_property_proof

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type storyPropertyProofGraph struct {
	GraphQuery
	calls         atomic.Int64
	mu            sync.Mutex
	runCyphers    []string
	singleCyphers []string
}

func (g *storyPropertyProofGraph) Run(
	ctx context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.calls.Add(1)
	g.mu.Lock()
	g.runCyphers = append(g.runCyphers, cypher)
	g.mu.Unlock()
	return g.GraphQuery.Run(ctx, cypher, params)
}

func (g *storyPropertyProofGraph) RunSingle(
	ctx context.Context,
	cypher string,
	params map[string]any,
) (map[string]any, error) {
	g.calls.Add(1)
	g.mu.Lock()
	g.singleCyphers = append(g.singleCyphers, cypher)
	g.mu.Unlock()
	return g.GraphQuery.RunSingle(ctx, cypher, params)
}

func (g *storyPropertyProofGraph) reset() {
	g.calls.Store(0)
	g.mu.Lock()
	g.runCyphers = nil
	g.singleCyphers = nil
	g.mu.Unlock()
}

func (g *storyPropertyProofGraph) queryShapes() (runCyphers []string, singleCyphers []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.runCyphers...), append([]string(nil), g.singleCyphers...)
}

func TestLiveRelationshipStoryResolveAnchorPropertyOnce(t *testing.T) {
	entityID := os.Getenv("ESHU_PROOF_ENTITY_ID")
	if entityID == "" {
		t.Fatal("ESHU_PROOF_ENTITY_ID is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext("bolt://localhost:7687", neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify graph connectivity: %v", err)
	}

	graph := &storyPropertyProofGraph{GraphQuery: NewNeo4jReader(driver, "nornic")}
	handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: graph}
	entity := &EntityContent{EntityID: entityID, EntityType: "Function"}
	types := []string{"CALLS", "IMPORTS", "REFERENCES", "INHERITS", "OVERRIDES", "TAINT_FLOWS_TO"}

	oldStarted := time.Now()
	oldRows := make([]map[string]any, 0)
	for _, relationshipType := range types {
		req := relationshipStoryRequest{
			EntityID:         entityID,
			Direction:        "both",
			RelationshipType: relationshipType,
			Limit:            50,
		}
		rows, err := handler.relationshipStoryGraphRows(ctx, req, entity)
		if err != nil {
			t.Fatalf("old %s rows: %v", relationshipType, err)
		}
		oldRows = append(oldRows, rows...)
	}
	oldDuration := time.Since(oldStarted)
	oldCalls := graph.calls.Load()

	graph.reset()
	newStarted := time.Now()
	newRows, backend, basis, err := handler.relationshipStoryRelationships(
		ctx,
		relationshipStoryRequest{
			EntityID:          entityID,
			Direction:         "both",
			RelationshipTypes: types,
			Limit:             50,
		},
		entity,
	)
	if err != nil {
		t.Fatalf("production relationship story: %v", err)
	}
	if backend != "graph" || basis != TruthBasisAuthoritativeGraph {
		t.Fatalf("production relationship story backend/basis = %q/%q, want graph/%q", backend, basis, TruthBasisAuthoritativeGraph)
	}
	newDuration := time.Since(newStarted)
	newCalls := graph.calls.Load()
	runCyphers, singleCyphers := graph.queryShapes()
	property := assertProductionRelationshipStoryProofShape(t, runCyphers, singleCyphers, len(types))

	if !reflect.DeepEqual(oldRows, newRows) {
		t.Fatalf("old/new ordered rows differ: old=%#v new=%#v", oldRows, newRows)
	}
	t.Logf(
		"old_seconds=%.6f old_calls=%d new_seconds=%.6f new_calls=%d rows=%d property=%s exact_diff=0/0",
		oldDuration.Seconds(),
		oldCalls,
		newDuration.Seconds(),
		newCalls,
		len(newRows),
		property,
	)
}

func TestLiveRelationshipStorySingleTypeFastPath(t *testing.T) {
	entityID := os.Getenv("ESHU_PROOF_ENTITY_ID")
	if entityID == "" {
		t.Fatal("ESHU_PROOF_ENTITY_ID is required")
	}
	emptyType := strings.TrimSpace(os.Getenv("ESHU_PROOF_EMPTY_RELATIONSHIP_TYPE"))
	if emptyType == "" {
		emptyType = "TAINT_FLOWS_TO"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext("bolt://localhost:7687", neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify graph connectivity: %v", err)
	}

	graph := &storyPropertyProofGraph{GraphQuery: NewNeo4jReader(driver, "nornic")}
	handler := &CodeHandler{GraphBackend: GraphBackendNornicDB, Neo4j: graph}
	entity := &EntityContent{EntityID: entityID, EntityType: "Function"}
	for _, relationshipType := range []string{"CALLS", emptyType} {
		t.Run(relationshipType, func(t *testing.T) {
			req := relationshipStoryRequest{
				EntityID:         entityID,
				Direction:        "incoming",
				RelationshipType: relationshipType,
				Limit:            50,
			}
			if _, _, _, err := handler.relationshipStoryRelationshipsForType(ctx, req, entity); err != nil {
				t.Fatalf("warm current-main single-type path: %v", err)
			}
			if _, _, _, err := handler.relationshipStoryRelationships(ctx, req, entity); err != nil {
				t.Fatalf("warm candidate single-type path: %v", err)
			}

			graph.reset()
			oldStarted := time.Now()
			oldRows, oldBackend, oldBasis, err := handler.relationshipStoryRelationshipsForType(ctx, req, entity)
			if err != nil {
				t.Fatalf("current-main single-type path: %v", err)
			}
			oldDuration := time.Since(oldStarted)
			oldCalls := graph.calls.Load()

			graph.reset()
			candidateStarted := time.Now()
			candidateRows, candidateBackend, candidateBasis, err := handler.relationshipStoryRelationships(ctx, req, entity)
			if err != nil {
				t.Fatalf("candidate single-type path: %v", err)
			}
			candidateDuration := time.Since(candidateStarted)
			candidateCalls := graph.calls.Load()
			_, singleCyphers := graph.queryShapes()

			if oldBackend != candidateBackend || oldBasis != candidateBasis {
				t.Fatalf(
					"old/candidate backend-basis = %q/%q and %q/%q, want identical",
					oldBackend,
					oldBasis,
					candidateBackend,
					candidateBasis,
				)
			}
			if !reflect.DeepEqual(oldRows, candidateRows) {
				t.Fatalf("old/candidate ordered rows differ: old=%#v candidate=%#v", oldRows, candidateRows)
			}
			if len(singleCyphers) != 0 {
				t.Fatalf("candidate single-type anchor preflights = %d, want zero: %v", len(singleCyphers), singleCyphers)
			}
			if relationshipType == emptyType && len(candidateRows) != 0 {
				t.Fatalf("empty relationship type %s returned %d rows", emptyType, len(candidateRows))
			}
			t.Logf(
				"type=%s old_seconds=%.6f old_calls=%d candidate_seconds=%.6f candidate_calls=%d rows=%d exact_ordered_diff=0/0 duplicates=%d preflights=0",
				relationshipType,
				oldDuration.Seconds(),
				oldCalls,
				candidateDuration.Seconds(),
				candidateCalls,
				len(candidateRows),
				duplicateRelationshipStoryProofRows(candidateRows),
			)
		})
	}
}

func duplicateRelationshipStoryProofRows(rows []map[string]any) int {
	seen := make(map[string]struct{}, len(rows))
	duplicates := 0
	for _, row := range rows {
		encoded, err := json.Marshal(row)
		if err != nil {
			continue
		}
		key := string(encoded)
		if _, ok := seen[key]; ok {
			duplicates++
			continue
		}
		seen[key] = struct{}{}
	}
	return duplicates
}

func assertProductionRelationshipStoryProofShape(
	t *testing.T,
	runCyphers []string,
	singleCyphers []string,
	typeCount int,
) string {
	t.Helper()
	if len(runCyphers) != typeCount*2 {
		t.Fatalf("relationship graph queries = %d, want %d fixed-property direction queries", len(runCyphers), typeCount*2)
	}
	property := ""
	for _, candidate := range []string{"uid", "id"} {
		anchor := "(anchor:Function {" + candidate + ": $entity_id})"
		allMatch := true
		for _, cypher := range runCyphers {
			if !strings.Contains(cypher, anchor) {
				allMatch = false
				break
			}
		}
		if allMatch {
			property = candidate
			break
		}
	}
	if property == "" {
		t.Fatalf("production relationship reads did not reuse one fixed uid/id anchor property")
	}
	wantResolutionQueries := 1
	if property == "id" {
		wantResolutionQueries = 2
	}
	if len(singleCyphers) != wantResolutionQueries {
		t.Fatalf(
			"anchor resolution queries = %d, want %d for %s anchor: %v",
			len(singleCyphers),
			wantResolutionQueries,
			property,
			singleCyphers,
		)
	}
	wantAnchor := "(anchor:Function {" + property + ": $entity_id})"
	otherProperty := "uid"
	if property == "uid" {
		otherProperty = "id"
	}
	otherAnchor := "(anchor:Function {" + otherProperty + ": $entity_id})"
	for _, cypher := range runCyphers {
		if !strings.Contains(cypher, wantAnchor) || strings.Contains(cypher, otherAnchor) {
			t.Fatalf("relationship query did not reuse only the resolved %s property: %s", property, cypher)
		}
	}
	return property
}
