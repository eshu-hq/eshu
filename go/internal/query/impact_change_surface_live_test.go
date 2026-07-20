// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestLiveChangeSurfaceImpactTraversal is the backend-required proof for the
// #5287 change-surface fix. It seeds a small impact graph on a live NornicDB,
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
		startID = "cs-live:start"
		r1ID    = "cs-live:r1"
		crID    = "cs-live:cr"
	)
	// Delete only the exact synthetic nodes by id (never a label-wide DETACH
	// DELETE), so pointing ESHU_NEO4J_URI at a retained evidence graph cannot
	// wipe production-shaped nodes. Same targeted cleanup runs on exit.
	cleanup := func() {
		for _, id := range []string{startID, r1ID, crID} {
			write(`MATCH (n {id:$id}) DETACH DELETE n`, map[string]any{"id": id})
		}
	}
	cleanup()
	defer cleanup()
	write(`CREATE (s:Workload {id:$s, name:'start'})
	       CREATE (r1:Repository {id:$r1, name:'r1', environment:'prod', repo_id:$r1})
	       CREATE (cr:CloudResource {id:$cr, name:'cr', environment:'prod'})
	       CREATE (s)-[:DEPENDS_ON {confidence:0.9, reason:'dep'}]->(r1)
	       CREATE (r1)-[:CONTAINS {confidence:0.8, reason:'contains'}]->(cr)`,
		map[string]any{"s": startID, "r1": r1ID, "cr": crID})

	dump := func(label string, rows []map[string]any) {
		b, _ := json.MarshalIndent(rows, "", "  ")
		t.Logf("\n=== %s (%d rows) ===\n%s", label, len(rows), b)
	}
	labelFilter := "['Repository', 'Workload', 'WorkloadInstance', 'CloudResource', 'TerraformModule', 'DataAsset']"

	// OLD investigate shape: 2x MATCH + RETURN DISTINCT + length(path).
	oldInvestigate, _ := reader.Run(ctx, `MATCH (start:Workload {id: $target_id})
MATCH path = (start)-[*1..4]->(impacted)
WHERE impacted.id <> $target_id AND any(label IN labels(impacted) WHERE label IN `+labelFilter+`)
RETURN DISTINCT impacted.id as id, length(path) as depth
ORDER BY depth, impacted.id`, map[string]any{"target_id": startID})
	dump("OLD investigate (2x MATCH + DISTINCT)", oldInvestigate)

	// OLD legacy shape: OPTIONAL MATCH + UNWIND + WITH + RETURN DISTINCT.
	oldLegacy, _ := reader.Run(ctx, `MATCH (start:Workload {id: $target_id})
OPTIONAL MATCH path = (start)-[rels*1..4]->(impacted)
WHERE impacted.id <> $target_id AND any(label IN labels(impacted) WHERE label IN `+labelFilter+`)
UNWIND relationships(path) as rel
WITH impacted, rel, length(path) as depth
RETURN DISTINCT impacted.id as id, type(rel) as rel_type, rel.confidence as confidence, depth
ORDER BY depth, impacted.id`, map[string]any{"target_id": startID})
	dump("OLD legacy (OPTIONAL + UNWIND + WITH + DISTINCT)", oldLegacy)

	target := changeSurfaceTargetCandidate{ID: startID, Name: "start", Labels: []string{"Workload"}}

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
	if len(investigate) != 2 || byID[r1ID] == nil || byID[crID] == nil {
		t.Fatalf("investigate impacted = %#v, want r1 (depth 1) and cr (depth 2)", investigate)
	}
	if got := IntVal(byID[r1ID], "depth"); got != 1 {
		t.Errorf("r1 depth = %d, want 1", got)
	}
	if got := IntVal(byID[crID], "depth"); got != 2 {
		t.Errorf("cr depth = %d, want 2", got)
	}

	// NEW legacy: per-edge provenance unwound in Go.
	legacy, _, err := handler.findChangeSurfaceImpactRows(ctx, target, "", 4, 50, repositoryAccessFilter{allScopes: true})
	if err != nil {
		t.Fatalf("findChangeSurfaceImpactRows() error = %v", err)
	}
	dump("NEW findChangeSurfaceImpactRows (legacy)", legacy)
	var r1Dep, crContains bool
	for _, row := range legacy {
		if StringVal(row, "id") == r1ID && StringVal(row, "rel_type") == "DEPENDS_ON" {
			r1Dep = true
			if got, ok := row["confidence"].(float64); !ok || got != 0.9 {
				t.Errorf("r1 DEPENDS_ON confidence = %v, want 0.9", row["confidence"])
			}
			if got := StringVal(row, "reason"); got != "dep" {
				t.Errorf("r1 DEPENDS_ON reason = %q, want dep", got)
			}
		}
		if StringVal(row, "id") == crID && StringVal(row, "rel_type") == "CONTAINS" {
			crContains = true
			if got := IntVal(row, "depth"); got != 2 {
				t.Errorf("cr CONTAINS depth = %d, want 2", got)
			}
		}
	}
	if !r1Dep {
		t.Errorf("legacy missing r1 DEPENDS_ON provenance (OLD shape returned all-null): %#v", legacy)
	}
	if !crContains {
		t.Errorf("legacy missing cr CONTAINS provenance: %#v", legacy)
	}

	// Environment-scoped read: the server-side environment predicate must filter
	// live alongside the relationships(path) projection (the coalesce/OR form that
	// dropped every row is avoided) and keep only prod/unset-environment impacted.
	// r1 and cr are both environment=prod, so a staging scope returns nothing.
	prodInvestigate, _, err := handler.changeSurfaceImpactRows(ctx, changeSurfaceInvestigationRequest{MaxDepth: 4, Limit: 50, Environment: "prod"}, target)
	if err != nil {
		t.Fatalf("investigate(env=prod) error = %v", err)
	}
	if len(prodInvestigate) != 2 {
		t.Errorf("investigate(env=prod) = %d rows, want 2 (r1, cr both prod)", len(prodInvestigate))
	}
	stagingInvestigate, _, err := handler.changeSurfaceImpactRows(ctx, changeSurfaceInvestigationRequest{MaxDepth: 4, Limit: 50, Environment: "staging"}, target)
	if err != nil {
		t.Fatalf("investigate(env=staging) error = %v", err)
	}
	if len(stagingInvestigate) != 0 {
		t.Errorf("investigate(env=staging) = %d rows, want 0 (no staging impacted)", len(stagingInvestigate))
	}
	prodLegacy, _, err := handler.findChangeSurfaceImpactRows(ctx, target, "prod", 4, 50, repositoryAccessFilter{allScopes: true})
	if err != nil {
		t.Fatalf("legacy(env=prod) error = %v", err)
	}
	if len(prodLegacy) == 0 {
		t.Errorf("legacy(env=prod) returned no rows, want prod provenance (server-side env predicate must not drop all rows)")
	}
	stagingLegacy, _, err := handler.findChangeSurfaceImpactRows(ctx, target, "staging", 4, 50, repositoryAccessFilter{allScopes: true})
	if err != nil {
		t.Fatalf("legacy(env=staging) error = %v", err)
	}
	if len(stagingLegacy) != 0 {
		t.Errorf("legacy(env=staging) = %d rows, want 0", len(stagingLegacy))
	}
}
