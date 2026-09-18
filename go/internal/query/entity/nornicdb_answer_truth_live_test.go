// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live answer-truth proof for #6689 row A6 on the pinned NornicDB image.
//
// GET /api/v0/entities/{id}/context (MCP get_entity_context) anchors on a lone
// node, then runs two OPTIONAL MATCHes. On an older NornicDB build that shape
// projected the expression text "r.id" as the repo_id and returned one garbage
// relationship instead of the real ones. The test drives the real handler
// against a seed whose right answer is known by construction.
//
// Run against an isolated container on the pinned image:
//
//	docker run -d --name eshu-answer-truth -e NORNICDB_NO_AUTH=true \
//	  -e NORNICDB_EMBEDDING_ENABLED=false -p 127.0.0.1:27687:7687 \
//	  timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27687 go test ./internal/query/entity \
//	  -tags live_nornicdb_answer_truth -run TestLiveNornicDBEntityContextAnswerTruth -count=1 -v
package entity

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

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// entityAnswerTruthSeed uses literal property values only: on the pinned
// v1.3.3 image an expression inside a CREATE property map can be stored as
// mangled literal text.
//
// Ground truth: function Main lives in a.go in repo B and CALLS three helpers.
var entityAnswerTruthSeed = []string{
	`CREATE (:Repository {id: 'answer-truth-entity:repo-b', name: 'answer-truth-repo-b'})`,
	`CREATE (:File {id: 'answer-truth-entity:file-a', relative_path: 'a.go', language: 'go'})`,
	`CREATE (:Function {id: 'answer-truth-entity:fn-Main', name: 'Main', language: 'go', start_line: 1, end_line: 9})`,
	`CREATE (:Function {id: 'answer-truth-entity:fn-Helper1', name: 'Helper1', language: 'go'})`,
	`CREATE (:Function {id: 'answer-truth-entity:fn-Helper2', name: 'Helper2', language: 'go'})`,
	`CREATE (:Function {id: 'answer-truth-entity:fn-Helper3', name: 'Helper3', language: 'go'})`,
	entityAnswerTruthEdge("Repository", "answer-truth-entity:repo-b", "REPO_CONTAINS", "File", "answer-truth-entity:file-a"),
	entityAnswerTruthEdge("File", "answer-truth-entity:file-a", "CONTAINS", "Function", "answer-truth-entity:fn-Main"),
	entityAnswerTruthEdge("Function", "answer-truth-entity:fn-Main", "CALLS", "Function", "answer-truth-entity:fn-Helper1"),
	entityAnswerTruthEdge("Function", "answer-truth-entity:fn-Main", "CALLS", "Function", "answer-truth-entity:fn-Helper2"),
	entityAnswerTruthEdge("Function", "answer-truth-entity:fn-Main", "CALLS", "Function", "answer-truth-entity:fn-Helper3"),
}

func entityAnswerTruthEdge(fromLabel, fromID, relType, toLabel, toID string) string {
	return `MATCH (a:` + fromLabel + ` {id: '` + fromID + `'}) MATCH (b:` + toLabel + ` {id: '` + toID + `'}) CREATE (a)-[:` + relType + `]->(b)`
}

const entityAnswerTruthCleanup = `MATCH (n) WHERE n.id STARTS WITH 'answer-truth-entity:' DETACH DELETE n`

func TestLiveNornicDBEntityContextAnswerTruth(t *testing.T) {
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

	reader := entityLiveReader{driver: driver}
	reader.write(ctx, t, entityAnswerTruthCleanup)
	for _, stmt := range entityAnswerTruthSeed {
		reader.write(ctx, t, stmt)
	}
	defer reader.write(context.Background(), t, entityAnswerTruthCleanup)

	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/answer-truth-entity:fn-Main/context", nil)
	req.SetPathValue("entity_id", "answer-truth-entity:fn-Main")
	rec := httptest.NewRecorder()
	handler.GetEntityContext(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	t.Logf("body: %s", rec.Body.String())

	var body struct {
		ID            string           `json:"id"`
		FilePath      string           `json:"file_path"`
		RepoID        string           `json:"repo_id"`
		RepoName      string           `json:"repo_name"`
		Relationships []map[string]any `json:"relationships"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.FilePath != "a.go" || body.RepoID != "answer-truth-entity:repo-b" || body.RepoName != "answer-truth-repo-b" {
		t.Fatalf("file/repo = (%q, %q, %q), want (a.go, answer-truth-entity:repo-b, answer-truth-repo-b)",
			body.FilePath, body.RepoID, body.RepoName)
	}
	got := make([]string, 0, len(body.Relationships))
	for _, rel := range body.Relationships {
		got = append(got, querycontract.StringVal(rel, "type")+" "+querycontract.StringVal(rel, "target_id"))
	}
	sort.Strings(got)
	want := []string{
		"CALLS answer-truth-entity:fn-Helper1",
		"CALLS answer-truth-entity:fn-Helper2",
		"CALLS answer-truth-entity:fn-Helper3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("relationships = %v, want %v", got, want)
	}
}

// entityLiveReader is the test-only live GraphQuery for this file. The package
// cannot import root query's Neo4jReader without a cycle.
type entityLiveReader struct {
	driver neo4jdriver.DriverWithContext
}

func (r entityLiveReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: "nornic"})
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

func (r entityLiveReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (r entityLiveReader) write(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: "nornic"})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		t.Fatalf("write %q: %v", cypher, err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume %q: %v", cypher, err)
	}
}
