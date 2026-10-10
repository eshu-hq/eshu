// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live proof of the gate's census check on a real Neo4j (#7212): the gate's own
// Bolt reader (boltGraphCounter.AnchorCensus and ResidualLabelSets) runs the
// census Cypher over a graph with Eshu's production schema applied, passes on
// canonical shapes, and fails on a planted id-only node and a planted null-uid
// node, naming the label sets and no id. The check assumes an empty database
// (the live runner gives each ledger file a fresh one). Run it with, for example:
//
//	docker compose -p eshu-7212-live -f docker-compose.live-backend-neo4j.yml up -d --wait
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:7687 ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  ESHU_LIVE_GRAPH_DATABASE=neo4j go test ./cmd/golden-corpus-gate \
//	  -tags live_nornicdb_answer_truth -run TestLiveAnchorCensusCheck -count=1 -v
package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

func TestLiveAnchorCensusCheck(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	if strings.ToLower(strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND"))) != string(graph.SchemaBackendNeo4j) {
		t.Skip("the anchor census check runs on the Neo4j leg only (#7212)")
	}
	db := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_DATABASE"))
	if db == "" {
		db = "neo4j"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := graph.EnsureSchemaWithBackend(ctx, liveSchemaExecutor{driver: driver, db: db}, slog.New(slog.DiscardHandler), graph.SchemaBackendNeo4j); err != nil {
		t.Fatalf("ensure neo4j schema: %v", err)
	}
	write := func(cypher string) {
		t.Helper()
		if _, err := neo4j.ExecuteQuery(ctx, driver, cypher, nil,
			neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(db)); err != nil {
			t.Fatalf("write %q: %v", cypher, err)
		}
	}
	const prefix = "anchor-census-check-live:"
	cleanup := func() {
		write(`MATCH (n) WHERE n.id STARTS WITH '` + prefix + `' OR n.uid STARTS WITH '` + prefix + `' DETACH DELETE n`)
	}
	cleanup()
	defer cleanup()
	counter := &boltGraphCounter{driver: driver, db: db}

	write(`CREATE (:Function {id: '` + prefix + `fn', uid: '` + prefix + `fn'})`)
	write(`CREATE (:Repository {id: '` + prefix + `repo'})`)
	var clean Report
	checkAnchorCensus(ctx, counter, true, &clean)
	f := censusFinding(t, clean)
	t.Logf("clean seed: ok=%t required=%t detail=%q", f.OK, f.Required, f.Detail)
	if !f.OK || !f.Required {
		t.Fatalf("canonical shapes failed the census check: %+v", f)
	}

	write(`CREATE (:Unconstrained {id: '` + prefix + `planted'})`)
	write(`CREATE (:Function {id: '` + prefix + `null-uid'})`)
	var planted Report
	checkAnchorCensus(ctx, counter, true, &planted)
	f = censusFinding(t, planted)
	t.Logf("planted: ok=%t required=%t detail=%q", f.OK, f.Required, f.Detail)
	if f.OK || !f.Required {
		t.Fatalf("planted unreachable nodes passed the census check: %+v", f)
	}
	for _, want := range []string{"residual 2", "labels=[Unconstrained] nodes=1", "labels=[Function] nodes=1"} {
		if !strings.Contains(f.Detail, want) {
			t.Errorf("detail lacks %q: %s", want, f.Detail)
		}
	}
	if strings.Contains(f.Detail, prefix) {
		t.Errorf("detail leaks an entity id: %s", f.Detail)
	}
}
