// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live proof for the #6782 row-token graph check: the gate's own Bolt reader
// (boltGraphCounter.ListGraphElementProperties) returns node and edge property
// maps on both backends, the check flags a junk token, and it passes real data
// that only looks like one (a File named "row.go", an explicit nil).
//
// Run each against a fresh, isolated container (the check scans the whole
// graph, so the expected counts assume an empty database):
//
//	docker run -d --name eshu-6782-rt-nornic -p 127.0.0.1:28060:7687 \
//	  -e NORNICDB_NO_AUTH=true -e NORNICDB_EMBEDDING_ENABLED=false \
//	  ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-6915-c4de1c5c@sha256:76dd5f9b016db047ba867b69b13e4b2dd0f7b90c2764059476821ba4ce52274a
//	docker run -d --name eshu-6782-rt-neo4j -p 127.0.0.1:28061:7687 -e NEO4J_AUTH=none \
//	  neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:28060 go test ./cmd/golden-corpus-gate \
//	  -tags live_nornicdb_answer_truth -run TestLiveUnresolvedRowTokenCheck -count=1 -v
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:28061 ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  go test ./cmd/golden-corpus-gate -tags live_nornicdb_answer_truth \
//	  -run TestLiveUnresolvedRowTokenCheck -count=1 -v
package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	gg "github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/graph"
)

// liveSchemaExecutor applies the production graph schema through the driver.
type liveSchemaExecutor struct {
	driver neo4j.DriverWithContext
	db     string
}

func (e liveSchemaExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	_, err := neo4j.ExecuteQuery(ctx, e.driver, stmt.Cypher, stmt.Parameters,
		neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(e.db))
	return err
}

func TestLiveUnresolvedRowTokenCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	backend := graph.SchemaBackendNornicDB
	db := "nornic"
	if strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")) == string(graph.SchemaBackendNeo4j) {
		backend, db = graph.SchemaBackendNeo4j, "neo4j"
	}
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}
	if err := graph.EnsureSchemaWithBackend(ctx, liveSchemaExecutor{driver: driver, db: db}, slog.New(slog.DiscardHandler), backend); err != nil {
		t.Fatalf("ensure %s schema: %v", backend, err)
	}
	write := func(cypher string, params map[string]any) {
		t.Helper()
		if _, err := neo4j.ExecuteQuery(ctx, driver, cypher, params,
			neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(db)); err != nil {
			t.Fatalf("write %q: %v", cypher, err)
		}
	}
	counter := &boltGraphCounter{driver: driver, db: db}
	if before, err := counter.ListGraphElementProperties(ctx); err != nil || len(before) != 0 {
		t.Fatalf("database not empty before seeding (%d elements, err=%v); use a fresh container", len(before), err)
	}

	for _, id := range []string{"live-6782-rt:a", "live-6782-rt:b", "live-6782-rt:c"} {
		write(`CREATE (:Repository {id: $id, name: $id})`, map[string]any{"id": id})
	}
	write(`CREATE (:File {uid: 'live-6782-rt:file', path: '/live-6782-rt/row.go', name: 'row.go'})`, nil)
	// The production defect shape: the row map omits source_tool.
	write(`UNWIND $rows AS row
MATCH (a:Repository {id: row.a}) MATCH (b:Repository {id: row.b})
MERGE (a)-[rel:DEPENDS_ON]->(b) SET rel.source_tool = row.source_tool`,
		map[string]any{"rows": []any{map[string]any{"a": "live-6782-rt:a", "b": "live-6782-rt:b"}}})
	// The fixed shape: the key is present with an explicit nil.
	write(`UNWIND $rows AS row
MATCH (a:Repository {id: row.a}) MATCH (b:Repository {id: row.b})
MERGE (a)-[rel:DEPENDS_ON]->(b) SET rel.source_tool = row.source_tool`,
		map[string]any{"rows": []any{map[string]any{"a": "live-6782-rt:b", "b": "live-6782-rt:c", "source_tool": nil}}})
	// A literal token, which every backend stores as written.
	write(`MATCH (a:Repository {id: 'live-6782-rt:a'}) MATCH (c:Repository {id: 'live-6782-rt:c'})
MERGE (a)-[rel:CALLS]->(c) SET rel.call_kind = 'row.call_kind'`, nil)

	elements, err := counter.ListGraphElementProperties(ctx)
	if err != nil {
		t.Fatalf("ListGraphElementProperties: %v", err)
	}
	var nodes, edges int
	for _, el := range elements {
		switch el.Kind {
		case "node":
			nodes++
		case "edge":
			edges++
		}
	}
	if nodes != 4 || edges != 3 {
		t.Fatalf("read back %d nodes and %d edges, want 4 and 3: %+v", nodes, edges, elements)
	}
	finding := gg.EvaluateUnresolvedRowTokens(elements)
	t.Logf("%s finding: ok=%t %s", backend, finding.OK, finding.Detail)
	if finding.OK {
		t.Fatalf("%s: the literal row.call_kind token was not flagged: %s", backend, finding.Detail)
	}
	if !strings.Contains(finding.Detail, "edge CALLS.call_kind=row.call_kind (1)") {
		t.Fatalf("%s: detail does not name the CALLS token: %s", backend, finding.Detail)
	}
	if strings.Contains(finding.Detail, "row.go") {
		t.Fatalf("%s: a File named row.go was flagged: %s", backend, finding.Detail)
	}
	// NornicDB v1.3.3 stores the omitted key as text; Neo4j leaves it absent.
	wantMissingKeyJunk := backend == graph.SchemaBackendNornicDB
	if got := strings.Contains(finding.Detail, "edge DEPENDS_ON.source_tool=row.source_tool (1)"); got != wantMissingKeyJunk {
		t.Fatalf("%s: missing-key junk flagged=%t, want %t: %s", backend, got, wantMissingKeyJunk, finding.Detail)
	}
}
