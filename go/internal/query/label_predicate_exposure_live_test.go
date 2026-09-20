// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_label_predicates

// Live #6786 X11 exposure controls for the production reads whose label
// predicate sits in a clause position NornicDB v1.3.3 does evaluate: a label
// test on a single-node MATCH (infra aggregate provider filter, Argo CD
// category search), `'Label' IN labels(n)` in a projection CASE (infra
// provider buckets), and `$type IN labels(e)` in the WHERE of a relationship
// MATCH (entity resolve). Each drives the production function against a seed
// whose answer is known by construction, on both backends.
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:28020 ESHU_LIVE_GRAPH_BACKEND=nornicdb \
//	  go test ./internal/query -tags live_nornicdb_label_predicates \
//	  -run TestLiveLabelPredicateExposureControls -count=1 -v
package query

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
	"github.com/eshu-hq/eshu/go/internal/query/entity"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

var labelExposureSeed = []string{
	`CREATE (:CloudResource {id: 'x11-na:cr-aws', name: 'cr-aws', source_system: 'aws'})`,
	`CREATE (:CloudResource {id: 'x11-na:cr-gcp', name: 'cr-gcp', source_system: 'gcp'})`,
	`CREATE (:TerraformResource {id: 'x11-na:tf-aws', name: 'tf-aws', provider: 'aws'})`,
	`CREATE (:TerraformResource {id: 'x11-na:tf-gcp', name: 'tf-gcp', provider: 'gcp'})`,
	`CREATE (:TerraformResource {id: 'x11-na:tf-src', name: 'tf-src', provider: 'gcp', source_system: 'aws'})`,
	`CREATE (:K8sResource {id: 'x11-na:k8s', name: 'k8s'})`,
	`CREATE (:ArgoCDApplication:ArgoCDApplicationSet {id: 'x11-na:argo-dual', name: 'argo-dual'})`,
	`CREATE (:ArgoCDApplicationSet {id: 'x11-na:argo-set', name: 'argo-set'})`,
	`CREATE (:ArgoCDApplication {id: 'x11-na:argo-app', name: 'argo-app'})`,
	`CREATE (:Repository {id: 'x11-na:repo', name: 'x11-na-repo'})`,
	`CREATE (:File {id: 'x11-na:file', relative_path: 'main.go'})`,
	`CREATE (:Function {id: 'x11-na:fn', name: 'dup'})`,
	`CREATE (:Class {id: 'x11-na:class', name: 'dup'})`,
	`MATCH (a:Repository {id: 'x11-na:repo'}) MATCH (b:File {id: 'x11-na:file'}) CREATE (a)-[:REPO_CONTAINS]->(b)`,
	`MATCH (a:File {id: 'x11-na:file'}) MATCH (b:Function {id: 'x11-na:fn'}) CREATE (a)-[:CONTAINS]->(b)`,
	`MATCH (a:File {id: 'x11-na:file'}) MATCH (b:Class {id: 'x11-na:class'}) CREATE (a)-[:CONTAINS]->(b)`,
}

func TestLiveLabelPredicateExposureControls(t *testing.T) {
	reader := openLabelExposureReader(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, stmt := range labelExposureSeed {
		if err := reader.ExecuteCypher(ctx, graph.CypherStatement{Cypher: stmt}); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	backend := os.Getenv("ESHU_LIVE_GRAPH_BACKEND")

	// infraSearchProviderFilterPredicate / infraResourceAggregateFilterClauses:
	// `(n.provider = $provider OR (n:CloudResource AND n.source_system = $provider))`
	// on a per-label single-node MATCH, plus the CASE `'CloudResource' IN labels(n)`
	// provider bucket. aws = cr-aws (via source_system) + tf-aws (via provider);
	// tf-src carries source_system 'aws' but is not a CloudResource, so an
	// ignored n:CloudResource test would count it as a third aws resource.
	t.Run("infra aggregate provider filter", func(t *testing.T) {
		store := GraphInfraResourceAggregateStore{Graph: reader}
		got, err := store.CountInfraResources(ctx, InfraResourceAggregateFilter{Provider: "aws"})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("backend=%s total=%d by_provider=%v by_label=%v", backend, got.TotalResources, got.ByProvider, got.ByLabel)
		if got.TotalResources != 2 || !reflect.DeepEqual(got.ByProvider, map[string]int{"aws": 2}) {
			t.Fatalf("aws count = %d by_provider=%v, want 2 {aws:2}", got.TotalResources, got.ByProvider)
		}
	})

	// searchArgoCDCategoryRows: `MATCH (n:ArgoCDApplicationSet) WHERE true AND
	// NOT n:ArgoCDApplication` must drop the dual-labelled node from the second
	// read so it appears once.
	t.Run("argocd category NOT label", func(t *testing.T) {
		handler := &InfraHandler{Neo4j: reader}
		rows, err := handler.searchArgoCDCategoryRows(ctx, querycontract.RepositoryAccessFilter{AllScopes: true}, 50)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, row := range rows {
			got = append(got, StringVal(row, "id"))
		}
		sort.Strings(got)
		t.Logf("backend=%s argocd ids=%v", backend, got)
		if want := []string{"x11-na:argo-app", "x11-na:argo-dual", "x11-na:argo-set"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("argocd ids = %v, want %v", got, want)
		}
	})

	// entity.BuildResolveGraphQuery: `WHERE e.name = $name AND $type IN labels(e)`
	// after a two-hop relationship MATCH must keep only the Function.
	t.Run("entity resolve type filter", func(t *testing.T) {
		cypher, params := entity.BuildResolveGraphQuery(entity.ResolveRequest{Name: "dup", Type: "function", RepoID: "x11-na:repo"}, 10, querycontract.RepositoryAccessFilter{AllScopes: true})
		rows, err := reader.Run(ctx, cypher, params)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, row := range rows {
			got = append(got, StringVal(row, "id"))
		}
		t.Logf("backend=%s type=%v resolve ids=%v", backend, params["type"], got)
		if want := []string{"x11-na:fn"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("resolve ids = %v, want %v", got, want)
		}
	})
}

// openLabelExposureReader connects to ESHU_NEO4J_URI and applies Eshu's schema
// for ESHU_LIVE_GRAPH_BACKEND, as production bootstrap does.
func openLabelExposureReader(t *testing.T) labelExposureReader {
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
	reader := labelExposureReader{driver: driver}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := graph.EnsureSchemaWithBackend(ctx, reader, logger, graph.SchemaBackend(backend)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return reader
}

// labelExposureReader is this file's live GraphQuery and schema executor.
type labelExposureReader struct {
	driver neo4jdriver.DriverWithContext
}

// Run reads in an auto-commit read session, as the production Neo4jReader
// does (NornicDB v1.3.3 rejects CALL subqueries in a managed transaction).
func (r labelExposureReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
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

func (r labelExposureReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (r labelExposureReader) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	_, err := neo4jdriver.ExecuteQuery(ctx, r.driver, stmt.Cypher, stmt.Parameters, neo4jdriver.EagerResultTransformer)
	return err
}
