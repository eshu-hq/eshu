// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live backend-parity proof for issue #6849: POST /api/v0/code/relationships
// with transitive CALLS answers the BFS contract on BOTH graph backends --
// each reachable node once, at its shortest depth, with the start node
// excluded. The Neo4j-compat route used to return every matching path, so a
// node appeared at several depths and a cycle returned the start node as its
// own callee, while the NornicDB breadth-first walk already applied the BFS
// rule. The test drives the real route on the backend named by
// ESHU_LIVE_GRAPH_BACKEND and pins identical node sets on both.
//
// Ground truth, by construction:
//   - TPing <-> TPong form a 2-cycle: outgoing and incoming from TPing are
//     exactly [TPong @1]. The old Neo4j statement also returned TPing @2.
//   - A -> {B, C} -> D is a diamond: outgoing from A is exactly
//     [B @1, C @1, D @2]. The old statement returned D twice (via B and C).
//   - Self calls itself: outgoing from Self is empty. The old statement
//     returned Self @1.
//
// Run against isolated containers on the pinned images:
//
//	docker run -d --name eshu-6849-nornic -p 127.0.0.1:27962:7687 \
//	  -e NORNICDB_NO_AUTH=true -e NORNICDB_EMBEDDING_ENABLED=false \
//	  timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27962 ESHU_LIVE_GRAPH_BACKEND=nornicdb \
//	  go test ./internal/query/codequery -tags live_nornicdb_answer_truth \
//	  -run TestLiveTransitiveCallersParity -count=1 -v
//
//	docker run -d --name eshu-6849-neo4j -p 127.0.0.1:27963:7687 \
//	  -e NEO4J_AUTH=none neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27963 ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  go test ./internal/query/codequery -tags live_nornicdb_answer_truth \
//	  -run TestLiveTransitiveCallersParity -count=1 -v
package codequery

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	eshugraph "github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// transitiveParitySeed uses literal property values only: on the pinned
// NornicDB v1.3.3 an expression inside a CREATE property map can be stored
// as mangled literal text.
var transitiveParitySeed = []string{
	`CREATE (:Repository {id: 'transitive-parity-6849:repo', name: 'transitive-parity-6849-repo'})`,
	`CREATE (:Function {uid: 'transitive-parity-6849:ping', id: 'transitive-parity-6849:ping', name: 'TransitiveParityPing', repo_id: 'transitive-parity-6849:repo', language: 'go'})`,
	`CREATE (:Function {uid: 'transitive-parity-6849:pong', id: 'transitive-parity-6849:pong', name: 'TransitiveParityPong', repo_id: 'transitive-parity-6849:repo', language: 'go'})`,
	`CREATE (:Function {uid: 'transitive-parity-6849:a', id: 'transitive-parity-6849:a', name: 'TransitiveParityA', repo_id: 'transitive-parity-6849:repo', language: 'go'})`,
	`CREATE (:Function {uid: 'transitive-parity-6849:b', id: 'transitive-parity-6849:b', name: 'TransitiveParityB', repo_id: 'transitive-parity-6849:repo', language: 'go'})`,
	`CREATE (:Function {uid: 'transitive-parity-6849:c', id: 'transitive-parity-6849:c', name: 'TransitiveParityC', repo_id: 'transitive-parity-6849:repo', language: 'go'})`,
	`CREATE (:Function {uid: 'transitive-parity-6849:d', id: 'transitive-parity-6849:d', name: 'TransitiveParityD', repo_id: 'transitive-parity-6849:repo', language: 'go'})`,
	`CREATE (:Function {uid: 'transitive-parity-6849:self', id: 'transitive-parity-6849:self', name: 'TransitiveParitySelf', repo_id: 'transitive-parity-6849:repo', language: 'go'})`,
	`CREATE (:File {id: 'transitive-parity-6849:file', path: '/transitive-parity-6849/calls.go', relative_path: 'calls.go', language: 'go', repo_id: 'transitive-parity-6849:repo'})`,
	`MATCH (r:Repository {id: 'transitive-parity-6849:repo'}) MATCH (f:File {id: 'transitive-parity-6849:file'}) CREATE (r)-[:REPO_CONTAINS]->(f)`,
	// One CONTAINS edge per function: a bulk MATCH..WHERE..CREATE linker
	// silently creates zero edges on NornicDB v1.3.3 (measured on the
	// pinned image), so each edge gets its own anchored statement.
	`MATCH (f:File {id: 'transitive-parity-6849:file'}) MATCH (n:Function {uid: 'transitive-parity-6849:ping'}) CREATE (f)-[:CONTAINS]->(n)`,
	`MATCH (f:File {id: 'transitive-parity-6849:file'}) MATCH (n:Function {uid: 'transitive-parity-6849:pong'}) CREATE (f)-[:CONTAINS]->(n)`,
	`MATCH (f:File {id: 'transitive-parity-6849:file'}) MATCH (n:Function {uid: 'transitive-parity-6849:a'}) CREATE (f)-[:CONTAINS]->(n)`,
	`MATCH (f:File {id: 'transitive-parity-6849:file'}) MATCH (n:Function {uid: 'transitive-parity-6849:b'}) CREATE (f)-[:CONTAINS]->(n)`,
	`MATCH (f:File {id: 'transitive-parity-6849:file'}) MATCH (n:Function {uid: 'transitive-parity-6849:c'}) CREATE (f)-[:CONTAINS]->(n)`,
	`MATCH (f:File {id: 'transitive-parity-6849:file'}) MATCH (n:Function {uid: 'transitive-parity-6849:d'}) CREATE (f)-[:CONTAINS]->(n)`,
	`MATCH (f:File {id: 'transitive-parity-6849:file'}) MATCH (n:Function {uid: 'transitive-parity-6849:self'}) CREATE (f)-[:CONTAINS]->(n)`,
	`MATCH (a:Function {uid: 'transitive-parity-6849:ping'}) MATCH (b:Function {uid: 'transitive-parity-6849:pong'}) CREATE (a)-[:CALLS]->(b)`,
	`MATCH (a:Function {uid: 'transitive-parity-6849:pong'}) MATCH (b:Function {uid: 'transitive-parity-6849:ping'}) CREATE (a)-[:CALLS]->(b)`,
	`MATCH (a:Function {uid: 'transitive-parity-6849:a'}) MATCH (b:Function {uid: 'transitive-parity-6849:b'}) CREATE (a)-[:CALLS]->(b)`,
	`MATCH (a:Function {uid: 'transitive-parity-6849:a'}) MATCH (b:Function {uid: 'transitive-parity-6849:c'}) CREATE (a)-[:CALLS]->(b)`,
	`MATCH (a:Function {uid: 'transitive-parity-6849:b'}) MATCH (b:Function {uid: 'transitive-parity-6849:d'}) CREATE (a)-[:CALLS]->(b)`,
	`MATCH (a:Function {uid: 'transitive-parity-6849:c'}) MATCH (b:Function {uid: 'transitive-parity-6849:d'}) CREATE (a)-[:CALLS]->(b)`,
	`MATCH (a:Function {uid: 'transitive-parity-6849:self'}) CREATE (a)-[:CALLS]->(a)`,
}

