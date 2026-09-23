// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live backend-parity proof for #6782: POST /api/v0/code/call-chain answers a
// self-recursive request (start = end) on BOTH graph backends, with every hop
// held inside the request's repository bound.
//
// The B-7 golden corpus asks for the chain from recursionFib to recursionFib.
// NornicDB's route walks CALLS breadth-first in Go and returns the cycle;
// Neo4j's route ran legacy shortestPath(), which raises
// Neo.DatabaseError.Statement.ExecutionFailed for start = end, so the tool
// failed with HTTP 500. The test drives the real route on the backend named by
// ESHU_LIVE_GRAPH_BACKEND.
//
// Run against an isolated container per backend:
//
//	docker run -d --name eshu-live-nornic -p 127.0.0.1:27940:7687 \
//	  -e NORNICDB_NO_AUTH=true -e NORNICDB_EMBEDDING_ENABLED=false \
//	  ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1
//	docker run -d --name eshu-live-neo4j -p 127.0.0.1:27950:7687 -e NEO4J_AUTH=none \
//	  neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27940 go test ./internal/query/codequery/chain \
//	  -tags live_nornicdb_answer_truth -run TestLiveCallChainSelfRecursion -count=1 -v
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27950 ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  go test ./internal/query/codequery/chain -tags live_nornicdb_answer_truth \
//	  -run TestLiveCallChainSelfRecursion -count=1 -v
//
// ESHU_LIVE_GRAPH_BACKEND is nornicdb (default) or neo4j. ESHU_LIVE_GRAPH_DATABASE
// defaults to "nornic" for NornicDB and "neo4j" for Neo4j.
package chain_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// selfRecursionSeed uses literal property values only: on the pinned NornicDB
// v1.3.3 an expression inside a CREATE property map can be stored as mangled
// literal text.
//
// Ground truth, by construction:
//   - LiveSelfFib calls itself: the chain LiveSelfFib -> LiveSelfFib, depth 1.
//   - LiveSelfLoop has no self-call. Its shortest cycle, LoopA -> Bridge ->
//     LoopA (depth 2), crosses repo-b; the in-repo cycle LoopA -> Mid1 -> Mid2
//     -> LoopA (depth 3) is the answer when the request is bound to repo-a.
//   - LiveSelfPlainStart -> LiveSelfPlainEnd is an ordinary start != end chain
//     (depth 1), so an ordinary chain stays covered.
var selfRecursionSeed = []string{
	`CREATE (:Repository {id: 'live-6782-chain:repo-a', name: 'live-6782-chain-repo-a'})`,
	`CREATE (:Function {uid: 'live-6782-chain:fib', id: 'live-6782-chain:fib', name: 'LiveSelfFib', repo_id: 'live-6782-chain:repo-a', language: 'go'})`,
	`CREATE (:Function {uid: 'live-6782-chain:loop', id: 'live-6782-chain:loop', name: 'LiveSelfLoop', repo_id: 'live-6782-chain:repo-a', language: 'go'})`,
	`CREATE (:Function {uid: 'live-6782-chain:bridge', id: 'live-6782-chain:bridge', name: 'LiveSelfBridge', repo_id: 'live-6782-chain:repo-b', language: 'go'})`,
	`CREATE (:Function {uid: 'live-6782-chain:mid1', id: 'live-6782-chain:mid1', name: 'LiveSelfMid1', repo_id: 'live-6782-chain:repo-a', language: 'go'})`,
	`CREATE (:Function {uid: 'live-6782-chain:mid2', id: 'live-6782-chain:mid2', name: 'LiveSelfMid2', repo_id: 'live-6782-chain:repo-a', language: 'go'})`,
	`CREATE (:Function {uid: 'live-6782-chain:plain-start', id: 'live-6782-chain:plain-start', name: 'LiveSelfPlainStart', repo_id: 'live-6782-chain:repo-a', language: 'go'})`,
	`CREATE (:Function {uid: 'live-6782-chain:plain-end', id: 'live-6782-chain:plain-end', name: 'LiveSelfPlainEnd', repo_id: 'live-6782-chain:repo-a', language: 'go'})`,
	`CREATE (:Repository {id: 'live-6782-chain:repo-b', name: 'live-6782-chain-repo-b'})`,
	`CREATE (:File {id: 'live-6782-chain:file-a', path: '/live-6782/a.go', relative_path: 'a.go', language: 'go', repo_id: 'live-6782-chain:repo-a'})`,
	`CREATE (:File {id: 'live-6782-chain:file-b', path: '/live-6782/b.go', relative_path: 'b.go', language: 'go', repo_id: 'live-6782-chain:repo-b'})`,
	`MATCH (r:Repository {id: 'live-6782-chain:repo-a'}) MATCH (f:File {id: 'live-6782-chain:file-a'}) CREATE (r)-[:REPO_CONTAINS]->(f)`,
	`MATCH (r:Repository {id: 'live-6782-chain:repo-b'}) MATCH (f:File {id: 'live-6782-chain:file-b'}) CREATE (r)-[:REPO_CONTAINS]->(f)`,
	`MATCH (f:File {id: 'live-6782-chain:file-a'}) MATCH (n:Function {uid: 'live-6782-chain:fib'}) CREATE (f)-[:CONTAINS]->(n)`,
	`MATCH (f:File {id: 'live-6782-chain:file-a'}) MATCH (n:Function {uid: 'live-6782-chain:loop'}) CREATE (f)-[:CONTAINS]->(n)`,
	`MATCH (f:File {id: 'live-6782-chain:file-a'}) MATCH (n:Function {uid: 'live-6782-chain:mid1'}) CREATE (f)-[:CONTAINS]->(n)`,
	`MATCH (f:File {id: 'live-6782-chain:file-a'}) MATCH (n:Function {uid: 'live-6782-chain:mid2'}) CREATE (f)-[:CONTAINS]->(n)`,
	`MATCH (f:File {id: 'live-6782-chain:file-a'}) MATCH (n:Function {uid: 'live-6782-chain:plain-start'}) CREATE (f)-[:CONTAINS]->(n)`,
	`MATCH (f:File {id: 'live-6782-chain:file-a'}) MATCH (n:Function {uid: 'live-6782-chain:plain-end'}) CREATE (f)-[:CONTAINS]->(n)`,
	`MATCH (f:File {id: 'live-6782-chain:file-b'}) MATCH (n:Function {uid: 'live-6782-chain:bridge'}) CREATE (f)-[:CONTAINS]->(n)`,
	`MATCH (a:Function {uid: 'live-6782-chain:fib'}) CREATE (a)-[:CALLS]->(a)`,
	`MATCH (a:Function {uid: 'live-6782-chain:loop'}) MATCH (b:Function {uid: 'live-6782-chain:bridge'}) CREATE (a)-[:CALLS]->(b)`,
	`MATCH (a:Function {uid: 'live-6782-chain:bridge'}) MATCH (b:Function {uid: 'live-6782-chain:loop'}) CREATE (a)-[:CALLS]->(b)`,
	`MATCH (a:Function {uid: 'live-6782-chain:loop'}) MATCH (b:Function {uid: 'live-6782-chain:mid1'}) CREATE (a)-[:CALLS]->(b)`,
	`MATCH (a:Function {uid: 'live-6782-chain:mid1'}) MATCH (b:Function {uid: 'live-6782-chain:mid2'}) CREATE (a)-[:CALLS]->(b)`,
	`MATCH (a:Function {uid: 'live-6782-chain:mid2'}) MATCH (b:Function {uid: 'live-6782-chain:loop'}) CREATE (a)-[:CALLS]->(b)`,
	`MATCH (a:Function {uid: 'live-6782-chain:plain-start'}) MATCH (b:Function {uid: 'live-6782-chain:plain-end'}) CREATE (a)-[:CALLS]->(b)`,
}

