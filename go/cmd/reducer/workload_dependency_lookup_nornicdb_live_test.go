// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/query"
)

const workloadDependencyLookupLiveEnv = "ESHU_WORKLOAD_DEPENDENCY_LOOKUP_LIVE"

// TestWorkloadDependencyLookupReturnsIncomingOnlyEdgeLive proves the production
// lookup returns an incoming edge when its outgoing branch has no matches.
func TestWorkloadDependencyLookupReturnsIncomingOnlyEdgeLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv(workloadDependencyLookupLiveEnv)) == "" {
		t.Skipf("set %s=1 and ESHU_NEO4J_URI to run against an isolated NornicDB", workloadDependencyLookupLiveEnv)
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })

	probe := fmt.Sprintf("workload-dependency-lookup-%d", time.Now().UnixNano())
	sourceID := probe + "-source"
	targetID := probe + "-target"
	seedWorkloadDependencyLookupEdge(t, ctx, driver, sourceID, targetID)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupWorkloadDependencyLookupEdge(t, cleanupCtx, driver, sourceID, targetID)
	})

	reader := query.NewNeo4jReader(driver, "nornic")
	directRows, err := reader.Run(ctx, `
		MATCH (source:Repository)-[:DEPENDS_ON]->(target:Repository {id: $target_id})
		RETURN source.id AS source_repo_id, target.id AS target_repo_id
	`, map[string]any{"target_id": targetID})
	if err != nil {
		t.Fatalf("read direct incoming positive control: %v", err)
	}
	if got, want := len(directRows), 1; got != want {
		t.Fatalf("direct incoming positive control rows = %d, want %d", got, want)
	}

	lookup := neo4jWorkloadDependencyLookup{reader: reader}
	for _, tc := range []struct {
		name     string
		repoIDs  []string
		wantRows int
	}{
		{name: "incoming only", repoIDs: []string{targetID}, wantRows: 1},
		{name: "outgoing only", repoIDs: []string{sourceID}, wantRows: 1},
		{name: "both endpoints deduplicate", repoIDs: []string{sourceID, targetID}, wantRows: 1},
		{name: "unknown repository", repoIDs: []string{probe + "-unknown"}, wantRows: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := lookup.ListRepoDependencyEdges(ctx, tc.repoIDs)
			if err != nil {
				t.Fatalf("ListRepoDependencyEdges() error = %v", err)
			}
			if got := len(rows); got != tc.wantRows {
				t.Fatalf("ListRepoDependencyEdges() rows = %d, want %d", got, tc.wantRows)
			}
			if tc.wantRows == 0 {
				return
			}
			if got, want := rows[0].SourceRepoID, sourceID; got != want {
				t.Fatalf("SourceRepoID = %q, want %q", got, want)
			}
			if got, want := rows[0].TargetRepoID, targetID; got != want {
				t.Fatalf("TargetRepoID = %q, want %q", got, want)
			}
		})
	}
}

// TestWorkloadDependencyLookupReadsExistingWorkloadEdgesLive proves that the
// production retract lookup returns evaluated repository IDs from the graph.
func TestWorkloadDependencyLookupReadsExistingWorkloadEdgesLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv(workloadDependencyLookupLiveEnv)) == "" {
		t.Skipf("set %s=1 and ESHU_NEO4J_URI to run against an isolated NornicDB", workloadDependencyLookupLiveEnv)
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })

	probe := fmt.Sprintf("workload-dependency-edges-%d", time.Now().UnixNano())
	sourceID, targetID := probe+"-source", probe+"-target"
	sourceRepoID, targetRepoID := probe+"-repo-a", probe+"-repo-b"
	evidenceSource := "finalization/workloads"
	runWorkloadDependencyLookupCypher(t, ctx, driver, `
		MERGE (source:Workload {id: $source_id})
		SET source.repo_id = $source_repo_id
		MERGE (target:Workload {id: $target_id})
		SET target.repo_id = $target_repo_id
		MERGE (source)-[rel:DEPENDS_ON {evidence_source: $evidence_source}]->(target)
	`, map[string]any{
		"source_id": sourceID, "target_id": targetID,
		"source_repo_id": sourceRepoID, "target_repo_id": targetRepoID,
		"evidence_source": evidenceSource,
	})
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		runWorkloadDependencyLookupCypher(t, cleanupCtx, driver,
			`MATCH (workload:Workload) WHERE workload.id IN $ids DETACH DELETE workload`,
			map[string]any{"ids": []string{sourceID, targetID}})
	})

	reader := query.NewNeo4jReader(driver, "nornic")
	directRows, err := reader.Run(ctx, `
		MATCH (source:Workload {id: $source_id})-[rel:DEPENDS_ON]->(target:Workload {id: $target_id})
		RETURN rel
	`, map[string]any{"source_id": sourceID, "target_id": targetID})
	if err != nil || len(directRows) != 1 {
		t.Fatalf("direct edge positive control: rows = %d, error = %v; want one edge", len(directRows), err)
	}

	lookup := neo4jWorkloadDependencyLookup{reader: reader}
	for _, tc := range []struct {
		name           string
		repoIDs        []string
		evidenceSource string
		wantRepoID     string
	}{
		{name: "outgoing edge", repoIDs: []string{sourceRepoID}, evidenceSource: evidenceSource, wantRepoID: sourceRepoID},
		{name: "incoming only", repoIDs: []string{targetRepoID}, evidenceSource: evidenceSource},
		{name: "wrong evidence", repoIDs: []string{sourceRepoID}, evidenceSource: probe + "-other"},
		{name: "unknown repository", repoIDs: []string{probe + "-unknown"}, evidenceSource: evidenceSource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edges, err := lookup.ListWorkloadDependencyEdges(ctx, tc.repoIDs, tc.evidenceSource)
			if err != nil {
				t.Fatalf("ListWorkloadDependencyEdges() error = %v", err)
			}
			if tc.wantRepoID == "" {
				if len(edges) != 0 {
					t.Fatalf("ListWorkloadDependencyEdges() = %#v, want no edges", edges)
				}
				return
			}
			if len(edges) != 1 || edges[0].RepoID != tc.wantRepoID {
				t.Fatalf("ListWorkloadDependencyEdges() = %#v, want one edge for %q", edges, tc.wantRepoID)
			}
		})
	}
}

func seedWorkloadDependencyLookupEdge(
	t *testing.T,
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	sourceID string,
	targetID string,
) {
	t.Helper()
	runWorkloadDependencyLookupCypher(t, ctx, driver, `
		MERGE (source:Repository {id: $source_id})
		MERGE (target:Repository {id: $target_id})
		MERGE (source)-[:DEPENDS_ON]->(target)
	`, map[string]any{"source_id": sourceID, "target_id": targetID})
}

func cleanupWorkloadDependencyLookupEdge(
	t *testing.T,
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	ids ...string,
) {
	t.Helper()
	runWorkloadDependencyLookupCypher(t, ctx, driver,
		`MATCH (repo:Repository) WHERE repo.id IN $ids DETACH DELETE repo`,
		map[string]any{"ids": ids})
}

func runWorkloadDependencyLookupCypher(
	t *testing.T,
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	cypher string,
	params map[string]any,
) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: "nornic",
	})
	defer func() { _ = session.Close(context.Background()) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("run live workload dependency setup: %v", err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume live workload dependency setup: %v", err)
	}
}
