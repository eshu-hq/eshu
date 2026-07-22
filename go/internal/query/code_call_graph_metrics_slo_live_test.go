// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build call_graph_metrics_slo_live

package query

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	callGraphMetricsSLOFunctionCount = 43167
	callGraphMetricsSLOEdgeCount     = 42197
	callGraphMetricsSLOBatchSize     = 1000
	callGraphMetricsSLOLimit         = 3 * time.Second
	callGraphMetricsSLORepoID        = "synthetic-call-graph-slo"
)

func TestCallGraphMetricsInteractiveSLO(t *testing.T) {
	if os.Getenv("ESHU_CALL_GRAPH_METRICS_SLO_LIVE") != "1" {
		t.Skip("set ESHU_CALL_GRAPH_METRICS_SLO_LIVE=1 to run the live SLO proof")
	}
	if os.Getenv("ESHU_CALL_GRAPH_METRICS_SLO_ISOLATED") != "1" {
		t.Fatal("ESHU_CALL_GRAPH_METRICS_SLO_ISOLATED=1 is required because this test replaces graph data")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	driver, database := openCallGraphMetricsSLOGraph(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedCallGraphMetricsSLOGraph(ctx, t, driver, database)

	handler := &CodeHandler{Neo4j: NewNeo4jReader(driver, database)}
	for _, metricType := range []string{"hub_functions", "recursive_functions"} {
		t.Run(metricType, func(t *testing.T) {
			req := callGraphMetricsRequest{
				MetricType: metricType,
				RepoID:     callGraphMetricsSLORepoID,
				Limit:      intPtr(25),
			}
			durations := make([]time.Duration, 4)
			for index := range durations {
				started := time.Now()
				data, err := handler.callGraphMetricsData(ctx, req)
				durations[index] = time.Since(started)
				if err != nil {
					t.Fatalf("callGraphMetricsData(%s): %v", metricType, err)
				}
				assertCallGraphMetricsSLOResult(t, metricType, data)
				if durations[index] > callGraphMetricsSLOLimit {
					t.Fatalf("%s run %d duration = %s, exceeds indexed-workspace SLO %s",
						metricType, index+1, durations[index], callGraphMetricsSLOLimit)
				}
			}
			warm := append([]time.Duration(nil), durations[1:]...)
			sort.Slice(warm, func(i, j int) bool { return warm[i] < warm[j] })
			t.Logf("metric_type=%s functions=%d edges=%d first=%s warm_median=%s limit=%s",
				metricType, callGraphMetricsSLOFunctionCount, callGraphMetricsSLOEdgeCount,
				durations[0], warm[1], callGraphMetricsSLOLimit)
		})
	}
}

func openCallGraphMetricsSLOGraph(
	ctx context.Context,
	t *testing.T,
) (neo4jdriver.DriverWithContext, string) {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	auth := neo4jdriver.NoAuth()
	if username := strings.TrimSpace(os.Getenv("ESHU_NEO4J_USERNAME")); username != "" {
		auth = neo4jdriver.BasicAuth(username, os.Getenv("ESHU_NEO4J_PASSWORD"), "")
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, auth)
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(context.Background())
		t.Fatalf("verify graph connectivity: %v", err)
	}
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database == "" {
		database = "neo4j"
	}
	return driver, database
}

func seedCallGraphMetricsSLOGraph(
	ctx context.Context,
	t *testing.T,
	driver neo4jdriver.DriverWithContext,
	database string,
) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: database,
	})
	defer func() { _ = session.Close(context.Background()) }()
	for _, statement := range []string{
		"MATCH (node) DETACH DELETE node",
		"CREATE CONSTRAINT call_graph_metrics_slo_function_id IF NOT EXISTS FOR (fn:Function) REQUIRE fn.id IS UNIQUE",
		"CREATE INDEX call_graph_metrics_slo_function_repo IF NOT EXISTS FOR (fn:Function) ON (fn.repo_id)",
		"CALL db.awaitIndexes(120)",
	} {
		runCallGraphMetricsSLOWrite(ctx, t, session, statement, nil)
	}
	for start := 0; start < callGraphMetricsSLOFunctionCount; start += callGraphMetricsSLOBatchSize {
		end := min(start+callGraphMetricsSLOBatchSize, callGraphMetricsSLOFunctionCount)
		rows := make([]map[string]any, 0, end-start)
		for index := start; index < end; index++ {
			language := "go"
			if index%2 != 0 {
				language = "typescript"
			}
			rows = append(rows, map[string]any{
				"id":       fmt.Sprintf("fn-%05d", index),
				"path":     fmt.Sprintf("src/file-%05d.go", index/100),
				"language": language,
				"name":     fmt.Sprintf("function%05d", index),
				"line":     index + 1,
			})
		}
		runCallGraphMetricsSLOWrite(ctx, t, session, `UNWIND $rows AS row
CREATE (:Function {id: row.id, uid: row.id, repo_id: $repo_id, relative_path: row.path,
                  language: row.language, name: row.name, start_line: row.line, end_line: row.line + 2})`,
			map[string]any{"repo_id": callGraphMetricsSLORepoID, "rows": rows})
	}
	edges := make([]map[string]any, 0, callGraphMetricsSLOEdgeCount)
	for index := 0; index < 42190; index++ {
		edges = append(edges, callGraphMetricsSLOEdge(index, index+1))
	}
	edges = append(edges,
		callGraphMetricsSLOEdge(100, 101), callGraphMetricsSLOEdge(100, 101),
		callGraphMetricsSLOEdge(11, 10), callGraphMetricsSLOEdge(20, 20),
		callGraphMetricsSLOEdge(31, 30), callGraphMetricsSLOEdge(41, 40),
		callGraphMetricsSLOEdge(1000, 2000),
	)
	if len(edges) != callGraphMetricsSLOEdgeCount {
		t.Fatalf("edge count = %d, want %d", len(edges), callGraphMetricsSLOEdgeCount)
	}
	for start := 0; start < len(edges); start += callGraphMetricsSLOBatchSize {
		end := min(start+callGraphMetricsSLOBatchSize, len(edges))
		runCallGraphMetricsSLOWrite(ctx, t, session, `UNWIND $rows AS row
MATCH (source:Function {id: row.source})
MATCH (target:Function {id: row.target})
CREATE (source)-[:CALLS]->(target)`, map[string]any{"rows": edges[start:end]})
	}
}

func callGraphMetricsSLOEdge(source int, target int) map[string]any {
	return map[string]any{
		"source": fmt.Sprintf("fn-%05d", source),
		"target": fmt.Sprintf("fn-%05d", target),
	}
}

func runCallGraphMetricsSLOWrite(
	ctx context.Context,
	t *testing.T,
	session neo4jdriver.SessionWithContext,
	cypher string,
	params map[string]any,
) {
	t.Helper()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("run SLO graph write: %v", err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume SLO graph write: %v", err)
	}
}

func assertCallGraphMetricsSLOResult(t *testing.T, metricType string, data map[string]any) {
	t.Helper()
	wantCount := 25
	wantTruncated := true
	if metricType == "recursive_functions" {
		wantCount = 4
		wantTruncated = false
	}
	if got := IntVal(data, "count"); got != wantCount {
		t.Fatalf("%s count = %d, want %d", metricType, got, wantCount)
	}
	if got := BoolVal(data, "truncated"); got != wantTruncated {
		t.Fatalf("%s truncated = %t, want %t", metricType, got, wantTruncated)
	}
}
