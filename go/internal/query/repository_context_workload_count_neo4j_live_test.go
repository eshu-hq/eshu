// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestRepositoryContextMaterializedWorkloadCountNeo4jLive proves that the
// production HTTP handler reports distinct graph workloads even when the
// repository read model retains a different workload-name count.
func TestRepositoryContextMaterializedWorkloadCountNeo4jLive(t *testing.T) {
	live := openRepositoryCountNeo4jLive(t)
	nonce := fmt.Sprintf("7542-%d", time.Now().UnixNano())
	prefix := "repository:" + nonce
	t.Cleanup(func() {
		live.raw(t, 30*time.Second, `MATCH (n) WHERE
  (n:Repository AND n.id STARTS WITH $repo_prefix) OR
  (n:Workload AND n.id STARTS WITH $workload_prefix)
DETACH DELETE n`, map[string]any{
			"repo_prefix":     prefix,
			"workload_prefix": "workload:" + nonce,
		})
	})

	zeroRepo := prefix + "-retained"
	twoRepo := prefix + "-materialized"
	otherRepo := prefix + "-other"
	live.raw(t, 30*time.Second, `CREATE
  (:Repository {id: $zero, name: 'retained'}),
  (:Repository {id: $two, name: 'materialized'}),
  (:Repository {id: $other, name: 'other'})`, map[string]any{
		"zero": zeroRepo, "two": twoRepo, "other": otherRepo,
	})
	live.raw(t, 30*time.Second, `MATCH (r:Repository {id: $repo_id})
CREATE (a:Workload {id: $first}), (b:Workload {id: $second})
CREATE (r)-[:DEFINES]->(a), (r)-[:DEFINES]->(a), (r)-[:DEFINES]->(b)`, map[string]any{
		"repo_id": twoRepo, "first": "workload:" + nonce + "-a", "second": "workload:" + nonce + "-b",
	})
	live.raw(t, 30*time.Second, `MATCH (r:Repository {id: $repo_id})
CREATE (w:Workload {id: $workload_id})
CREATE (r)-[:DEFINES]->(w)`, map[string]any{
		"repo_id": otherRepo, "workload_id": "workload:" + nonce + "-other",
	})

	for _, tc := range []struct {
		name      string
		repo      string
		want      float64
		wantEdges int64
	}{
		{name: "retained name without materialization", repo: zeroRepo, want: 0, wantEdges: 0},
		{name: "two distinct workloads and duplicate defines", repo: twoRepo, want: 2, wantEdges: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := live.raw(t, 30*time.Second, `MATCH (r:Repository {id: $repo_id})
OPTIONAL MATCH (r)-[:DEFINES]->(w:Workload)
RETURN count(w) AS edges, count(DISTINCT w) AS workloads`, map[string]any{"repo_id": tc.repo})
			if len(rows) != 1 || rows[0]["edges"] != tc.wantEdges || rows[0]["workloads"] != int64(tc.want) {
				t.Fatalf("graph fixture = %#v, want edges=%d workloads=%d", rows, tc.wantEdges, int64(tc.want))
			}
			handler := &RepositoryHandler{
				Neo4j: live.reader,
				Content: fakePortContentStore{
					coverage: RepositoryContentCoverage{Available: true},
					summary: RepositoryReadModelSummary{
						Available: true, WorkloadNames: []string{"retained-intent"},
						PlatformCount: 3, DependencyCount: 4,
					},
				},
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/"+tc.repo+"/context", nil)
			req.SetPathValue("repo_id", tc.repo)
			rec := httptest.NewRecorder()
			handler.GetRepositoryContext(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
			}
			var response map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got := response["workload_count"]; got != tc.want {
				t.Fatalf("workload_count = %#v, want %v", got, tc.want)
			}
			if response["platform_count"] != float64(3) || response["dependency_count"] != float64(4) {
				t.Fatalf("summary counts changed: platform=%#v dependency=%#v", response["platform_count"], response["dependency_count"])
			}
		})
	}
}

// repositoryCountNeo4jLive keeps fixture writes separate from the production
// Neo4jReader used by the HTTP handler.
type repositoryCountNeo4jLive struct {
	driver   neo4jdriver.DriverWithContext
	database string
	reader   *Neo4jReader
}

func openRepositoryCountNeo4jLive(t *testing.T) *repositoryCountNeo4jLive {
	t.Helper()
	if backend := os.Getenv("ESHU_LIVE_GRAPH_BACKEND"); backend != "" && backend != "neo4j" {
		t.Fatalf("repository count live proof requires neo4j backend, got %q", backend)
	}
	uri := os.Getenv("ESHU_NEO4J_URI")
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required for repository count live proof")
	}
	database := os.Getenv("ESHU_LIVE_GRAPH_DATABASE")
	if database == "" {
		database = "neo4j"
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open Neo4j driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify Neo4j connectivity: %v", err)
	}
	return &repositoryCountNeo4jLive{driver: driver, database: database, reader: NewNeo4jReader(driver, database)}
}

func (l *repositoryCountNeo4jLive) raw(t *testing.T, timeout time.Duration, cypher string, params map[string]any) []map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	session := l.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: l.database})
	defer func() { _ = session.Close(context.Background()) }()
	result, err := session.Run(ctx, cypher, params, neo4jdriver.WithTxTimeout(timeout))
	if err != nil {
		t.Fatalf("fixture Cypher failed: %v", err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		t.Fatalf("collect fixture Cypher: %v", err)
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		rows = append(rows, record.AsMap())
	}
	return rows
}
