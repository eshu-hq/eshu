// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_label_predicates

// Live #6786 X11 proof for the repository OVERRIDES story read. Its
// any(label IN labels(x) WHERE label IN $override_labels) endpoint filters sit
// in the WHERE of a relationship MATCH, where NornicDB v1.3.3 ignores them.
// The canonical inheritance writer only creates OVERRIDES between the override
// labels, so production data cannot currently expose the leak; the seed below
// writes one out-of-contract edge directly to prove the filter is dead.
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:28020 ESHU_LIVE_GRAPH_BACKEND=nornicdb \
//	  go test ./internal/query/codequery -tags live_nornicdb_label_predicates \
//	  -run TestLiveRelationshipStoryOverrideLabelPredicate -count=1 -v
package codequery

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
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Ground truth: Function 'x11-ov:method' OVERRIDES Class 'x11-ov:base' (in
// contract) and Variable 'x11-ov:var' (out of contract). Only the first is an
// override row.
var overrideLabelSeed = []string{
	`CREATE (:Repository {id: 'x11-ov:repo', name: 'x11-ov-repo'})`,
	`CREATE (:File {id: 'x11-ov:file', relative_path: 'svc.py'})`,
	`CREATE (:Function {id: 'x11-ov:method', uid: 'x11-ov:method', name: 'handle'})`,
	`CREATE (:Class {id: 'x11-ov:base', uid: 'x11-ov:base', name: 'Base'})`,
	`CREATE (:Variable {id: 'x11-ov:var', uid: 'x11-ov:var', name: 'handle_var'})`,
	`MATCH (a:Repository {id: 'x11-ov:repo'}) MATCH (b:File {id: 'x11-ov:file'}) CREATE (a)-[:REPO_CONTAINS]->(b)`,
	`MATCH (a:File {id: 'x11-ov:file'}) MATCH (b:Function {id: 'x11-ov:method'}) CREATE (a)-[:CONTAINS]->(b)`,
	`MATCH (a:Function {id: 'x11-ov:method'}) MATCH (b:Class {id: 'x11-ov:base'}) CREATE (a)-[:OVERRIDES {reason: 'in-contract'}]->(b)`,
	`MATCH (a:Function {id: 'x11-ov:method'}) MATCH (b:Variable {id: 'x11-ov:var'}) CREATE (a)-[:OVERRIDES {reason: 'out-of-contract'}]->(b)`,
}

func TestLiveRelationshipStoryOverrideLabelPredicate(t *testing.T) {
	reader := openOverrideLabelReader(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, stmt := range overrideLabelSeed {
		if err := reader.ExecuteCypher(ctx, graph.CypherStatement{Cypher: stmt}); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	handler := &CodeHandler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	start := time.Now()
	rows, _, _, err := handler.relationshipStoryOverrideRows(ctx, codemodel.RelationshipStoryRequest{RepoID: "x11-ov:repo"})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, row := range rows {
		got = append(got, querycontract.StringVal(row, "source_id")+"->"+querycontract.StringVal(row, "target_id"))
	}
	sort.Strings(got)
	t.Logf("backend=%s rows=%v elapsed=%s", os.Getenv("ESHU_LIVE_GRAPH_BACKEND"), got, elapsed)
	if want := []string{"x11-ov:method->x11-ov:base"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("override rows = %v, want %v", got, want)
	}
}

// openOverrideLabelReader connects to ESHU_NEO4J_URI and applies Eshu's schema
// for ESHU_LIVE_GRAPH_BACKEND, as production bootstrap does.
func openOverrideLabelReader(t *testing.T) overrideLabelReader {
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
	reader := overrideLabelReader{driver: driver}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := graph.EnsureSchemaWithBackend(ctx, reader, logger, graph.SchemaBackend(backend)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return reader
}

// overrideLabelReader is this file's live GraphQuery and schema executor.
type overrideLabelReader struct {
	driver neo4jdriver.DriverWithContext
}

// Run reads in an auto-commit read session, as the production Neo4jReader does.
func (r overrideLabelReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
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

func (r overrideLabelReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (r overrideLabelReader) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	_, err := neo4jdriver.ExecuteQuery(ctx, r.driver, stmt.Cypher, stmt.Parameters, neo4jdriver.EagerResultTransformer)
	return err
}
