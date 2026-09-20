// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_label_predicates

// Live #6786 X11 proof for QueryRepoInfrastructureFromGraph. On NornicDB
// v1.3.3 a label test in the WHERE of a relationship MATCH is ignored, so the
// infrastructure read returned every node a repository file CONTAINS
// (functions, classes, workloads) as infrastructure. The test drives the
// production function against a seed whose right answer is known by
// construction, on both backends.
//
// Run against a fresh container per backend (see
// docs/internal/evidence/6786-nornicdb-label-predicates.md):
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:28020 ESHU_LIVE_GRAPH_BACKEND=nornicdb \
//	  go test ./internal/query/repository -tags live_nornicdb_label_predicates \
//	  -run TestLiveRepositoryInfrastructureLabelPredicate -count=1 -v
package repository

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Ground truth: the repository's one file CONTAINS five infrastructure nodes
// (K8sResource, TerraformModule, two TerraformResources, TerraformDataSource)
// plus a Function, a Class and a Workload (not infrastructure). The read must
// return exactly the five, with every projected column intact and in the
// server's ORDER BY type, name order: the fix moves the label filter into a
// WHERE attached to WITH, and a multi-clause read is where NornicDB has
// corrupted projections before. The two TerraformResources are seeded in
// reverse name order so the name tiebreak is asserted, not inherited. Rows
// are compared as returned, because the order decides which rows survive the
// LIMIT on a repository with more than the page of infrastructure. The
// "code-heavy" case adds repositoryInfrastructureEntityLimit more Functions,
// which sort ahead of every infrastructure type under ORDER BY type: when the
// label filter is ignored they fill the server-side LIMIT window and the Go
// type filter is left with no infrastructure at all.
var infraLabelPredicateSeed = []string{
	`CREATE (:Repository {id: 'x11-infra:repo', name: 'x11-infra-repo'})`,
	`CREATE (:File {id: 'x11-infra:file', relative_path: 'deploy/main.tf'})`,
	`CREATE (:K8sResource {id: 'x11-infra:k8s', name: 'api-deployment', kind: 'Deployment', config_path: 'k8s/deploy.yaml'})`,
	`CREATE (:TerraformModule {id: 'x11-infra:module', name: 'vpc', source: 'terraform-aws-modules/vpc/aws', terraform_source: 'registry'})`,
	`CREATE (:TerraformResource {id: 'x11-infra:bucket', name: 'bucket', provider: 'aws', resource_type: 'aws_s3_bucket', resource_service: 's3', resource_category: 'storage'})`,
	`CREATE (:TerraformResource {id: 'x11-infra:alpha', name: 'alpha', provider: 'aws', resource_type: 'aws_sqs_queue', resource_service: 'sqs', resource_category: 'messaging'})`,
	`CREATE (:TerraformDataSource {id: 'x11-infra:ami', name: 'ami', provider: 'aws', data_type: 'aws_ami'})`,
	`CREATE (:Function {id: 'x11-infra:fn', name: 'handler'})`,
	`CREATE (:Class {id: 'x11-infra:class', name: 'Service'})`,
	`CREATE (:Workload {id: 'x11-infra:workload', name: 'api'})`,
	infraLabelPredicateEdge("Repository", "x11-infra:repo", "REPO_CONTAINS", "File", "x11-infra:file"),
	infraLabelPredicateEdge("File", "x11-infra:file", "CONTAINS", "K8sResource", "x11-infra:k8s"),
	infraLabelPredicateEdge("File", "x11-infra:file", "CONTAINS", "TerraformModule", "x11-infra:module"),
	infraLabelPredicateEdge("File", "x11-infra:file", "CONTAINS", "TerraformResource", "x11-infra:bucket"),
	infraLabelPredicateEdge("File", "x11-infra:file", "CONTAINS", "TerraformResource", "x11-infra:alpha"),
	infraLabelPredicateEdge("File", "x11-infra:file", "CONTAINS", "TerraformDataSource", "x11-infra:ami"),
	infraLabelPredicateEdge("File", "x11-infra:file", "CONTAINS", "Function", "x11-infra:fn"),
	infraLabelPredicateEdge("File", "x11-infra:file", "CONTAINS", "Class", "x11-infra:class"),
	infraLabelPredicateEdge("File", "x11-infra:file", "CONTAINS", "Workload", "x11-infra:workload"),
}

func infraLabelPredicateEdge(fromLabel, fromID, relType, toLabel, toID string) string {
	return `MATCH (a:` + fromLabel + ` {id: '` + fromID + `'}) MATCH (b:` + toLabel + ` {id: '` + toID + `'}) CREATE (a)-[:` + relType + `]->(b)`
}

