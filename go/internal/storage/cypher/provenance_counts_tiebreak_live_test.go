// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live backend-parity proof for #7711: the provenance count reads deliver
// tied groups in a deterministic, backend-identical order.
//
// The per-verb source_tool counts and the File language counts order by the
// group count alone, so tied groups deliver in backend-undefined order and
// flip the order-sensitive backend-diff digest while the counted multiset
// agrees (24/24, 18/18, 126/126 rows). The group-key tiebreaker added here
// pins delivery order; this test seeds tied groups, runs the production
// statement text, and asserts the exact row sequence on each backend leg.
// Both legs green with identical expectations is the cross-backend proof.
//
// Run against an isolated container per backend:
//
//	docker run -d --name eshu-live-nornic -p 127.0.0.1:27940:7687 \
//	  -e NORNICDB_NO_AUTH=true -e NORNICDB_EMBEDDING_ENABLED=false \
//	  ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1
//	docker run -d --name eshu-live-neo4j -p 127.0.0.1:27950:7687 -e NEO4J_AUTH=none \
//	  neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27940 go test ./internal/storage/cypher \
//	  -tags live_nornicdb_answer_truth -run TestLiveProvenanceCountsTiebreakIsDeterministic -count=1 -v
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27950 ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  go test ./internal/storage/cypher -tags live_nornicdb_answer_truth \
//	  -run TestLiveProvenanceCountsTiebreakIsDeterministic -count=1 -v
//
// ESHU_LIVE_GRAPH_BACKEND is nornicdb (default) or neo4j. ESHU_LIVE_GRAPH_DATABASE
// defaults to "nornic" for NornicDB and "neo4j" for Neo4j.
package cypher_test

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const liveTiebreakPrefix = "live-7711-tiebreak:"

func TestLiveProvenanceCountsTiebreakIsDeterministic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	live := openLiveTiebreakGraph(ctx, t)

	cleanupAnchor := `MATCH (n:ZZTiebreak7711) DETACH DELETE n`
	cleanupFiles := `MATCH (n:File) WHERE n.id STARTS WITH '` + liveTiebreakPrefix + `' DELETE n`
	live.write(ctx, t, cleanupAnchor, nil)
	live.write(ctx, t, cleanupFiles, nil)
	defer live.write(context.Background(), t, cleanupAnchor, nil)
	defer live.write(context.Background(), t, cleanupFiles, nil)

	live.write(ctx, t, `CREATE (:ZZTiebreak7711 {id: 'a'})`, nil)
	live.write(ctx, t, `CREATE (:ZZTiebreak7711 {id: 'b'})`, nil)
	edge := func(verb, tool string, n int) {
		for i := 0; i < n; i++ {
			live.write(ctx, t, `MATCH (a:ZZTiebreak7711 {id: 'a'}), (b:ZZTiebreak7711 {id: 'b'}) CREATE (a)-[:`+verb+` {source_tool: '`+tool+`'}]->(b)`, nil)
		}
	}
	// Tied at 3 (alpha sorts before bravo), single charlie below.
	edge("DEPENDS_ON", "bravo", 3)
	edge("DEPENDS_ON", "alpha", 3)
	edge("DEPENDS_ON", "charlie", 1)
	// Tied at 2 (xray sorts before yankee).
	edge("DEPLOYS_FROM", "yankee", 2)
	edge("DEPLOYS_FROM", "xray", 2)
	// Tied at 2 (go sorts before py), single js below.
	file := func(id, lang string) {
		live.write(ctx, t, `CREATE (:File {id: '`+liveTiebreakPrefix+id+`', language: '`+lang+`'})`, nil)
	}
	file("f1", "py")
	file("f2", "go")
	file("f3", "py")
	file("f4", "go")
	file("f5", "js")

	rec := &recordingTiebreakReader{live: live}
	store := sourcecypher.NewProvenanceCountStore(rec)
	edgeCounts, err := store.EdgesBySourceTool(ctx)
	if err != nil {
		t.Fatalf("EdgesBySourceTool: %v", err)
	}
	fileCounts, err := store.FilesByLanguage(ctx)
	if err != nil {
		t.Fatalf("FilesByLanguage: %v", err)
	}
	if edgeCounts["alpha"] != 3 || edgeCounts["bravo"] != 3 || edgeCounts["charlie"] != 1 {
		t.Fatalf("edge counts = %v on %s, want alpha/bravo 3 and charlie 1", edgeCounts, live.backend)
	}
	if edgeCounts["xray"] != 2 || edgeCounts["yankee"] != 2 {
		t.Fatalf("edge counts = %v on %s, want xray/yankee 2", edgeCounts, live.backend)
	}
	if fileCounts["go"] != 2 || fileCounts["py"] != 2 || fileCounts["js"] != 1 {
		t.Fatalf("file counts = %v on %s, want go/py 2 and js 1", fileCounts, live.backend)
	}

	// The store folds rows into maps, so delivery order is asserted by
	// re-running the recorded production statements and comparing the
	// exact row sequence: cnt DESC with the group-key tiebreaker.
	dependsRows := live.runRead(ctx, t, rec.queryFor(t, "DEPENDS_ON"), map[string]any{"limit": 10000})
	assertTiebreakSequence(t, live.backend, "DEPENDS_ON", dependsRows, "source_tool",
		[2]string{"alpha", "3"}, [2]string{"bravo", "3"}, [2]string{"charlie", "1"})
	deploysRows := live.runRead(ctx, t, rec.queryFor(t, "DEPLOYS_FROM"), map[string]any{"limit": 10000})
	assertTiebreakSequence(t, live.backend, "DEPLOYS_FROM", deploysRows, "source_tool",
		[2]string{"xray", "2"}, [2]string{"yankee", "2"})
	fileRows := live.runRead(ctx, t, rec.fileQuery(t), map[string]any{"limit": 10000})
	assertTiebreakSequence(t, live.backend, "File", fileRows, "language",
		[2]string{"go", "2"}, [2]string{"py", "2"}, [2]string{"js", "1"})
}

