// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live backend-parity proof for #6782: a repo-dependency edge written without
// a source_tool stays unstamped on BOTH graph backends.
//
// The B-7 golden corpus on Neo4j failed mcp:get_repo_context because
// orders-api had no source_tool_breakdown there while it had one on NornicDB.
// orders-api's only verb-typed outgoing edge is the package-consumption
// DEPENDS_ON to lib-common, which carries no source_tool. The batched writer
// SETs `rel.source_tool = row.source_tool`; when the row map omitted the key,
// the pinned NornicDB v1.3.3 stored the literal text "row.source_tool" and the
// breakdown reported it, while Neo4j correctly left the edge unstamped. This
// test drives the production EdgeWriter and the production breakdown read.
//
// Run against an isolated container per backend:
//
//	docker run -d --name eshu-live-nornic -p 127.0.0.1:27940:7687 \
//	  -e NORNICDB_NO_AUTH=true -e NORNICDB_EMBEDDING_ENABLED=false \
//	  ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-6915-f2163176@sha256:a41fa912b0ac85aa8383d3095237347201fa66bc5c8ab644ce869a6c799c44be
//	docker run -d --name eshu-live-neo4j -p 127.0.0.1:27950:7687 -e NEO4J_AUTH=none \
//	  neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27940 go test ./internal/storage/cypher \
//	  -tags live_nornicdb_answer_truth -run TestLiveRepoDependencyWithoutSourceTool -count=1 -v
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27950 ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  go test ./internal/storage/cypher -tags live_nornicdb_answer_truth \
//	  -run TestLiveRepoDependencyWithoutSourceTool -count=1 -v
//
// ESHU_LIVE_GRAPH_BACKEND is nornicdb (default) or neo4j. ESHU_LIVE_GRAPH_DATABASE
// defaults to "nornic" for NornicDB and "neo4j" for Neo4j.
package cypher_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	edgewriter "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/writer"
)

const (
	liveSourceToolRepo   = "live-6782-source-tool:repo-consumer"
	liveSourceToolTarget = "live-6782-source-tool:repo-library"
	liveSourceToolPeer   = "live-6782-source-tool:repo-peer"
)

func TestLiveRepoDependencyWithoutSourceToolStaysUnstamped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	live := openLiveSourceToolGraph(ctx, t)

	cleanup := `MATCH (n) WHERE n.id STARTS WITH 'live-6782-source-tool:' DETACH DELETE n`
	live.write(ctx, t, cleanup, nil)
	defer live.write(context.Background(), t, cleanup, nil)
	for _, id := range []string{liveSourceToolRepo, liveSourceToolTarget, liveSourceToolPeer} {
		live.write(ctx, t, `CREATE (:Repository {id: '`+id+`', name: '`+id+`'})`, nil)
	}

	rows := []reducer.SharedProjectionIntentRow{
		{
			// Shaped like the package-consumption intent: no source_tool.
			IntentID:     "live-6782-package-consumption",
			RepositoryID: liveSourceToolRepo,
			GenerationID: "gen-1",
			Payload: map[string]any{
				"repo_id":           liveSourceToolRepo,
				"target_repo_id":    liveSourceToolTarget,
				"relationship_type": "DEPENDS_ON",
				"evidence_type":     "package_consumption",
			},
		},
		{
			// A genuinely stamped edge, so the breakdown is not vacuous.
			IntentID:     "live-6782-helm",
			RepositoryID: liveSourceToolRepo,
			GenerationID: "gen-1",
			Payload: map[string]any{
				"repo_id":           liveSourceToolRepo,
				"target_repo_id":    liveSourceToolPeer,
				"relationship_type": "DEPLOYS_FROM",
				"evidence_type":     "helm_chart_reference",
				"source_tool":       "helm",
			},
		},
	}
	writer := edgewriter.NewEdgeWriter(live, 0)
	if _, err := writer.WriteEdges(ctx, reducer.DomainRepoDependency, rows, "resolver/cross-repo"); err != nil {
		t.Fatalf("WriteEdges: %v", err)
	}

	edge, err := live.RunSingle(ctx, `
		MATCH (:Repository {id: $repo})-[rel:DEPENDS_ON]->(:Repository {id: $target})
		RETURN rel.source_tool AS source_tool, rel.source_tool IS NOT NULL AS stamped`,
		map[string]any{"repo": liveSourceToolRepo, "target": liveSourceToolTarget})
	if err != nil || edge == nil {
		t.Fatalf("read DEPENDS_ON edge: row=%v err=%v", edge, err)
	}
	t.Logf("%s DEPENDS_ON edge: source_tool=%#v stamped=%v", live.backend, edge["source_tool"], edge["stamped"])
	if stamped, _ := edge["stamped"].(bool); stamped {
		t.Fatalf("DEPENDS_ON written without a source_tool is stamped %#v on %s; the provenance contract leaves it unstamped",
			edge["source_tool"], live.backend)
	}

	breakdown, _ := repository.QueryRepoSourceToolBreakdown(ctx, live, map[string]any{"repo_id": liveSourceToolRepo})
	t.Logf("%s source_tool breakdown: %v", live.backend, breakdown)
	if len(breakdown) != 1 || breakdown[0]["source_tool"] != "helm" || breakdown[0]["edge_count"] != 1 {
		t.Fatalf("breakdown = %v on %s, want exactly [{source_tool: helm, edge_count: 1}]", breakdown, live.backend)
	}
}