func TestLiveRepositoryInfrastructureLabelPredicate(t *testing.T) {
	reader := openInfraLabelPredicateReader(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, stmt := range infraLabelPredicateSeed {
		reader.write(ctx, t, stmt)
	}

	want := []map[string]any{
		{"type": "K8sResource", "name": "api-deployment", "file_path": "deploy/main.tf", "kind": "Deployment", "config_path": "k8s/deploy.yaml"},
		{"type": "TerraformDataSource", "name": "ami", "file_path": "deploy/main.tf", "provider": "aws", "resource_type": "aws_ami"},
		{"type": "TerraformModule", "name": "vpc", "file_path": "deploy/main.tf", "source": "terraform-aws-modules/vpc/aws", "terraform_source": "registry"},
		{"type": "TerraformResource", "name": "alpha", "file_path": "deploy/main.tf", "provider": "aws", "resource_type": "aws_sqs_queue", "resource_service": "sqs", "resource_category": "messaging"},
		{"type": "TerraformResource", "name": "bucket", "file_path": "deploy/main.tf", "provider": "aws", "resource_type": "aws_s3_bucket", "resource_service": "s3", "resource_category": "storage"},
	}
	assertInfra := func(t *testing.T) {
		t.Helper()
		start := time.Now()
		rows, truncated, err := QueryRepoInfrastructureFromGraph(ctx, reader, map[string]any{"repo_id": "x11-infra:repo"})
		elapsed := time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("backend=%s rows=%d truncated=%v elapsed=%s", os.Getenv("ESHU_LIVE_GRAPH_BACKEND"), len(rows), truncated, elapsed)
		if !reflect.DeepEqual(rows, want) || truncated {
			t.Fatalf("infrastructure rows = %v truncated=%v, want %v truncated=false", rows, truncated, want)
		}
	}
	t.Run("small", assertInfra)

	functions := make([]map[string]any, 0, repositoryInfrastructureEntityLimit)
	for i := 0; i < repositoryInfrastructureEntityLimit; i++ {
		functions = append(functions, map[string]any{"id": fmt.Sprintf("x11-infra:bulk-fn-%05d", i), "name": fmt.Sprintf("fn%05d", i)})
	}
	// Two committed statements: on NornicDB v1.3.3 a single
	// MATCH ... UNWIND ... CREATE seed writes one node, not the batch.
	if err := reader.ExecuteCypher(ctx, graph.CypherStatement{
		Cypher:     `UNWIND $rows AS row CREATE (:Function {id: row.id, name: row.name, x11_bulk: true})`,
		Parameters: map[string]any{"rows": functions},
	}); err != nil {
		t.Fatalf("seed bulk functions: %v", err)
	}
	reader.write(ctx, t, `MATCH (f:File {id: 'x11-infra:file'}) MATCH (fn:Function {x11_bulk: true}) CREATE (f)-[:CONTAINS]->(fn)`)
	count, err := reader.RunSingle(ctx, `MATCH (:File {id: 'x11-infra:file'})-[:CONTAINS]->(fn:Function) RETURN count(fn) AS n, count(fn.id) AS ids`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(repositoryInfrastructureEntityLimit + 1); count["n"] != want || count["ids"] != want {
		t.Fatalf("bulk seed read back %v contained functions (%v with ids), want %d", count["n"], count["ids"], want)
	}
	t.Run("code-heavy", assertInfra)
}

// openInfraLabelPredicateReader connects to ESHU_NEO4J_URI and applies Eshu's
// schema for ESHU_LIVE_GRAPH_BACKEND, as production bootstrap does: probes
// without the schema behave differently on NornicDB.
func openInfraLabelPredicateReader(t *testing.T) infraLabelPredicateReader {
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
	reader := infraLabelPredicateReader{driver: driver}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := graph.EnsureSchemaWithBackend(ctx, reader, logger, graph.SchemaBackend(backend)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return reader
}

// infraLabelPredicateReader is this file's live GraphQuery and schema
// executor; the package cannot import root query's reader without a cycle.
type infraLabelPredicateReader struct {
	driver neo4jdriver.DriverWithContext
}

// Run reads in an auto-commit read session, as the production Neo4jReader
// does: NornicDB v1.3.3 rejects CALL subqueries inside a managed transaction,
// so a managed-transaction harness would not exercise the production path.
func (r infraLabelPredicateReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
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

func (r infraLabelPredicateReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (r infraLabelPredicateReader) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	_, err := neo4jdriver.ExecuteQuery(ctx, r.driver, stmt.Cypher, stmt.Parameters, neo4jdriver.EagerResultTransformer)
	return err
}

// write commits one seed statement on its own, as production commits each
// writer phase separately.
func (r infraLabelPredicateReader) write(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	if err := r.ExecuteCypher(ctx, graph.CypherStatement{Cypher: cypher}); err != nil {
		t.Fatalf("seed %q: %v", cypher, err)
	}
}
