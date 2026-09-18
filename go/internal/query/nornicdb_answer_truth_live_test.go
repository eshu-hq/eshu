// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live answer-truth proof for #6689 rows A1, A2, A5 and A7 on the pinned
// NornicDB image.
//
// Each row was a production read that returned a wrong answer with no error on
// an older NornicDB build: a count(DISTINCT) that ignored DISTINCT (A1, A2, A5)
// and a second OPTIONAL MATCH that bound its relationship to null after a
// node-only anchor (A7). The tests run the production statements -- the
// package-level count tables and the real HTTP handler -- against a seed whose
// right answer is known by construction, and fail on anything but the exact
// expected rows.
//
// Run against an isolated container on the pinned image:
//
//	docker run -d --name eshu-answer-truth -e NORNICDB_NO_AUTH=true \
//	  -e NORNICDB_EMBEDDING_ENABLED=false -p 127.0.0.1:27687:7687 \
//	  timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27687 go test ./internal/query \
//	  -tags live_nornicdb_answer_truth -run TestLiveNornicDBAnswerTruth -count=1 -v
package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// answerTruthSeed is the fixture. Every id carries the answer-truth prefix so
// cleanup removes exactly what the test wrote. Every property value is a
// literal: on the pinned v1.3.3 image an expression inside a CREATE property
// map (string concatenation, or a list subscript over an UNWIND row) is stored
// as mangled literal text, so a computed id would never match the reads below.
//
// Ground truth, by construction:
//   - repo A defines one workload with three instances, all on one platform:
//     three raw paths, platform_count 1.
//   - repo A reaches repo B through DEPENDS_ON and USES_MODULE: two edges,
//     dependency_count 1.
//   - function Helper2 has three incoming edges (CONTAINS from its file, CALLS
//     from Main and Helper1) and one outgoing edge (CALLS to Helper3).
var answerTruthSeed = answerTruthSeedStatements()

func answerTruthSeedStatements() []string {
	stmts := []string{
		`CREATE (:Repository {id: 'answer-truth-query:repo-a', name: 'answer-truth-repo-a'})`,
		`CREATE (:Repository {id: 'answer-truth-query:repo-b', name: 'answer-truth-repo-b'})`,
		`CREATE (:Workload {id: 'answer-truth-query:wl-a', name: 'answer-truth-wl-a', repo_id: 'answer-truth-query:repo-a'})`,
		`CREATE (:WorkloadInstance {id: 'answer-truth-query:wi-1', environment: 'prod', repo_id: 'answer-truth-query:repo-a'})`,
		`CREATE (:WorkloadInstance {id: 'answer-truth-query:wi-2', environment: 'staging', repo_id: 'answer-truth-query:repo-a'})`,
		`CREATE (:WorkloadInstance {id: 'answer-truth-query:wi-3', environment: 'prod', repo_id: 'answer-truth-query:repo-a'})`,
		`CREATE (:Platform {id: 'answer-truth-query:plat-1', name: 'answer-truth-eks'})`,
		`CREATE (:File {id: 'answer-truth-query:file-b', relative_path: 'b.go', language: 'go'})`,
	}
	for _, name := range []string{"Main", "Helper1", "Helper2", "Helper3"} {
		stmts = append(stmts, `CREATE (:Function {id: 'answer-truth-query:fn-`+name+`', name: '`+name+`', language: 'go'})`)
	}
	edges := [][3]string{
		{"Repository", "answer-truth-query:repo-a", "DEFINES Workload answer-truth-query:wl-a"},
		{"WorkloadInstance", "answer-truth-query:wi-1", "INSTANCE_OF Workload answer-truth-query:wl-a"},
		{"WorkloadInstance", "answer-truth-query:wi-2", "INSTANCE_OF Workload answer-truth-query:wl-a"},
		{"WorkloadInstance", "answer-truth-query:wi-3", "INSTANCE_OF Workload answer-truth-query:wl-a"},
		{"WorkloadInstance", "answer-truth-query:wi-1", "RUNS_ON Platform answer-truth-query:plat-1"},
		{"WorkloadInstance", "answer-truth-query:wi-2", "RUNS_ON Platform answer-truth-query:plat-1"},
		{"WorkloadInstance", "answer-truth-query:wi-3", "RUNS_ON Platform answer-truth-query:plat-1"},
		{"Repository", "answer-truth-query:repo-a", "DEPENDS_ON Repository answer-truth-query:repo-b"},
		{"Repository", "answer-truth-query:repo-a", "USES_MODULE Repository answer-truth-query:repo-b"},
		{"Repository", "answer-truth-query:repo-b", "REPO_CONTAINS File answer-truth-query:file-b"},
		{"File", "answer-truth-query:file-b", "CONTAINS Function answer-truth-query:fn-Helper2"},
		{"Function", "answer-truth-query:fn-Main", "CALLS Function answer-truth-query:fn-Helper2"},
		{"Function", "answer-truth-query:fn-Helper1", "CALLS Function answer-truth-query:fn-Helper2"},
		{"Function", "answer-truth-query:fn-Helper2", "CALLS Function answer-truth-query:fn-Helper3"},
	}
	for _, edge := range edges {
		parts := strings.Fields(edge[2])
		stmts = append(stmts, `MATCH (a:`+edge[0]+` {id: '`+edge[1]+`'}) MATCH (b:`+parts[1]+` {id: '`+parts[2]+`'}) CREATE (a)-[:`+parts[0]+`]->(b)`)
	}
	return stmts
}