// liveSourceToolGraph is a live Bolt session adapter that satisfies both the
// writer's Executor/GroupExecutor and the query layer's GraphQuery, and
// retries transient backend errors so a busy container does not flake the
// proof.
type liveSourceToolGraph struct {
	driver   neo4jdriver.DriverWithContext
	database string
	backend  string
}

func openLiveSourceToolGraph(ctx context.Context, t *testing.T) *liveSourceToolGraph {
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
	live := &liveSourceToolGraph{driver: driver, database: database, backend: backend}
	if err := graph.EnsureSchemaWithBackend(ctx, live, slog.New(slog.DiscardHandler), graph.SchemaBackend(backend)); err != nil {
		t.Fatalf("ensure %s schema: %v", backend, err)
	}
	return live
}

// ExecuteCypher satisfies graph.CypherExecutor for schema bootstrap.
func (g *liveSourceToolGraph) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	return g.withRetry(ctx, func() error { return g.runWrite(ctx, []string{stmt.Cypher}, []map[string]any{stmt.Parameters}) })
}

// Execute satisfies sourcecypher.Executor with one auto-retried transaction.
func (g *liveSourceToolGraph) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	return g.ExecuteGroup(ctx, []sourcecypher.Statement{stmt})
}

// ExecuteGroup satisfies sourcecypher.GroupExecutor: one managed write
// transaction, the same shape the reducer's grouped executor uses.
func (g *liveSourceToolGraph) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	cyphers := make([]string, 0, len(stmts))
	params := make([]map[string]any, 0, len(stmts))
	for _, stmt := range stmts {
		cyphers = append(cyphers, stmt.Cypher)
		params = append(params, stmt.Parameters)
	}
	return g.withRetry(ctx, func() error { return g.runWrite(ctx, cyphers, params) })
}

func (g *liveSourceToolGraph) runWrite(ctx context.Context, cyphers []string, params []map[string]any) error {
	session := g.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: g.database})
	defer func() { _ = session.Close(ctx) }()
	_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		for i, cypher := range cyphers {
			result, err := tx.Run(ctx, cypher, params[i])
			if err != nil {
				return nil, err
			}
			if _, err := result.Consume(ctx); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	return err
}

// Run satisfies querycontract.GraphQuery.
func (g *liveSourceToolGraph) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
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
func (g *liveSourceToolGraph) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (g *liveSourceToolGraph) write(ctx context.Context, t *testing.T, cypher string, params map[string]any) {
	t.Helper()
	if err := g.withRetry(ctx, func() error { return g.runWrite(ctx, []string{cypher}, []map[string]any{params}) }); err != nil {
		t.Fatalf("write %q: %v", cypher, err)
	}
}

// withRetry retries transient backend errors a bounded number of times.
func (g *liveSourceToolGraph) withRetry(ctx context.Context, fn func() error) error {
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