const transitiveParityCleanup = `MATCH (n) WHERE n.uid STARTS WITH 'transitive-parity-6849:' OR n.id STARTS WITH 'transitive-parity-6849:' DETACH DELETE n`

func TestLiveTransitiveCallersParity(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	backend := strings.TrimSpace(strings.ToLower(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")))
	if backend != "nornicdb" && backend != "neo4j" {
		t.Fatal("ESHU_LIVE_GRAPH_BACKEND is required (nornicdb|neo4j)")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_DATABASE"))
	if database == "" {
		database = "nornic"
		if backend == "neo4j" {
			database = "neo4j"
		}
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

	reader := codequeryLiveReader{driver: driver, database: database}
	reader.write(ctx, t, transitiveParityCleanup)
	defer reader.write(context.Background(), t, transitiveParityCleanup)

	schemaBackend := eshugraph.SchemaBackendNeo4j
	if backend == "nornicdb" {
		schemaBackend = eshugraph.SchemaBackendNornicDB
	}
	if err := eshugraph.EnsureSchemaWithBackend(ctx, reader, nil, schemaBackend); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	for _, stmt := range transitiveParitySeed {
		reader.write(ctx, t, stmt)
	}

	graphBackend := GraphBackendNeo4j
	if backend == "nornicdb" {
		graphBackend = GraphBackendNornicDB
	}
	handler := &CodeHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative, GraphBackend: graphBackend}

	cases := []struct {
		name      string
		body      map[string]any
		field     string
		wantPairs []string
	}{
		{
			name:      "cycle_outgoing",
			body:      map[string]any{"name": "TransitiveParityPing", "repo_id": "transitive-parity-6849:repo", "direction": "outgoing", "relationship_type": "CALLS", "transitive": true, "max_depth": 4},
			field:     "outgoing",
			wantPairs: []string{"TransitiveParityPong@1"},
		},
		{
			name:      "cycle_incoming",
			body:      map[string]any{"name": "TransitiveParityPing", "repo_id": "transitive-parity-6849:repo", "direction": "incoming", "relationship_type": "CALLS", "transitive": true, "max_depth": 4},
			field:     "incoming",
			wantPairs: []string{"TransitiveParityPong@1"},
		},
		{
			name:      "diamond_outgoing",
			body:      map[string]any{"name": "TransitiveParityA", "repo_id": "transitive-parity-6849:repo", "direction": "outgoing", "relationship_type": "CALLS", "transitive": true, "max_depth": 4},
			field:     "outgoing",
			wantPairs: []string{"TransitiveParityB@1", "TransitiveParityC@1", "TransitiveParityD@2"},
		},
		{
			name:      "self_outgoing_empty",
			body:      map[string]any{"name": "TransitiveParitySelf", "repo_id": "transitive-parity-6849:repo", "direction": "outgoing", "relationship_type": "CALLS", "transitive": true, "max_depth": 4},
			field:     "outgoing",
			wantPairs: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := queryTransitivePairs(t, handler, tc.body, tc.field)
			sort.Strings(got)
			want := append([]string{}, tc.wantPairs...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s %s: pairs = %v, want %v", backend, tc.name, got, want)
			}
		})
	}
}

// queryTransitivePairs drives the real POST /api/v0/code/relationships
// handler and returns "name@depth" pairs from the requested result field.
func queryTransitivePairs(t *testing.T, handler *CodeHandler, body map[string]any, field string) []string {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.handleRelationships(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	rows, _ := envelope.Data[field].([]any)
	pairs := make([]string, 0, len(rows))
	for _, row := range rows {
		m, _ := row.(map[string]any)
		if m == nil {
			continue
		}
		nameKey := "target_name"
		if field == "incoming" {
			nameKey = "source_name"
		}
		name, _ := m[nameKey].(string)
		var depth int
		switch d := m["depth"].(type) {
		case float64:
			depth = int(d)
		case int:
			depth = d
		}
		if name == "" {
			continue
		}
		pairs = append(pairs, name+"@"+strconv.Itoa(depth))
	}
	return pairs
}
