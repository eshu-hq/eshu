// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestLiveResourceInvestigationReadsAreNornicDBSafe is the backend-required proof
// for the #5287 resource-investigation reads. On the pinned NornicDB build the
// prior queries corrupt: resourceInvestigationWorkloads (multi-clause
// MATCH+MATCH+OPTIONAL MATCH+WITH) returned all-null columns, and
// resourceInvestigationRepoPaths returned a null depth and a mangled map-valued
// `hops` comprehension. This seeds a resource -> workload-instance -> workload
// chain and a resource -> repository path, drives the shipped handler methods,
// and asserts the corrected values.
//
//	Run: ESHU_INFRA_AGG_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17687 \
//		go test ./internal/query -run TestLiveResourceInvestigationReadsAreNornicDBSafe -count=1 -v
func TestLiveResourceInvestigationReadsAreNornicDBSafe(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_INFRA_AGG_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_INFRA_AGG_PROVE_LIVE=1 to run the live resource-investigation proof")
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

	ids := []string{"ri5287:res", "ri5287:inst", "ri5287:wl", "ri5287:repo"}
	clean := func() {
		for _, id := range ids {
			write(`MATCH (n {id:$id}) DETACH DELETE n`, map[string]any{"id": id})
		}
	}
	clean()
	defer clean()
	write(`CREATE (r:CloudResource {id:'ri5287:res', environment:'prod'})`, nil)
	write(`CREATE (w:Workload {id:'ri5287:wl', name:'orders'})`, nil)
	write(`CREATE (i:WorkloadInstance {id:'ri5287:inst', environment:'prod', workload_id:'ri5287:wl', name:'orders-instance'})`, nil)
	write(`CREATE (repo:Repository {id:'ri5287:repo', name:'orders-api'})`, nil)
	write(`MATCH (i:WorkloadInstance {id:'ri5287:inst'}),(r:CloudResource {id:'ri5287:res'}) CREATE (i)-[:USES {reason:'runtime-use', evidence_type:'observed', confidence:0.91}]->(r)`, nil)
	write(`MATCH (i:WorkloadInstance {id:'ri5287:inst'}),(w:Workload {id:'ri5287:wl'}) CREATE (i)-[:INSTANCE_OF]->(w)`, nil)
	write(`MATCH (r:CloudResource {id:'ri5287:res'}),(repo:Repository {id:'ri5287:repo'}) CREATE (r)-[:BELONGS_TO {reason:'provisioned-by', evidence_type:'iac', confidence:0.77}]->(repo)`, nil)

	handler := &ImpactHandler{Neo4j: NewNeo4jReader(driver, "nornic"), Profile: ProfileLocalAuthoritative}
	req := resourceInvestigationRequest{Environment: "", MaxDepth: 4, Limit: 25}
	// Labels set so the traversal folds in the resolved `:CloudResource` anchor
	// (bounded-scan start) rather than the unlabeled fallback (Codex P1, #5302).
	selected := &resourceInvestigationCandidate{ID: "ri5287:res", Labels: []string{"CloudResource"}}
	if ref := resourceInvestigationResourceRef(selected); ref != "resource:CloudResource" {
		t.Fatalf("resource ref = %q, want resource:CloudResource (labeled anchor)", ref)
	}

	// Workloads read.
	workloads, _, err := handler.resourceInvestigationWorkloads(ctx, req, selected, repositoryAccessFilter{allScopes: true})
	if err != nil {
		t.Fatalf("resourceInvestigationWorkloads: %v", err)
	}
	if len(workloads) != 1 {
		t.Fatalf("workloads = %d, want 1: %#v", len(workloads), workloads)
	}
	wl := workloads[0]
	for key, want := range map[string]any{
		"workload_id":         "ri5287:wl",
		"workload_name":       "orders",
		"instance_id":         "ri5287:inst",
		"environment":         "prod",
		"relationship_type":   "USES",
		"relationship_reason": "runtime-use",
	} {
		if wl[key] != want {
			t.Errorf("workload[%q] = %#v, want %#v", key, wl[key], want)
		}
	}
	if wl["confidence"] != 0.91 {
		t.Errorf("workload confidence = %#v, want 0.91 (multi-clause projection must not null it)", wl["confidence"])
	}

	// Repository-paths read (outgoing) with full hop provenance.
	paths, _, err := handler.resourceInvestigationRepoPaths(ctx, req, selected, "outgoing", repositoryAccessFilter{allScopes: true})
	if err != nil {
		t.Fatalf("resourceInvestigationRepoPaths: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %d, want 1: %#v", len(paths), paths)
	}
	p := paths[0]
	if p["repo_id"] != "ri5287:repo" || p["repo_name"] != "orders-api" {
		t.Errorf("path repo = %#v/%#v, want ri5287:repo/orders-api", p["repo_id"], p["repo_name"])
	}
	if p["depth"] != 1 {
		t.Errorf("path depth = %#v, want 1 (must not collapse to 0)", p["depth"])
	}
	hops, ok := p["hops"].([]map[string]any)
	if !ok || len(hops) != 1 {
		t.Fatalf("path hops = %#v, want 1 decoded hop", p["hops"])
	}
	if hops[0]["type"] != "BELONGS_TO" || hops[0]["confidence"] != 0.77 || hops[0]["reason"] != "provisioned-by" {
		t.Errorf("hop = %#v, want {BELONGS_TO, 0.77, provisioned-by} (map-comprehension must not mangle it)", hops[0])
	}
}
