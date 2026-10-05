// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_label_predicates

// Live #6786 X11 proof for the change-surface traversal. On NornicDB v1.3.3
// the unscoped traversal's any(label IN labels(impacted) ...) whitelist, in
// the WHERE of the variable-length MATCH, is ignored. The Go-side label
// filter drops the non-whitelisted rows, but the server-side LIMIT has already
// run over the unfiltered set, so File nodes that sort ahead of a real impact
// crowd it out of the page. The scoped traversal filters labels in a WHERE
// attached to WITH, which v1.3.3 does evaluate; it is driven here as the
// not-affected control.
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:28020 ESHU_LIVE_GRAPH_BACKEND=nornicdb \
//	  go test ./internal/query/impact -tags live_nornicdb_label_predicates \
//	  -run TestLiveChangeSurfaceLabelPredicate -count=1 -v
package impact

import (
	"context"
	"log/slog"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Ground truth: the repository DEFINES one Workload ('z-checkout') and
// REPO_CONTAINS three Files whose names sort ahead of it. Only the Workload is
// a change-surface impact; with limit 2 it must still be returned.
var changeSurfaceLabelSeed = []string{
	`CREATE (:Repository {id: 'x11-cs:repo', name: 'x11-cs-repo', repo_id: 'x11-cs:repo'})`,
	`CREATE (:File {id: 'x11-cs:file-1', name: 'a-file-1', repo_id: 'x11-cs:repo'})`,
	`CREATE (:File {id: 'x11-cs:file-2', name: 'a-file-2', repo_id: 'x11-cs:repo'})`,
	`CREATE (:File {id: 'x11-cs:file-3', name: 'a-file-3', repo_id: 'x11-cs:repo'})`,
	`CREATE (:Workload {id: 'x11-cs:workload', name: 'z-checkout', repo_id: 'x11-cs:repo'})`,
	changeSurfaceLabelEdge("File", "x11-cs:file-1"),
	changeSurfaceLabelEdge("File", "x11-cs:file-2"),
	changeSurfaceLabelEdge("File", "x11-cs:file-3"),
	`MATCH (a:Repository {id: 'x11-cs:repo'}) MATCH (b:Workload {id: 'x11-cs:workload'}) CREATE (a)-[:DEFINES]->(b)`,
}

func changeSurfaceLabelEdge(label, id string) string {
	return `MATCH (a:Repository {id: 'x11-cs:repo'}) MATCH (b:` + label + ` {id: '` + id + `'}) CREATE (a)-[:REPO_CONTAINS]->(b)`
}

func TestLiveChangeSurfaceLabelPredicate(t *testing.T) {
	reader := openChangeSurfaceLabelReader(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, stmt := range changeSurfaceLabelSeed {
		if err := reader.ExecuteCypher(ctx, graph.CypherStatement{Cypher: stmt}); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	target := ChangeSurfaceTargetCandidate{ID: "x11-cs:repo", Name: "x11-cs-repo", Labels: []string{"Repository"}}
	backend := os.Getenv("ESHU_LIVE_GRAPH_BACKEND")

	cases := []struct {
		name   string
		access querycontract.RepositoryAccessFilter
	}{
		{name: "unscoped", access: querycontract.RepositoryAccessFilter{AllScopes: true}},
		{name: "scoped", access: querycontract.RepositoryAccessFilter{
			AllowedRepositoryIDs: []string{"x11-cs:repo"},
			Allowed:              map[string]struct{}{"x11-cs:repo": {}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			rows, truncated, err := handler.FindChangeSurfaceImpactRows(ctx, target, "", 1, 2, tc.access)
			elapsed := time.Since(start)
			if err != nil {
				t.Fatal(err)
			}
			ids := map[string]struct{}{}
			for _, row := range rows {
				ids[querycontract.StringVal(row, "id")] = struct{}{}
			}
			got := make([]string, 0, len(ids))
			for id := range ids {
				got = append(got, id)
			}
			sort.Strings(got)
			t.Logf("backend=%s access=%s impacted=%v truncated=%v elapsed=%s", backend, tc.name, got, truncated, elapsed)
			if want := []string{"x11-cs:workload"}; !reflect.DeepEqual(got, want) || truncated {
				t.Fatalf("impacted = %v truncated=%v, want %v truncated=false", got, truncated, want)
			}
		})
	}
}

// changeSurfaceDeepSeed is the #7246 control for the `impacted:Label`
// conjunct ahead of the IN labels() whitelist. The depth-1 seed above cannot
// show a traversal being emptied, because its only whitelisted node is one hop
// out. This graph reaches whitelisted nodes at one, two and three hops, through
// non-whitelisted File and Function nodes, with two environments.
var changeSurfaceDeepSeed = []string{
	`CREATE (:Repository {id: 'x7246:repo', name: 'x7246-repo', repo_id: 'x7246:repo'})`,
	`CREATE (:File {id: 'x7246:file-1', name: 'a-file-1', repo_id: 'x7246:repo'})`,
	`CREATE (:File {id: 'x7246:file-2', name: 'a-file-2', repo_id: 'x7246:repo'})`,
	`CREATE (:Function {id: 'x7246:fn', name: 'a-fn', repo_id: 'x7246:repo'})`,
	`CREATE (:TerraformModule {id: 'x7246:module', name: 'y-module', repo_id: 'x7246:repo'})`,
	`CREATE (:Workload {id: 'x7246:deep', name: 'z-deep', environment: 'prod', repo_id: 'x7246:repo'})`,
	`CREATE (:Workload {id: 'x7246:dev', name: 'z-dev', environment: 'dev', repo_id: 'x7246:repo'})`,
	changeSurfaceDeepRel("Repository", "x7246:repo", "REPO_CONTAINS", "File", "x7246:file-1"),
	changeSurfaceDeepRel("Repository", "x7246:repo", "REPO_CONTAINS", "File", "x7246:file-2"),
	changeSurfaceDeepRel("File", "x7246:file-1", "CONTAINS", "Function", "x7246:fn"),
	changeSurfaceDeepRel("File", "x7246:file-2", "CONTAINS", "TerraformModule", "x7246:module"),
	changeSurfaceDeepRel("Function", "x7246:fn", "RUNS_IN", "Workload", "x7246:deep"),
	changeSurfaceDeepRel("Repository", "x7246:repo", "DEFINES", "Workload", "x7246:dev"),
}

func changeSurfaceDeepRel(fromLabel, fromID, rel, toLabel, toID string) string {
	return `MATCH (a:` + fromLabel + ` {id: '` + fromID + `'}) MATCH (b:` + toLabel + ` {id: '` + toID + `'}) CREATE (a)-[:` + rel + `]->(b)`
}

// TestLiveChangeSurfaceLabelConjunctDeepTraversal drives the unscoped read
// the way production does (depth 4) over a graph with whitelisted nodes at
// several hops. On NornicDB v1.3.3 the `impacted:Label` conjunct is ignored in
// this clause position and the IN labels() terms enforce the whitelist; on
// Neo4j both run. The conjunct must neither empty the traversal nor leak
// non-whitelisted nodes into the server-side LIMIT, so the page is checked at
// a limit that truncates the whitelisted set.
func TestLiveChangeSurfaceLabelConjunctDeepTraversal(t *testing.T) {
	reader := openChangeSurfaceLabelReader(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, stmt := range changeSurfaceDeepSeed {
		if err := reader.ExecuteCypher(ctx, graph.CypherStatement{Cypher: stmt}); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	target := ChangeSurfaceTargetCandidate{ID: "x7246:repo", Name: "x7246-repo", Labels: []string{"Repository"}}
	all := querycontract.RepositoryAccessFilter{AllScopes: true}

	cases := []struct {
		name      string
		env       string
		limit     int
		want      []string
		truncated bool
	}{
		{name: "every whitelisted node", limit: 50, want: []string{"x7246:deep", "x7246:dev", "x7246:module"}},
		{name: "environment prod keeps unlabelled-environment nodes", env: "prod", limit: 50, want: []string{"x7246:deep", "x7246:module"}},
		{name: "limit truncates the whitelisted set", limit: 2, want: []string{"x7246:dev", "x7246:module"}, truncated: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, truncated, err := handler.FindChangeSurfaceImpactRows(ctx, target, tc.env, 4, tc.limit, all)
			if err != nil {
				t.Fatal(err)
			}
			ids := map[string]struct{}{}
			for _, row := range rows {
				ids[querycontract.StringVal(row, "id")] = struct{}{}
			}
			got := make([]string, 0, len(ids))
			for id := range ids {
				got = append(got, id)
			}
			sort.Strings(got)
			t.Logf("backend=%s impacted=%v truncated=%v", os.Getenv("ESHU_LIVE_GRAPH_BACKEND"), got, truncated)
			if !reflect.DeepEqual(got, tc.want) || truncated != tc.truncated {
				t.Fatalf("impacted = %v truncated=%v, want %v truncated=%v", got, truncated, tc.want, tc.truncated)
			}
		})
	}
}

// openChangeSurfaceLabelReader connects to ESHU_NEO4J_URI and applies Eshu's
// schema for ESHU_LIVE_GRAPH_BACKEND, as production bootstrap does.
func openChangeSurfaceLabelReader(t *testing.T) changeSurfaceLabelReader {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	backend := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND"))
	if uri == "" || backend == "" {
		t.Fatal("ESHU_NEO4J_URI and ESHU_LIVE_GRAPH_BACKEND (nornicdb|neo4j) are required")
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}
	reader := changeSurfaceLabelReader{driver: driver}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := graph.EnsureSchemaWithBackend(ctx, reader, logger, graph.SchemaBackend(backend)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return reader
}

// changeSurfaceLabelReader is this file's live GraphQuery and schema executor.
type changeSurfaceLabelReader struct {
	driver neo4jdriver.DriverWithContext
}

// Run reads in an auto-commit read session, as the production Neo4jReader
// does: NornicDB v1.3.3 rejects CALL subqueries inside a managed transaction,
// so a managed-transaction harness would not exercise the production path.
func (r changeSurfaceLabelReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead})
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
		rows = append(rows, record.AsMap())
	}
	return rows, nil
}

func (r changeSurfaceLabelReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (r changeSurfaceLabelReader) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	_, err := neo4jdriver.ExecuteQuery(ctx, r.driver, stmt.Cypher, stmt.Parameters, neo4jdriver.EagerResultTransformer)
	return err
}
