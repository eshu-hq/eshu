// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live proof for #6785: the affected-repo gate statements answer on the
// pinned NornicDB image against a seeded repo chain.
//
// Run against an isolated container on the pinned image:
//
//	docker run -d --name eshu-answer-truth -e NORNICDB_NO_AUTH=true \
//	  -e NORNICDB_EMBEDDING_ENABLED=false -p 127.0.0.1:27687:7687 \
//	  ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27687 go test ./internal/reducer/code/value/affected \
//	  -tags live_nornicdb_answer_truth -run TestLiveAffectedGate -count=1 -v
package affected

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Ground truth, by construction:
//   - repo-cloud defines wl-cloud, whose handler fn invokes a cloud action:
//     counted by all three statements (repo, workload, and principal keys).
//   - repo-plain defines wl-plain, whose handler fn invokes nothing: never
//     counted.
var affectedLiveSeed = []string{
	`CREATE (:Repository {id: 'answer-truth-affected:repo-cloud'})`,
	`CREATE (:Repository {id: 'answer-truth-affected:repo-plain'})`,
	`CREATE (:Workload {id: 'answer-truth-affected:wl-cloud'})`,
	`CREATE (:Workload {id: 'answer-truth-affected:wl-plain'})`,
	`CREATE (:Function {uid: 'answer-truth-affected:fn-cloud', name: 'Cloud'})`,
	`CREATE (:Function {uid: 'answer-truth-affected:fn-plain', name: 'Plain'})`,
	`CREATE (:CloudAction {action: 'answer-truth-affected:s3:GetObject'})`,
	`CREATE (:WorkloadInstance {id: 'answer-truth-affected:vi-cloud'})`,
	`CREATE (:CloudResource {uid: 'answer-truth-affected:vp-role'})`,
	`MATCH (r:Repository {id: 'answer-truth-affected:repo-cloud'}) MATCH (w:Workload {id: 'answer-truth-affected:wl-cloud'}) CREATE (r)-[:DEFINES]->(w)`,
	`MATCH (r:Repository {id: 'answer-truth-affected:repo-plain'}) MATCH (w:Workload {id: 'answer-truth-affected:wl-plain'}) CREATE (r)-[:DEFINES]->(w)`,
	`MATCH (f:Function {uid: 'answer-truth-affected:fn-cloud'}) MATCH (w:Workload {id: 'answer-truth-affected:wl-cloud'}) CREATE (f)-[:RUNS_IN]->(w)`,
	`MATCH (f:Function {uid: 'answer-truth-affected:fn-plain'}) MATCH (w:Workload {id: 'answer-truth-affected:wl-plain'}) CREATE (f)-[:RUNS_IN]->(w)`,
	`MATCH (f:Function {uid: 'answer-truth-affected:fn-cloud'}) MATCH (a:CloudAction {action: 'answer-truth-affected:s3:GetObject'}) CREATE (f)-[:INVOKES_CLOUD_ACTION]->(a)`,
	`MATCH (i:WorkloadInstance {id: 'answer-truth-affected:vi-cloud'}) MATCH (w:Workload {id: 'answer-truth-affected:wl-cloud'}) CREATE (i)-[:INSTANCE_OF]->(w)`,
	`MATCH (i:WorkloadInstance {id: 'answer-truth-affected:vi-cloud'}) MATCH (p:CloudResource {uid: 'answer-truth-affected:vp-role'}) CREATE (i)-[:USES]->(p)`,
}

const affectedLiveCleanup = `MATCH (n)
WHERE n.uid STARTS WITH 'answer-truth-affected:' OR n.id STARTS WITH 'answer-truth-affected:' OR n.action STARTS WITH 'answer-truth-affected:'
DETACH DELETE n`

type affectedLiveGraph struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (g affectedLiveGraph) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := g.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: g.database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for i, key := range record.Keys {
			row[key] = record.Values[i]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (g affectedLiveGraph) write(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	session := g.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: g.database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		t.Fatalf("write %q: %v", cypher, err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume %q: %v", cypher, err)
	}
}

func TestLiveAffectedGate(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database == "" {
		database = "nornic"
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

	graph := affectedLiveGraph{driver: driver, database: database}
	graph.write(ctx, t, affectedLiveCleanup)
	for _, stmt := range affectedLiveSeed {
		graph.write(ctx, t, stmt)
	}
	defer graph.write(context.Background(), t, affectedLiveCleanup)

	for name, tc := range map[string]struct {
		run  func(context.Context, affectedLiveGraph) (int, error)
		want int
	}{
		"repos": {
			run: func(ctx context.Context, g affectedLiveGraph) (int, error) {
				return ReposWithCloudCallers(ctx, g, []string{
					"answer-truth-affected:repo-cloud",
					"answer-truth-affected:repo-plain",
					"answer-truth-affected:repo-missing",
				})
			},
			want: 1,
		},
		"workloads": {
			run: func(ctx context.Context, g affectedLiveGraph) (int, error) {
				return ReposWithCloudCallersForWorkloads(ctx, g, []string{
					"answer-truth-affected:wl-cloud",
					"answer-truth-affected:wl-plain",
				})
			},
			want: 1,
		},
		"principals": {
			run: func(ctx context.Context, g affectedLiveGraph) (int, error) {
				return ReposWithCloudCallersForPrincipals(ctx, g, []string{
					"answer-truth-affected:vp-role",
				})
			},
			want: 1,
		},
		"resources": {
			run: func(ctx context.Context, g affectedLiveGraph) (int, error) {
				return ReposWithCloudCallersForResources(ctx, g, []string{
					"answer-truth-affected:vp-role",
				})
			},
			want: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			n, err := tc.run(ctx, graph)
			if err != nil {
				t.Fatalf("%s gate: %v", name, err)
			}
			if n != tc.want {
				t.Errorf("%s gate = %d, want %d", name, n, tc.want)
			}
		})
	}
}
