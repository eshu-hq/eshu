// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_story_property_proof

package query

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func TestLiveRelationshipStoryUIDFirstTheory(t *testing.T) {
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
	request := relationshipStoryRequest{
		EntityID:  entityID,
		Direction: "both",
		RelationshipTypes: []string{
			"CALLS",
			"IMPORTS",
			"REFERENCES",
			"INHERITS",
			"OVERRIDES",
			"TAINT_FLOWS_TO",
		},
		Limit: 50,
	}

	candidateStarted := time.Now()
	candidateRows, candidateBackend, candidateBasis, err := handler.relationshipStoryRelationships(
		ctx,
		request,
		entity,
	)
	if err != nil {
		t.Fatalf("uid-first theory story: %v", err)
	}
	candidateDuration := time.Since(candidateStarted)
	candidateCalls := graph.calls.Load()

	graph.reset()
	legacyStarted := time.Now()
	legacyRequest, err := resolveNornicDBRelationshipStoryLegacyCollisionCheck(
		ctx,
		handler,
		request,
		entity,
	)
	if err != nil {
		t.Fatalf("resolve legacy collision-check baseline: %v", err)
	}
	legacyRows, legacyBackend, legacyBasis, err := handler.relationshipStoryRelationships(
		ctx,
		legacyRequest,
		entity,
	)
	if err != nil {
		t.Fatalf("legacy collision-check baseline story: %v", err)
	}
	legacyDuration := time.Since(legacyStarted)
	legacyCalls := graph.calls.Load()
	if candidateBackend != legacyBackend || candidateBasis != legacyBasis {
		t.Fatalf(
			"candidate/legacy backend-basis = %q/%q and %q/%q",
			candidateBackend,
			candidateBasis,
			legacyBackend,
			legacyBasis,
		)
	}
	if !reflect.DeepEqual(candidateRows, legacyRows) {
		t.Fatalf("uid-first/legacy ordered rows differ: candidate=%#v legacy=%#v", candidateRows, legacyRows)
	}
	t.Logf(
		"candidate_seconds=%.6f candidate_calls=%d legacy_seconds=%.6f legacy_calls=%d rows=%d exact_ordered_diff=0/0",
		candidateDuration.Seconds(),
		candidateCalls,
		legacyDuration.Seconds(),
		legacyCalls,
		len(candidateRows),
	)
}

func resolveNornicDBRelationshipStoryLegacyCollisionCheck(
	ctx context.Context,
	handler *CodeHandler,
	request relationshipStoryRequest,
	entity *EntityContent,
) (relationshipStoryRequest, error) {
	params := map[string]any{"entity_id": entity.EntityID}
	uidRow, err := handler.Neo4j.RunSingle(
		ctx,
		"MATCH (anchor:Function {uid: $entity_id}) RETURN true AS found LIMIT 1",
		params,
	)
	if err != nil {
		return request, err
	}
	if len(uidRow) > 0 {
		collisionRow, collisionErr := handler.Neo4j.RunSingle(
			ctx,
			"MATCH (idAnchor:Function {id: $entity_id}) "+
				"WHERE coalesce(idAnchor.uid, '') <> $entity_id "+
				"RETURN true AS collision LIMIT 1",
			params,
		)
		if collisionErr != nil {
			return request, collisionErr
		}
		if len(collisionRow) > 0 {
			return request, nil
		}
		request.graphAnchorPropertyResolved = true
		request.graphAnchorProperty = "uid"
		return request, nil
	}
	idRow, err := handler.Neo4j.RunSingle(
		ctx,
		"MATCH (anchor:Function {id: $entity_id}) RETURN true AS found LIMIT 1",
		params,
	)
	if err != nil {
		return request, err
	}
	request.graphAnchorPropertyResolved = true
	if len(idRow) > 0 {
		request.graphAnchorProperty = "id"
	}
	return request, nil
}
