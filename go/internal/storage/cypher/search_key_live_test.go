// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live proof for #7245 PR-1 on the pinned Neo4j: real writers store the
// infra search key under the 8,000 code-point cap and set
// infra_search_ovf = 'T' with no key past it, so a 40 KB name and a 70 KB
// config_path are written, the infra_search_key_ft FULLTEXT index stays
// ONLINE, and the overflowed nodes are found through infra_search_ovf:T.
// NornicDB cannot evaluate the clause, so this file is Neo4j-only.
//
//	docker run -d --name eshu-live-neo4j -p 127.0.0.1:27950:7687 -e NEO4J_AUTH=none \
//	  neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27950 ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  go test ./internal/storage/cypher -tags live_nornicdb_answer_truth \
//	  -run TestLiveInfraSearchKey -count=1 -v
//
// TestLiveInfraSearchKeyBackfillScale also runs when
// ESHU_INFRA_SEARCH_KEY_SCALE=1; it seeds the 181,929-node infra label shape
// measured in the QA environment and records timings only (never asserted,
// the host is shared).
package cypher

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
)

const liveSearchKeyPrefix = "zz7245:"

// liveSearchKeyGraph is a Bolt session adapter: the sourcecypher.Executor the
// real writers run through, and the search.Runner the backfill uses.
type liveSearchKeyGraph struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func openLiveSearchKeyGraph(ctx context.Context, t *testing.T) *liveSearchKeyGraph {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	if backend := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")); backend != string(graph.SchemaBackendNeo4j) {
		t.Skipf("ESHU_LIVE_GRAPH_BACKEND = %q; the infra search key is Neo4j-only", backend)
	}
	database := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_DATABASE"))
	if database == "" {
		database = "neo4j"
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}
	return &liveSearchKeyGraph{driver: driver, database: database}
}

func (g *liveSearchKeyGraph) Execute(ctx context.Context, stmt Statement) error {
	s := g.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: g.database})
	defer func() { _ = s.Close(ctx) }()
	_, err := s.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, stmt.Cypher, stmt.Parameters)
		if err != nil {
			return nil, err
		}
		return res.Consume(ctx)
	})
	return err
}

// ExecuteGroup runs the statements in one write transaction, the atomic
// group shape the Neo4j production executors use.
func (g *liveSearchKeyGraph) ExecuteGroup(ctx context.Context, stmts []Statement) error {
	s := g.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: g.database})
	defer func() { _ = s.Close(ctx) }()
	_, err := s.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		for _, stmt := range stmts {
			res, err := tx.Run(ctx, stmt.Cypher, stmt.Parameters)
			if err != nil {
				return nil, err
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	return err
}

func (g *liveSearchKeyGraph) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	s := g.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: g.database})
	defer func() { _ = s.Close(ctx) }()
	res, err := s.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	records, err := res.Collect(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(records))
	for _, r := range records {
		rows = append(rows, r.AsMap())
	}
	return rows, nil
}

func (g *liveSearchKeyGraph) must(ctx context.Context, t *testing.T, cypher string, params map[string]any) []map[string]any {
	t.Helper()
	rows, err := g.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("%v\ncypher=%s", err, cypher)
	}
	return rows
}

// ensureIndex applies the production DDL statement for the index and waits.
func (g *liveSearchKeyGraph) ensureIndex(ctx context.Context, t *testing.T) {
	t.Helper()
	stmts, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range stmts {
		if strings.Contains(stmt, graph.InfraSearchKeyIndexName) {
			g.must(ctx, t, stmt, nil)
		}
	}
	g.must(ctx, t, "CALL db.awaitIndexes(300)", nil)
}

func (g *liveSearchKeyGraph) indexState(ctx context.Context, t *testing.T) string {
	t.Helper()
	rows := g.must(ctx, t, "SHOW FULLTEXT INDEXES YIELD name, state WHERE name = $name RETURN state", map[string]any{"name": graph.InfraSearchKeyIndexName})
	if len(rows) != 1 {
		t.Fatalf("index %s rows = %v", graph.InfraSearchKeyIndexName, rows)
	}
	state, _ := rows[0]["state"].(string)
	return state
}

