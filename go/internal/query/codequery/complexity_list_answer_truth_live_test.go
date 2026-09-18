// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live answer-truth proof for #6689 row A8 on the pinned NornicDB image.
//
// The unscoped complexity list with no repo_id (POST /api/v0/code/complexity,
// MCP find_most_complex_functions) anchors on a bare MATCH (e:Function) and
// attributes the repository through an OPTIONAL MATCH. On an older NornicDB
// build that shape projected the expression text "repo.id" and "repo.name" on
// every row. The test calls the production list function against a seed whose
// right answer is known by construction, including a function with no
// repository path, which must keep ranking with empty repository columns.
//
// Run against an isolated container on the pinned image:
//
//	docker run -d --name eshu-answer-truth -e NORNICDB_NO_AUTH=true \
//	  -e NORNICDB_EMBEDDING_ENABLED=false -p 127.0.0.1:27687:7687 \
//	  timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27687 go test ./internal/query/codequery \
//	  -tags live_nornicdb_answer_truth -run TestLiveNornicDBComplexityListAnswerTruth -count=1 -v
package codequery

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// complexityAnswerTruthSeed uses literal property values only: on the pinned
// v1.3.3 image an expression inside a CREATE property map can be stored as
// mangled literal text.
var complexityAnswerTruthSeed = []string{
	`CREATE (:Repository {id: 'answer-truth-code:repo-b', name: 'answer-truth-repo-b'})`,
	`CREATE (:File {id: 'answer-truth-code:file-a', relative_path: 'a.go', language: 'go'})`,
	`CREATE (:File {id: 'answer-truth-code:file-b', relative_path: 'b.go', language: 'go'})`,
	`CREATE (:Function {id: 'answer-truth-code:fn-Main', name: 'AnswerTruthMain', language: 'go', cyclomatic_complexity: 7})`,
	`CREATE (:Function {id: 'answer-truth-code:fn-Helper1', name: 'AnswerTruthHelper1', language: 'go', cyclomatic_complexity: 3})`,
	`CREATE (:Function {id: 'answer-truth-code:fn-Orphan', name: 'AnswerTruthOrphan', language: 'go', cyclomatic_complexity: 2})`,
	`MATCH (r:Repository {id: 'answer-truth-code:repo-b'}) MATCH (f:File {id: 'answer-truth-code:file-a'}) CREATE (r)-[:REPO_CONTAINS]->(f)`,
	`MATCH (r:Repository {id: 'answer-truth-code:repo-b'}) MATCH (f:File {id: 'answer-truth-code:file-b'}) CREATE (r)-[:REPO_CONTAINS]->(f)`,
	`MATCH (f:File {id: 'answer-truth-code:file-a'}) MATCH (e:Function {id: 'answer-truth-code:fn-Main'}) CREATE (f)-[:CONTAINS]->(e)`,
	`MATCH (f:File {id: 'answer-truth-code:file-b'}) MATCH (e:Function {id: 'answer-truth-code:fn-Helper1'}) CREATE (f)-[:CONTAINS]->(e)`,
}

const complexityAnswerTruthCleanup = `MATCH (n) WHERE n.id STARTS WITH 'answer-truth-code:' DETACH DELETE n`

func TestLiveNornicDBComplexityListAnswerTruth(t *testing.T) {
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

	writeComplexityAnswerTruth(ctx, t, driver, complexityAnswerTruthCleanup)
	for _, stmt := range complexityAnswerTruthSeed {
		writeComplexityAnswerTruth(ctx, t, driver, stmt)
	}
	defer writeComplexityAnswerTruth(context.Background(), t, driver, complexityAnswerTruthCleanup)

	handler := &CodeHandler{
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: GraphBackendNornicDB,
		Neo4j:        newLiveNornicDBReader(driver, "nornic"),
	}
	results, _, truncated, err := handler.listMostComplexFunctions(ctx, "", 10, querycontract.RepositoryAccessFilter{AllScopes: true})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(results))
	for _, row := range results {
		got = append(got, fmt.Sprintf("%s|%v|%s|%s|%s",
			StringVal(row, "name"), row["complexity"], StringVal(row, "file_path"),
			StringVal(row, "repo_id"), StringVal(row, "repo_name")))
	}
	t.Logf("rows (name|complexity|file|repo_id|repo_name): %v truncated=%v", got, truncated)
	want := []string{
		"AnswerTruthMain|7|a.go|answer-truth-code:repo-b|answer-truth-repo-b",
		"AnswerTruthHelper1|3|b.go|answer-truth-code:repo-b|answer-truth-repo-b",
		"AnswerTruthOrphan|2|||",
	}
	if !reflect.DeepEqual(got, want) || truncated {
		t.Fatalf("rows = %v (truncated=%v), want %v", got, truncated, want)
	}
}

func writeComplexityAnswerTruth(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, cypher string) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: "nornic"})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		t.Fatalf("write %q: %v", cypher, err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume %q: %v", cypher, err)
	}
}
