// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Live positive proof for the #5385 catalog SAME_NAME sibling read
// (CatalogWorkloadSiblingCypher). The differential B-7 corpus covers the
// same read through its own seeded pair (#8540: jenkins-ci-pipelines'
// deployable-config/Dockerfile names the deployable-config unit, pairing
// SAME_NAME with the deployable-config repo's workload); this test seeds
// the pair it needs and asserts the exact production Cypher resolves both
// directions plus a negative control. It runs on both live backends under
// the same tag (the CI
// live-backend job points ESHU_NEO4J_URI at each backend in turn and exports
// ESHU_LIVE_GRAPH_BACKEND with the per-backend ESHU_LIVE_GRAPH_DATABASE pin).
//
// Seed uses literal property values only (see the pinned NornicDB
// literal-values note on repoAnswerTruthSeed). Reads and writes go through
// the shared repositoryLiveReader, whose seed writes retry transient
// conflicts (#6784). Ground truth:
//   - repo A defines workload sib-live-wl-a named sib-live-shared.
//   - repo B defines workload sib-live-wl-b named sib-live-shared.
//   - repo A defines workload sib-live-wl-solo named sib-live-solo.
//   - one SAME_NAME edge wl-a -> wl-b (the read pattern is undirected).
var siblingLiveSeed = []string{
	`CREATE (:Repository {id: 'sib-live-repo-a', name: 'sib-live-a'})`,
	`CREATE (:Repository {id: 'sib-live-repo-b', name: 'sib-live-b'})`,
	`CREATE (:Workload {id: 'sib-live-wl-a', name: 'sib-live-shared'})`,
	`CREATE (:Workload {id: 'sib-live-wl-b', name: 'sib-live-shared'})`,
	`CREATE (:Workload {id: 'sib-live-wl-solo', name: 'sib-live-solo'})`,
	`MATCH (r:Repository {id: 'sib-live-repo-a'}) MATCH (w:Workload {id: 'sib-live-wl-a'}) CREATE (r)-[:DEFINES]->(w)`,
	`MATCH (r:Repository {id: 'sib-live-repo-b'}) MATCH (w:Workload {id: 'sib-live-wl-b'}) CREATE (r)-[:DEFINES]->(w)`,
	`MATCH (r:Repository {id: 'sib-live-repo-a'}) MATCH (w:Workload {id: 'sib-live-wl-solo'}) CREATE (r)-[:DEFINES]->(w)`,
	`MATCH (a:Workload {id: 'sib-live-wl-a'}) MATCH (b:Workload {id: 'sib-live-wl-b'}) CREATE (a)-[:SAME_NAME]->(b)`,
}

const siblingLiveCleanup = `MATCH (n) WHERE n.id STARTS WITH 'sib-live-' DETACH DELETE n`

func TestLiveCatalogSameNameSiblings(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	backend := strings.TrimSpace(strings.ToLower(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")))
	if backend == "" {
		t.Fatal("ESHU_LIVE_GRAPH_BACKEND is required (nornicdb|neo4j)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}

	reader := repositoryLiveReader{driver: driver, database: liveGraphDatabaseName(backend)}
	reader.write(ctx, t, siblingLiveCleanup)
	for _, stmt := range siblingLiveSeed {
		reader.write(ctx, t, stmt)
	}
	defer reader.write(context.Background(), t, siblingLiveCleanup)

	ids := []string{"sib-live-wl-a", "sib-live-wl-b", "sib-live-wl-solo"}
	rows, err := reader.Run(ctx, CatalogWorkloadSiblingCypher, map[string]any{"ids": ids})
	if err != nil {
		t.Fatalf("sibling cypher: %v", err)
	}
	type siblingRow struct{ id, sibling, repo string }
	var got []siblingRow
	for _, row := range rows {
		id, _ := row["id"].(string)
		sib, _ := row["sibling_id"].(string)
		repo, _ := row["sibling_repo_id"].(string)
		got = append(got, siblingRow{id, sib, repo})
	}
	want := []siblingRow{
		{"sib-live-wl-a", "sib-live-wl-b", "sib-live-repo-b"},
		{"sib-live-wl-b", "sib-live-wl-a", "sib-live-repo-a"},
	}
	if len(got) != len(want) {
		t.Fatalf("sibling rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sibling rows = %v, want %v", got, want)
		}
	}

	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	enrichments, err := handler.catalogWorkloadEnrichments(ctx, ids)
	if err != nil {
		t.Fatalf("enrichments: %v", err)
	}
	if enrichments["sib-live-wl-a"].sameNameSiblings != 1 {
		t.Fatalf("wl-a sameNameSiblings = %d, want 1", enrichments["sib-live-wl-a"].sameNameSiblings)
	}
	solo, ok := enrichments["sib-live-wl-solo"]
	if !ok {
		t.Fatalf("no enrichment for the sibling-less workload: %v", enrichments)
	}
	if solo.sameNameSiblings != 0 {
		t.Fatalf("solo sameNameSiblings = %d, want 0", solo.sameNameSiblings)
	}
}
