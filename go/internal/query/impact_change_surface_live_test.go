// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestLiveChangeSurfaceImpactTraversal is the backend-required proof for the
// #5287 change-surface fix and the consumer-direction contract. It seeds a
// small impact graph on a live NornicDB,
// captures the OLD multi-clause shapes (which corrupt on the pinned build) for
// evidence, and asserts that the shipped single-clause changeSurfaceImpactRows
// (investigate) and findChangeSurfaceImpactRows (legacy) return the correct
// impacted nodes and per-edge provenance.
//
//	Run: ESHU_OCI_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17687 \
//		go test ./internal/query -run TestLiveChangeSurfaceImpactTraversal -count=1 -v
func TestLiveChangeSurfaceImpactTraversal(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_OCI_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_OCI_PROVE_LIVE=1 to run the live change-surface proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required (e.g. bolt://localhost:17687)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	write := func(cypher string, params map[string]any) {
		s := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: "nornic"})
		defer func() { _ = s.Close(ctx) }()
		if _, err := s.Run(ctx, cypher, params); err != nil {
			t.Fatalf("seed write failed: %v\ncypher=%s", err, cypher)
		}
	}
	reader := NewNeo4jReader(driver, "nornic")
	handler := &ImpactHandler{Neo4j: reader}

	const (
		changedID  = "cs-live:changed"
		consumerID = "cs-live:consumer"
		workloadID = "cs-live:workload"
		crID       = "cs-live:cr"
	)
	proofIDs := []string{changedID, consumerID, workloadID, crID}
	deniedWorkloadIDs := make([]string, 0, 11)
	deniedConsumerIDs := make([]string, 0, 11)
	for i := range 11 {
		deniedWorkloadIDs = append(deniedWorkloadIDs, fmt.Sprintf("cs-live:denied-workload:%02d", i))
		deniedConsumerIDs = append(deniedConsumerIDs, fmt.Sprintf("cs-live:denied-consumer:%02d", i))
	}
	proofIDs = append(proofIDs, deniedWorkloadIDs...)
	proofIDs = append(proofIDs, deniedConsumerIDs...)
	// Delete only the exact synthetic nodes by id (never a label-wide DETACH
	// DELETE), so pointing ESHU_NEO4J_URI at a retained evidence graph cannot
	// wipe production-shaped nodes. Same targeted cleanup runs on exit.
	cleanup := func() {
		for _, id := range proofIDs {
			write(`MATCH (n {id:$id}) DETACH DELETE n`, map[string]any{"id": id})
		}
	}
	cleanup()
	defer cleanup()
	write(`CREATE (changed:Repository {id:$changed, name:'changed', environment:'prod'})
	       CREATE (consumer:Repository {id:$consumer, name:'consumer', environment:'prod'})
	       CREATE (workload:Workload {id:$workload, name:'workload', environment:'prod', repo_id:$changed})
	       CREATE (cr:CloudResource {id:$cr, name:'cr', environment:'prod', repo_id:$changed})
	       CREATE (consumer)-[:DEPENDS_ON {confidence:0.9, reason:'consumer_dependency'}]->(changed)
	       CREATE (changed)-[:DEFINES {confidence:0.95, reason:'defines_workload'}]->(workload)
		CREATE (workload)-[:CONTAINS {confidence:0.8, reason:'contains'}]->(cr)`,
		map[string]any{"changed": changedID, "consumer": consumerID, "workload": workloadID, "cr": crID})

	dump := func(label string, rows []map[string]any) {
		b, _ := json.MarshalIndent(rows, "", "  ")
		t.Logf("\n=== %s (%d rows) ===\n%s", label, len(rows), b)
	}
	labelFilter := "['Repository', 'Workload', 'WorkloadInstance', 'CloudResource', 'TerraformModule', 'DataAsset']"

	// OLD investigate shape: 2x MATCH + RETURN DISTINCT + length(path).
	oldInvestigate, _ := reader.Run(ctx, `MATCH (start:Repository {id: $target_id})
MATCH path = (start)-[*1..4]->(impacted)
WHERE impacted.id <> $target_id AND any(label IN labels(impacted) WHERE label IN `+labelFilter+`)
RETURN DISTINCT impacted.id as id, length(path) as depth
	ORDER BY depth, impacted.id`, map[string]any{"target_id": changedID})
	dump("OLD investigate (2x MATCH + DISTINCT)", oldInvestigate)

	// OLD legacy shape: OPTIONAL MATCH + UNWIND + WITH + RETURN DISTINCT.
	oldLegacy, _ := reader.Run(ctx, `MATCH (start:Repository {id: $target_id})
OPTIONAL MATCH path = (start)-[rels*1..4]->(impacted)
WHERE impacted.id <> $target_id AND any(label IN labels(impacted) WHERE label IN `+labelFilter+`)
UNWIND relationships(path) as rel
WITH impacted, rel, length(path) as depth
RETURN DISTINCT impacted.id as id, type(rel) as rel_type, rel.confidence as confidence, depth
	ORDER BY depth, impacted.id`, map[string]any{"target_id": changedID})
	dump("OLD legacy (OPTIONAL + UNWIND + WITH + DISTINCT)", oldLegacy)

	target := changeSurfaceTargetCandidate{ID: changedID, Name: "changed", Labels: []string{"Repository"}}

	// NEW investigate: distinct impacted nodes at their minimum depth.
	investigate, _, err := handler.changeSurfaceImpactRows(ctx, changeSurfaceInvestigationRequest{MaxDepth: 4, Limit: 50}, target)
	if err != nil {
		t.Fatalf("changeSurfaceImpactRows() error = %v", err)
	}
	dump("NEW changeSurfaceImpactRows (investigate)", investigate)
	byID := map[string]map[string]any{}
	for _, row := range investigate {
		byID[StringVal(row, "id")] = row
	}
	if len(investigate) != 3 || byID[consumerID] == nil || byID[workloadID] == nil || byID[crID] == nil {
		t.Fatalf("investigate impacted = %#v, want consumer and workload (depth 1) plus cr (depth 2)", investigate)
	}
	if got := IntVal(byID[consumerID], "depth"); got != 1 {
		t.Errorf("consumer depth = %d, want 1", got)
	}
	if got := IntVal(byID[workloadID], "depth"); got != 1 {
		t.Errorf("workload depth = %d, want 1", got)
	}
	if got := IntVal(byID[crID], "depth"); got != 2 {
		t.Errorf("cr depth = %d, want 2", got)
	}

	// NEW legacy: per-edge provenance unwound in Go.
	legacy, _, err := handler.findChangeSurfaceImpactRows(ctx, target, "", 4, 50, repositoryAccessFilter{AllScopes: true})
	if err != nil {
		t.Fatalf("findChangeSurfaceImpactRows() error = %v", err)
	}
	dump("NEW findChangeSurfaceImpactRows (legacy)", legacy)
	var consumerDependency, workloadDefines, crContains bool
	for _, row := range legacy {
		if StringVal(row, "id") == consumerID && StringVal(row, "rel_type") == "DEPENDS_ON" {
			consumerDependency = true
			if got, ok := row["confidence"].(float64); !ok || got != 0.9 {
				t.Errorf("consumer DEPENDS_ON confidence = %v, want 0.9", row["confidence"])
			}
			if got := StringVal(row, "reason"); got != "consumer_dependency" {
				t.Errorf("consumer DEPENDS_ON reason = %q, want consumer_dependency", got)
			}
		}
		if StringVal(row, "id") == workloadID && StringVal(row, "rel_type") == "DEFINES" {
			workloadDefines = true
		}
		if StringVal(row, "id") == crID && StringVal(row, "rel_type") == "CONTAINS" {
			crContains = true
			if got := IntVal(row, "depth"); got != 2 {
				t.Errorf("cr CONTAINS depth = %d, want 2", got)
			}
		}
	}
	if !consumerDependency {
		t.Errorf("legacy missing incoming consumer DEPENDS_ON provenance: %#v", legacy)
	}
	if !workloadDefines {
		t.Errorf("legacy missing outgoing workload DEFINES provenance: %#v", legacy)
	}
	if !crContains {
		t.Errorf("legacy missing cr CONTAINS provenance: %#v", legacy)
	}

	// Environment-scoped read: the server-side environment predicate must filter
	// live alongside the relationships(path) projection (the coalesce/OR form that
	// dropped every row is avoided) and keep only prod/unset-environment impacted.
	// Every impacted node is environment=prod, so a staging scope returns nothing.
	prodInvestigate, _, err := handler.changeSurfaceImpactRows(ctx, changeSurfaceInvestigationRequest{MaxDepth: 4, Limit: 50, Environment: "prod"}, target)
	if err != nil {
		t.Fatalf("investigate(env=prod) error = %v", err)
	}
	if len(prodInvestigate) != 3 {
		t.Errorf("investigate(env=prod) = %d rows, want 3", len(prodInvestigate))
	}
	stagingInvestigate, _, err := handler.changeSurfaceImpactRows(ctx, changeSurfaceInvestigationRequest{MaxDepth: 4, Limit: 50, Environment: "staging"}, target)
	if err != nil {
		t.Fatalf("investigate(env=staging) error = %v", err)
	}
	if len(stagingInvestigate) != 0 {
		t.Errorf("investigate(env=staging) = %d rows, want 0 (no staging impacted)", len(stagingInvestigate))
	}
	prodLegacy, _, err := handler.findChangeSurfaceImpactRows(ctx, target, "prod", 4, 50, repositoryAccessFilter{AllScopes: true})
	if err != nil {
		t.Fatalf("legacy(env=prod) error = %v", err)
	}
	if len(prodLegacy) == 0 {
		t.Errorf("legacy(env=prod) returned no rows, want prod provenance (server-side env predicate must not drop all rows)")
	}
	stagingLegacy, _, err := handler.findChangeSurfaceImpactRows(ctx, target, "staging", 4, 50, repositoryAccessFilter{AllScopes: true})
	if err != nil {
		t.Fatalf("legacy(env=staging) error = %v", err)
	}
	if len(stagingLegacy) != 0 {
		t.Errorf("legacy(env=staging) = %d rows, want 0", len(stagingLegacy))
	}

	for i := range 11 {
		write(`MATCH (changed:Repository {id:$changed})
		       CREATE (workload:Workload {id:$workload, name:$workload_name, environment:'prod', repo_id:$denied_repo})
		       CREATE (consumer:Repository {id:$consumer, name:$consumer_name, environment:'prod'})
		       CREATE (changed)-[:DEFINES {confidence:0.5, reason:'denied_workload'}]->(workload)
		       CREATE (consumer)-[:DEPENDS_ON {confidence:0.5, reason:'denied_consumer'}]->(changed)`,
			map[string]any{
				"changed":       changedID,
				"workload":      deniedWorkloadIDs[i],
				"workload_name": fmt.Sprintf("a-denied-workload-%02d", i),
				"consumer":      deniedConsumerIDs[i],
				"consumer_name": fmt.Sprintf("a-denied-consumer-%02d", i),
				"denied_repo":   "repository:denied",
			})
	}

	// Scoped proof: eleven denied rows sort before every granted row in each
	// traversal direction. Grant predicates must run before the per-branch
	// LIMIT, otherwise the authorized consumer/workload rows are starved and
	// denied cardinality incorrectly sets truncated=true.
	scopedAuth := AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{consumerID},
		AllowedScopeIDs:      []string{changedID},
	}
	scopedCtx := ContextWithAuthContext(ctx, scopedAuth)
	scopedAccess := repositoryAccessFilterFromContext(scopedCtx)
	scopedOutgoing, err := handler.runChangeSurfaceOutgoing(
		scopedCtx,
		"(start:Repository {id: $target_id})",
		"",
		4,
		10,
		map[string]any{"target_id": changedID},
		scopedAccess,
	)
	if err != nil {
		t.Fatalf("scoped outgoing diagnostic error = %v", err)
	}
	dump("SCOPED outgoing raw", scopedOutgoing)
	scopedConsumers, err := handler.runChangeSurfaceRepositoryConsumers(
		scopedCtx,
		"",
		4,
		10,
		map[string]any{"target_id": changedID},
		scopedAccess,
	)
	if err != nil {
		t.Fatalf("scoped consumers diagnostic error = %v", err)
	}
	dump("SCOPED consumers raw", scopedConsumers)
	scopedInvestigate, scopedTruncated, err := handler.changeSurfaceImpactRows(
		scopedCtx,
		changeSurfaceInvestigationRequest{MaxDepth: 4, Limit: 10},
		target,
	)
	if err != nil {
		t.Fatalf("scoped investigate error = %v", err)
	}
	if scopedTruncated {
		t.Fatal("scoped investigate truncated = true, want false")
	}
	if got, want := changeSurfaceTestIDs(scopedInvestigate), []string{consumerID, workloadID, crID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped investigate ids = %#v, want %#v", got, want)
	}

	scopedLegacy, scopedLegacyTruncated, err := handler.findChangeSurfaceImpactRows(
		scopedCtx,
		target,
		"",
		4,
		10,
		scopedAccess,
	)
	if err != nil {
		t.Fatalf("scoped legacy error = %v", err)
	}
	if scopedLegacyTruncated {
		t.Fatal("scoped legacy truncated = true, want false")
	}
	if len(scopedLegacy) == 0 {
		t.Fatal("scoped legacy returned no granted provenance")
	}
	for _, row := range scopedLegacy {
		if strings.HasPrefix(StringVal(row, "id"), "cs-live:denied-") {
			t.Fatalf("scoped legacy leaked denied row: %#v", row)
		}
	}
}
