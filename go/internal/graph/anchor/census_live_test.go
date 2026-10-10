// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live proof of the census Cypher against a real Neo4j (#7212): the statement
// must count exactly what the reference classification counts, including the
// null-uid trap, and a planted id-only node on an unconstrained label must
// raise the residual by one. Run it with, for example:
//
//	docker compose -p eshu-7212-live -f docker-compose.live-backend-neo4j.yml up -d --wait
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:7687 ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  ESHU_LIVE_GRAPH_DATABASE=neo4j go test ./internal/graph/anchor \
//	  -tags live_nornicdb_answer_truth -run TestLiveAnchorCensus -count=1 -v
//
// Without ESHU_LIVE_GRAPH_BACKEND=neo4j the test skips, which reads as ok.
//
// The measure is a delta around the seed, so other tests' nodes in a shared
// database do not move it. Run with -p 1 beside other live packages.
package anchor

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const liveCensusPrefix = "anchor-census-live:"

type liveCensusSource struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (s liveCensusSource) AnchorCensus(ctx context.Context) (Census, error) {
	result, err := neo4jdriver.ExecuteQuery(ctx, s.driver, CensusCypher, CensusParameters(),
		neo4jdriver.EagerResultTransformer, neo4jdriver.ExecuteQueryWithDatabase(s.database))
	if err != nil {
		return Census{}, err
	}
	return ParseCensusRow(result.Records[0].AsMap())
}

func TestLiveAnchorCensus(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	if backend := strings.ToLower(strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND"))); backend != "neo4j" {
		t.Skip("the id-anchor census is a Neo4j statement; NornicDB keeps the unlabeled fallback (#7212)")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_DATABASE"))
	if database == "" {
		database = "neo4j"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	source := liveCensusSource{driver: driver, database: database}
	write := func(cypher string) {
		t.Helper()
		if _, err := neo4jdriver.ExecuteQuery(ctx, driver, cypher, nil,
			neo4jdriver.EagerResultTransformer, neo4jdriver.ExecuteQueryWithDatabase(database)); err != nil {
			t.Fatalf("write %q: %v", cypher, err)
		}
	}
	cleanup := func() {
		write(`MATCH (n) WHERE n.id STARTS WITH '` + liveCensusPrefix + `' OR n.uid STARTS WITH '` + liveCensusPrefix + `' DETACH DELETE n`)
	}
	cleanup()
	defer cleanup()

	before, err := source.AnchorCensus(ctx)
	if err != nil {
		t.Fatalf("census before seed: %v", err)
	}
	// Canonical shapes: uid == id on a uid label, an id label with no uid, a
	// File with a uid and no id, and a multi-label node with one covered label.
	for _, seed := range []string{
		`CREATE (:Function {id: '` + liveCensusPrefix + `fn', uid: '` + liveCensusPrefix + `fn'})`,
		`CREATE (:Repository {id: '` + liveCensusPrefix + `repo'})`,
		`CREATE (:File {uid: '` + liveCensusPrefix + `file'})`,
		`CREATE (:Unconstrained:Function {id: '` + liveCensusPrefix + `multi', uid: '` + liveCensusPrefix + `multi'})`,
	} {
		write(seed)
	}
	clean, err := source.AnchorCensus(ctx)
	if err != nil {
		t.Fatalf("census after clean seed: %v", err)
	}
	if got := clean.Residual - before.Residual; got != 0 {
		t.Fatalf("residual moved by %d on canonical seeds, want 0", got)
	}
	if got := clean.ViaUIDOnly - before.ViaUIDOnly; got != 2 {
		t.Fatalf("via-uid moved by %d, want 2 (Function and the multi-label node)", got)
	}
	if got := clean.ViaID - before.ViaID; got != 1 {
		t.Fatalf("via-id moved by %d, want 1 (Repository)", got)
	}

	// The shapes the census must catch: an id-only node on an unconstrained
	// label, and a uid-labeled node with a null uid (the null-comparison trap).
	write(`CREATE (:Unconstrained {id: '` + liveCensusPrefix + `planted'})`)
	write(`CREATE (:Function {id: '` + liveCensusPrefix + `null-uid'})`)
	planted, err := source.AnchorCensus(ctx)
	if err != nil {
		t.Fatalf("census after planting: %v", err)
	}
	if got := planted.Residual - clean.Residual; got != 2 {
		t.Fatalf("residual moved by %d after planting two unreachable nodes, want 2", got)
	}
	if verdict := EvaluateCensus(ctx, source); verdict.OK {
		t.Fatalf("EvaluateCensus passed with planted nodes: %s", verdict.Detail)
	}

	// The Cypher must agree with the reference classification on every seeded
	// shape: read the seeded nodes back and classify them in Go.
	uid, id := map[string]bool{}, map[string]bool{}
	for _, l := range UIDLabels() {
		uid[l] = true
	}
	for _, l := range IDLabels() {
		id[l] = true
	}
	result, err := neo4jdriver.ExecuteQuery(ctx, driver,
		`MATCH (n) WHERE n.id STARTS WITH $p OR n.uid STARTS WITH $p RETURN labels(n) AS labels, n.id AS id, n.uid AS uid`,
		map[string]any{"p": liveCensusPrefix}, neo4jdriver.EagerResultTransformer, neo4jdriver.ExecuteQueryWithDatabase(database))
	if err != nil {
		t.Fatalf("read seeded nodes: %v", err)
	}
	var want Census
	for _, rec := range result.Records {
		node := Node{}
		rawLabels, _ := rec.Get("labels")
		for _, l := range rawLabels.([]any) {
			node.Labels = append(node.Labels, l.(string))
		}
		if v, _ := rec.Get("id"); v != nil {
			s := v.(string)
			node.ID = &s
		}
		if v, _ := rec.Get("uid"); v != nil {
			s := v.(string)
			node.UID = &s
		}
		switch Classify(node, uid, id) {
		case ReachViaID:
			want.IDBearing++
			want.ViaID++
		case ReachViaUID:
			want.IDBearing++
			want.ViaUIDOnly++
		case Unreachable:
			want.IDBearing++
			want.Residual++
		}
	}
	got := Census{
		IDBearing:  planted.IDBearing - before.IDBearing,
		ViaID:      planted.ViaID - before.ViaID,
		ViaUIDOnly: planted.ViaUIDOnly - before.ViaUIDOnly,
		Residual:   planted.Residual - before.Residual,
	}
	if got != want {
		t.Fatalf("census delta = %+v, reference classification of the seeded nodes = %+v", got, want)
	}
}
