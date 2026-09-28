// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_infra_scope_neo4j

package query

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"
)

// TestLiveGraphSummaryNeo4jDegreePage compares the optimized Neo4j result
// with the unchanged raw-edge Go ranking on an isolated graph fixture.
func TestLiveGraphSummaryNeo4jDegreePage(t *testing.T) {
	if os.Getenv("ESHU_GRAPH_SUMMARY_NEO4J_FIXTURE") != "1" {
		t.Skip("set ESHU_GRAPH_SUMMARY_NEO4J_FIXTURE=1 for the isolated Neo4j fixture")
	}
	live := openLiveNeo4jScope(t)
	repoID := fmt.Sprintf("graphsummary-7251-%d", time.Now().UnixNano())
	params := map[string]any{"repo_id": repoID, "a": repoID + "-a", "b": repoID + "-b"}
	live.raw(t, 30*time.Second, `CREATE (a:Function {repo_id:$repo_id, uid:$a, id:$a, name:'alpha', start_line:1}),
       (b:Function {repo_id:$repo_id, uid:$b, id:$b, name:'beta', relative_path:'b.go', start_line:2}),
       (a)-[:CALLS]->(b), (a)-[:CALLS]->(b), (b)-[:CALLS]->(a)`, params)
	t.Cleanup(func() {
		live.raw(t, 30*time.Second, "MATCH (n:Function {repo_id:$repo_id}) DETACH DELETE n", params)
	})

	handler := &InfraHandler{GraphBackend: GraphBackendNeo4j, Neo4j: live.reader}
	want, wantTruncated, err := handler.graphSummaryHotEntitiesRaw(context.Background(), repoID, 10)
	if err != nil {
		t.Fatalf("raw hot entities: %v", err)
	}
	if len(want) != 2 || wantTruncated || StringVal(want[0], "function_id") != params["a"] {
		t.Fatalf("raw fixture oracle has unexpected cardinality, truncation, or rank")
	}
	got, gotTruncated, err := handler.graphSummaryHotEntities(context.Background(), repoID, 10)
	if err != nil {
		t.Fatalf("Neo4j hot entities: %v", err)
	}
	if gotTruncated != wantTruncated || !reflect.DeepEqual(got, want) {
		t.Fatalf("Neo4j degree page differs from raw-edge Go ranking: got=%#v, want=%#v", got, want)
	}
}