// assertTiebreakSequence requires the exact delivered row sequence: key
// order and count per row, in order.
func assertTiebreakSequence(t *testing.T, backend, what string, rows []map[string]any, keyCol string, want ...[2]string) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("%s rows on %s = %d, want %d (%v)", what, backend, len(rows), len(want), rows)
	}
	for i, w := range want {
		gotKey, _ := rows[i][keyCol].(string)
		gotCnt := tiebreakInt(rows[i]["cnt"])
		if gotKey != w[0] || gotCnt != w[1] {
			t.Fatalf("%s row %d on %s = %s/%s, want %s/%s (rows=%v)", what, i, backend, gotKey, gotCnt, w[0], w[1], rows)
		}
	}
}

func tiebreakInt(v any) string {
	switch n := v.(type) {
	case int64:
		return strconv.FormatInt(n, 10)
	case int:
		return strconv.Itoa(n)
	case float64:
		return strconv.FormatInt(int64(n), 10)
	default:
		return "?"
	}
}

// recordingTiebreakReader records the production statements the store runs
// while delegating execution to the live backend.
type recordingTiebreakReader struct {
	live    *liveTiebreakGraph
	queries []string
}

func (r *recordingTiebreakReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	r.queries = append(r.queries, cypher)
	return r.live.runReadResult(ctx, cypher, params)
}

// queryFor returns the recorded per-verb edge statement for verb.
func (r *recordingTiebreakReader) queryFor(t *testing.T, verb string) string {
	t.Helper()
	for _, q := range r.queries {
		if strings.Contains(q, "[r:"+verb+"]") {
			return q
		}
	}
	t.Fatalf("no recorded query for verb %s (%d queries)", verb, len(r.queries))
	return ""
}

// fileQuery returns the recorded File language statement.
func (r *recordingTiebreakReader) fileQuery(t *testing.T) string {
	t.Helper()
	for _, q := range r.queries {
		if strings.Contains(q, "MATCH (f:File)") {
			return q
		}
	}
	t.Fatalf("no recorded File query (%d queries)", len(r.queries))
	return ""
}

// liveTiebreakGraph is a minimal live Bolt session adapter for the seeded
// fixture: raw writes plus raw reads.
type liveTiebreakGraph struct {
	driver   neo4jdriver.DriverWithContext
	database string
	backend  string
}

func openLiveTiebreakGraph(ctx context.Context, t *testing.T) *liveTiebreakGraph {
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
	return &liveTiebreakGraph{driver: driver, database: database, backend: backend}
}

func (g *liveTiebreakGraph) write(ctx context.Context, t *testing.T, cypher string, params map[string]any) {
	t.Helper()
	session := g.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: g.database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("write %.60q: %v", cypher, err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume %.60q: %v", cypher, err)
	}
}

func (g *liveTiebreakGraph) runRead(ctx context.Context, t *testing.T, cypher string, params map[string]any) []map[string]any {
	t.Helper()
	rows, err := g.runReadResult(ctx, cypher, params)
	if err != nil {
		t.Fatalf("read %.60q: %v", cypher, err)
	}
	return rows
}

func (g *liveTiebreakGraph) runReadResult(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
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