const selfRecursionCleanup = `MATCH (n) WHERE n.uid STARTS WITH 'live-6782-chain:' OR n.id STARTS WITH 'live-6782-chain:' DETACH DELETE n`

func TestLiveCallChainSelfRecursion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	live := openLiveSelfRecursionGraph(ctx, t)
	live.write(ctx, t, selfRecursionCleanup)
	defer live.write(context.Background(), t, selfRecursionCleanup)
	for _, stmt := range selfRecursionSeed {
		live.write(ctx, t, stmt)
	}

	backend := querycontract.GraphBackendNornicDB
	if live.backend == string(graph.SchemaBackendNeo4j) {
		backend = querycontract.GraphBackendNeo4j
	}
	handler := &codequery.CodeHandler{
		Profile:      querycontract.ProfileLocalAuthoritative,
		GraphBackend: backend,
		Neo4j:        live,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	cases := []struct {
		name      string
		body      map[string]any
		wantChain []string
		wantDepth int
	}{
		{
			name:      "self_call",
			body:      map[string]any{"start": "LiveSelfFib", "end": "LiveSelfFib", "repo_id": "live-6782-chain:repo-a", "max_depth": 2},
			wantChain: []string{"LiveSelfFib", "LiveSelfFib"},
			wantDepth: 1,
		},
		{
			name:      "self_cycle_stays_in_repo",
			body:      map[string]any{"start": "LiveSelfLoop", "end": "LiveSelfLoop", "repo_id": "live-6782-chain:repo-a", "max_depth": 5},
			wantChain: []string{"LiveSelfLoop", "LiveSelfMid1", "LiveSelfMid2", "LiveSelfLoop"},
			wantDepth: 3,
		},
		{
			name:      "self_cycle_unbounded_takes_shortest",
			body:      map[string]any{"start": "LiveSelfLoop", "end": "LiveSelfLoop", "max_depth": 5},
			wantChain: []string{"LiveSelfLoop", "LiveSelfBridge", "LiveSelfLoop"},
			wantDepth: 2,
		},
		{
			name:      "plain_chain",
			body:      map[string]any{"start": "LiveSelfPlainStart", "end": "LiveSelfPlainEnd", "repo_id": "live-6782-chain:repo-a", "max_depth": 3},
			wantChain: []string{"LiveSelfPlainStart", "LiveSelfPlainEnd"},
			wantDepth: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, newChainRouteRequest(t, tc.body, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status = %d body=%s", live.backend, rec.Code, rec.Body.String())
			}
			var envelope struct {
				Data struct {
					Chains []struct {
						Chain []struct {
							Name string `json:"name"`
						} `json:"chain"`
						Depth int `json:"depth"`
					} `json:"chains"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode %s: %v", rec.Body.String(), err)
			}
			t.Logf("%s %s: %s", live.backend, tc.name, rec.Body.String())
			// The first chain is the shortest on both routes. The NornicDB route
			// may return further, longer chains (its breadth-first walk keeps up
			// to five), while the Neo4j route returns one shortest path per
			// endpoint pair; that pre-existing cardinality difference is not
			// what this test pins.
			if len(envelope.Data.Chains) == 0 {
				t.Fatalf("%s: no chains: %s", live.backend, rec.Body.String())
			}
			got := make([]string, 0, len(envelope.Data.Chains[0].Chain))
			for _, node := range envelope.Data.Chains[0].Chain {
				got = append(got, node.Name)
			}
			if !reflect.DeepEqual(got, tc.wantChain) || envelope.Data.Chains[0].Depth != tc.wantDepth {
				t.Fatalf("%s: chain = %v depth %d, want %v depth %d",
					live.backend, got, envelope.Data.Chains[0].Depth, tc.wantChain, tc.wantDepth)
			}
		})
	}
}

// liveSelfRecursionGraph is a live Bolt adapter for querycontract.GraphQuery
// and graph.CypherExecutor that retries transient backend errors.
type liveSelfRecursionGraph struct {
	driver   neo4jdriver.DriverWithContext
	database string
	backend  string
}

func openLiveSelfRecursionGraph(ctx context.Context, t *testing.T) *liveSelfRecursionGraph {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	backend := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND"))
	if backend == "" {
		backend = string(graph.SchemaBackendNornicDB)
	}
	if backend != string(graph.SchemaBackendNornicDB) && backend != string(graph.SchemaBackendNeo4j) {
		t.Fatalf("ESHU_LIVE_GRAPH_BACKEND = %q, want nornicdb or neo4j", backend)
	}
	database := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_DATABASE"))
	if database == "" {
		database = "nornic"
		if backend == string(graph.SchemaBackendNeo4j) {
			database = "neo4j"
		}
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}
	live := &liveSelfRecursionGraph{driver: driver, database: database, backend: backend}
	if err := graph.EnsureSchemaWithBackend(ctx, live, slog.New(slog.DiscardHandler), graph.SchemaBackend(backend)); err != nil {
		t.Fatalf("ensure %s schema: %v", backend, err)
	}
	return live
}

// ExecuteCypher satisfies graph.CypherExecutor for schema bootstrap.
func (g *liveSelfRecursionGraph) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	return g.withRetry(ctx, func() error { return g.runWrite(ctx, stmt.Cypher, stmt.Parameters) })
}

func (g *liveSelfRecursionGraph) runWrite(ctx context.Context, cypher string, params map[string]any) error {
	session := g.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: g.database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

// Run satisfies querycontract.GraphQuery.
func (g *liveSelfRecursionGraph) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	var rows []map[string]any
	err := g.withRetry(ctx, func() error {
		session := g.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: g.database})
		defer func() { _ = session.Close(ctx) }()
		result, err := session.Run(ctx, cypher, params)
		if err != nil {
			return err
		}
		records, err := result.Collect(ctx)
		if err != nil {
			return err
		}
		rows = make([]map[string]any, 0, len(records))
		for _, record := range records {
			rows = append(rows, record.AsMap())
		}
		return nil
	})
	return rows, err
}

// RunSingle satisfies querycontract.GraphQuery.
func (g *liveSelfRecursionGraph) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (g *liveSelfRecursionGraph) write(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	if err := g.withRetry(ctx, func() error { return g.runWrite(ctx, cypher, nil) }); err != nil {
		t.Fatalf("write %q: %v", cypher, err)
	}
}

// withRetry retries transient backend errors a bounded number of times.
func (g *liveSelfRecursionGraph) withRetry(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if err = fn(); err == nil || !neo4jdriver.IsRetryable(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		case <-time.After(time.Duration(attempt+1) * 200 * time.Millisecond):
		}
	}
	return fmt.Errorf("after 5 attempts: %w", err)
}