func (g *liveSearchKeyGraph) cleanup(ctx context.Context, t *testing.T) {
	t.Helper()
	g.must(ctx, t, "MATCH (n) WHERE n.uid STARTS WITH $p OR n.path STARTS WITH $path DETACH DELETE n",
		map[string]any{"p": liveSearchKeyPrefix, "path": "/zz7245"})
}

type liveKeyState struct {
	ovf    string
	keyLen int64
	hasKey bool
}

func (g *liveSearchKeyGraph) keyState(ctx context.Context, t *testing.T, uid string) liveKeyState {
	t.Helper()
	rows := g.must(ctx, t, "MATCH (n {uid: $uid}) RETURN n.infra_search_ovf AS ovf, size(n.infra_search_key) AS len, n.infra_search_key IS NOT NULL AS has", map[string]any{"uid": uid})
	if len(rows) != 1 {
		t.Fatalf("node %s rows = %v, want 1 (the write must succeed)", uid, rows)
	}
	st := liveKeyState{}
	st.ovf, _ = rows[0]["ovf"].(string)
	st.keyLen, _ = rows[0]["len"].(int64)
	st.hasKey, _ = rows[0]["has"].(bool)
	return st
}

// TestLiveInfraSearchKey writes through the production writers on Neo4j and
// proves the cap, the overflow flag, the index state, and the backfill.
func TestLiveInfraSearchKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	g := openLiveSearchKeyGraph(ctx, t)
	g.ensureIndex(ctx, t)
	g.cleanup(ctx, t)
	defer g.cleanup(context.Background(), t)

	exec := &InstrumentedExecutor{Inner: g}
	bigName := liveSearchKeyPrefix + "big-name"
	normal := liveSearchKeyPrefix + "normal"
	at8000 := liveSearchKeyPrefix + "emoji-8000"
	at8001 := liveSearchKeyPrefix + "emoji-8001"
	// The CloudResource writer sets id = uid and ten empty fields, so the key
	// is name + uid + ten separators. Size the emoji names to land exactly on
	// 8,000 and 8,001 code points (about 32,000 UTF-8 bytes).
	emojiName := func(uid string, total int) string { return strings.Repeat("😀", total-10-len([]rune(uid))) }
	rows := []map[string]any{
		{"uid": bigName, "name": strings.Repeat("x", 40000)},
		{"uid": normal, "name": "zz7245-alpha-bucket", "resource_type": "aws_s3_bucket", "region": "us-east-1"},
		{"uid": at8000, "name": emojiName(at8000, 8000)},
		{"uid": at8001, "name": emojiName(at8001, 8001)},
	}
	if err := NewCloudResourceNodeWriterWithDialect(exec, 0, search.On).WriteCloudResourceNodes(ctx, rows, "reducer/aws"); err != nil {
		t.Fatalf("WriteCloudResourceNodes() error = %v (a capped key must never fail the write)", err)
	}

	configPath := liveSearchKeyPrefix + "tfvar-config"
	mat := canonical.CanonicalMaterialization{
		ScopeID: "zz7245-scope", GenerationID: "zz7245-gen", RepoID: liveSearchKeyPrefix + "repo",
		Repository: &canonical.RepositoryRow{RepoID: liveSearchKeyPrefix + "repo", Name: "zz7245", Path: "/zz7245/repo"},
		Files:      []canonical.FileRow{{Path: "/zz7245/repo/main.tf", RelativePath: "main.tf", Name: "main.tf", Language: "hcl", RepoID: liveSearchKeyPrefix + "repo", DirPath: "/zz7245/repo"}},
		Entities: []canonical.EntityRow{{
			EntityID: configPath, Label: "TerraformVariable", EntityName: "region", FilePath: "/zz7245/repo/main.tf",
			RelativePath: "main.tf", StartLine: 1, EndLine: 3, Language: "hcl", RepoID: liveSearchKeyPrefix + "repo",
			Metadata: map[string]any{"config_path": strings.Repeat("c", 70000)},
		}},
	}
	if err := NewCanonicalNodeWriterWithDialect(exec, 0, nil, search.On).Write(ctx, mat); err != nil {
		t.Fatalf("canonical Write() error = %v (a capped key must never fail the write)", err)
	}

	for uid, want := range map[string]liveKeyState{
		bigName:    {ovf: graph.InfraSearchOverflowYes},
		configPath: {ovf: graph.InfraSearchOverflowYes},
		at8001:     {ovf: graph.InfraSearchOverflowYes},
		at8000:     {ovf: graph.InfraSearchOverflowNo, hasKey: true, keyLen: 8000},
	} {
		if got := g.keyState(ctx, t, uid); got != want {
			t.Errorf("%s key state = %+v, want %+v", uid, got, want)
		}
	}
	if got := g.keyState(ctx, t, normal); got.ovf != graph.InfraSearchOverflowNo || !got.hasKey {
		t.Errorf("%s key state = %+v, want a stored key and flag F", normal, got)
	}

	g.must(ctx, t, "CALL db.awaitIndexes(300)", nil)
	if state := g.indexState(ctx, t); state != "ONLINE" {
		t.Fatalf("%s state = %s after the writes, want ONLINE", graph.InfraSearchKeyIndexName, state)
	}
	g.must(ctx, t, "RETURN 1 AS up", nil)

	found := func(query string) map[string]bool {
		rows := g.must(ctx, t, "CALL db.index.fulltext.queryNodes($index, $q) YIELD node WHERE node.uid STARTS WITH $p RETURN node.uid AS uid",
			map[string]any{"index": graph.InfraSearchKeyIndexName, "q": query, "p": liveSearchKeyPrefix})
		out := map[string]bool{}
		for _, r := range rows {
			uid, _ := r["uid"].(string)
			out[uid] = true
		}
		return out
	}
	overflowed := found("infra_search_ovf:T")
	for _, uid := range []string{bigName, configPath, at8001} {
		if !overflowed[uid] {
			t.Errorf("infra_search_ovf:T did not find %s (got %v)", uid, overflowed)
		}
	}
	if overflowed[normal] || overflowed[at8000] {
		t.Errorf("infra_search_ovf:T found a keyed node: %v", overflowed)
	}
	if hits := found("infra_search_key:*alpha*"); !hits[normal] {
		t.Errorf("infra_search_key:*alpha* did not find %s (got %v)", normal, hits)
	}

	// Backfill repairs a node whose key a non-keying writer dropped, writes
	// nothing on a second run, and the readiness probe stays at zero.
	g.must(ctx, t, "MATCH (n:CloudResource {uid: $uid}) REMOVE n.infra_search_key, n.infra_search_ovf SET n.region = 'eu-west-1'", map[string]any{"uid": normal})
	unkeyed, err := search.CountUnkeyed(ctx, g)
	if err != nil || unkeyed["CloudResource"] < 1 {
		t.Fatalf("CountUnkeyed() = %v, %v; want CloudResource >= 1", unkeyed, err)
	}
	if _, err := search.Backfill(ctx, g, search.BackfillOptions{BatchSize: 2}); err != nil {
		t.Fatalf("Backfill() error = %v", err)
	}
	rows2 := g.must(ctx, t, "MATCH (n:CloudResource {uid: $uid}) RETURN n.infra_search_key CONTAINS 'eu-west-1' AS fresh, n.infra_search_ovf AS ovf", map[string]any{"uid": normal})
	if fresh, _ := rows2[0]["fresh"].(bool); !fresh || rows2[0]["ovf"] != graph.InfraSearchOverflowNo {
		t.Fatalf("backfilled %s = %v, want a fresh key and flag F", normal, rows2)
	}
	again, err := search.Backfill(ctx, g, search.BackfillOptions{})
	if err != nil {
		t.Fatalf("second Backfill() error = %v", err)
	}
	for _, r := range again {
		if r.Label == "CloudResource" || r.Label == "TerraformVariable" {
			if r.Keyed != 0 {
				t.Errorf("second Backfill keyed %d %s nodes, want 0 (idempotent)", r.Keyed, r.Label)
			}
		}
	}
	if n, err := search.OversizedKeyCount(ctx, g); err != nil || n != 0 {
		t.Fatalf("OversizedKeyCount() = %d, %v; want 0", n, err)
	}
	if state := g.indexState(ctx, t); state != "ONLINE" {
		t.Fatalf("%s state = %s after the backfill, want ONLINE", graph.InfraSearchKeyIndexName, state)
	}
}