const answerTruthCleanup = `MATCH (n) WHERE n.id STARTS WITH 'answer-truth-query:' DETACH DELETE n`

func TestLiveNornicDBAnswerTruth(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
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

	answerTruthWrite(ctx, t, driver, answerTruthCleanup)
	for _, stmt := range answerTruthSeed {
		answerTruthWrite(ctx, t, driver, stmt)
	}
	defer answerTruthWrite(context.Background(), t, driver, answerTruthCleanup)

	reader := NewNeo4jReader(driver, "nornic")
	repoParams := map[string]any{"repo_id": "answer-truth-query:repo-a"}

	t.Run("A1 A2 graph-summary repo counts", func(t *testing.T) {
		want := map[string]int{"workload_count": 1, "platform_count": 1, "dependency_count": 1}
		for _, entry := range graphSummaryRepoEcosystemCounts {
			expected, checked := want[entry.field]
			if !checked {
				continue
			}
			row, err := reader.RunSingle(ctx, entry.cypher, repoParams)
			if err != nil {
				t.Fatalf("%s: %v", entry.field, err)
			}
			t.Logf("%s row: %v", entry.field, row)
			if got := IntVal(row, "count"); got != expected {
				t.Fatalf("%s = %d, want %d (row %v)", entry.field, got, expected, row)
			}
		}
	})

	t.Run("A5 ecosystem overview platform_count", func(t *testing.T) {
		unscoped, err := runEcosystemOverviewCounts(ctx, reader, repositoryAccessFilterFromContext(ctx))
		if err != nil {
			t.Fatalf("unscoped: %v", err)
		}
		t.Logf("unscoped counts: %v", unscoped)
		if got := unscoped["platform_count"]; got != 1 {
			t.Fatalf("unscoped platform_count = %v, want 1", got)
		}

		for _, tc := range []struct {
			grant []string
			want  int
		}{
			{grant: []string{"answer-truth-query:repo-a"}, want: 1},
			{grant: []string{"answer-truth-query:repo-b"}, want: 0},
		} {
			scopedCtx := ContextWithAuthContext(ctx, AuthContext{
				Mode:                 AuthModeScoped,
				TenantID:             "tenant-a",
				WorkspaceID:          "workspace-a",
				AllowedRepositoryIDs: tc.grant,
			})
			scoped, err := runEcosystemOverviewCounts(scopedCtx, reader, repositoryAccessFilterFromContext(scopedCtx))
			if err != nil {
				t.Fatalf("scoped %v: %v", tc.grant, err)
			}
			t.Logf("scoped %v counts: %v", tc.grant, scoped)
			if got := scoped["platform_count"]; got != tc.want {
				t.Fatalf("scoped %v platform_count = %v, want %d", tc.grant, got, tc.want)
			}
		}
	})

	t.Run("A7 infra relationships incoming and outgoing", func(t *testing.T) {
		handler := &InfraHandler{Profile: ProfileLocalAuthoritative, Neo4j: reader}
		req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships",
			strings.NewReader(`{"entity_id":"answer-truth-query:fn-Helper2"}`))
		rec := httptest.NewRecorder()
		handler.getRelationships(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		t.Logf("body: %s", rec.Body.String())

		var body struct {
			Outgoing []map[string]any `json:"outgoing"`
			Incoming []map[string]any `json:"incoming"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		gotIn := answerTruthEdges(body.Incoming, "source_id")
		wantIn := []string{
			"CALLS answer-truth-query:fn-Helper1",
			"CALLS answer-truth-query:fn-Main",
			"CONTAINS answer-truth-query:file-b",
		}
		if !reflect.DeepEqual(gotIn, wantIn) {
			t.Fatalf("incoming = %v, want %v", gotIn, wantIn)
		}
		gotOut := answerTruthEdges(body.Outgoing, "target_id")
		if want := []string{"CALLS answer-truth-query:fn-Helper3"}; !reflect.DeepEqual(gotOut, want) {
			t.Fatalf("outgoing = %v, want %v", gotOut, want)
		}
	})
}

// answerTruthEdges renders relationship entries as sorted "TYPE id" strings so
// a garbage entry (a null type or an echoed expression such as "source.id")
// fails the comparison instead of being counted.
func answerTruthEdges(entries []map[string]any, idKey string) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, StringVal(entry, "type")+" "+StringVal(entry, idKey))
	}
	sort.Strings(out)
	return out
}

func answerTruthWrite(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, cypher string) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: "nornic",
	})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		t.Fatalf("write %q: %v", cypher, err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume %q: %v", cypher, err)
	}
}
