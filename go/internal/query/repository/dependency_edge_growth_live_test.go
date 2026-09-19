// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live proof for #6786 review R4-F1: the capped unscoped dependency-edge read
// must not silently drop a real group when a source repository gains its
// first DEPENDS_ON edge between the group-size read and the grouped read.
//
// The test wraps the live reader so a real write lands on the backend right
// after RepositoryDependencyGroupSizeCypher returns, which is the window a
// concurrent reducer write can hit. It drives the production
// loadUnscopedRepositoryDependencyEdges with a small bound, and seeds
// Workload DEPENDS_ON edges so the whole-graph probe exceeds that bound and
// the capped path runs.
//
// It needs a graph with no other Repository DEPENDS_ON edges, because the
// group-size read and the bound are whole-graph. Run it against a fresh
// container, on its own:
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:28080 ESHU_LIVE_GRAPH_BACKEND=nornicdb \
//	  go test ./internal/query/repository -tags live_nornicdb_answer_truth \
//	  -run TestLiveRepositoryDependencyEdgesCappedConcurrentGrowth -count=1 -v
package repository

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	eshugraph "github.com/eshu-hq/eshu/go/internal/graph"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	growthA = "repository:6786-growth-a"
	growthB = "repository:6786-growth-b"
	growthM = "repository:6786-growth-m"
	growthZ = "repository:6786-growth-z"
	growthT = "repository:6786-growth-t"
)

// growthCleanup removes every node this test seeds.
var growthCleanup = []string{
	`MATCH (n) WHERE n.id STARTS WITH 'repository:6786-growth-' DETACH DELETE n`,
	`MATCH (n) WHERE n.id STARTS WITH 'workload:6786-growth-' DETACH DELETE n`,
}

// growthSeed holds sources a, b and z with one Repository edge each, m with
// none yet, and four Workload edges that lift the whole-graph probe to seven,
// above both bounds the subtests use.
var growthSeed = []string{
	`CREATE (:Repository {id: '` + growthA + `', name: 'a'})`,
	`CREATE (:Repository {id: '` + growthB + `', name: 'b'})`,
	`CREATE (:Repository {id: '` + growthM + `', name: 'm'})`,
	`CREATE (:Repository {id: '` + growthZ + `', name: 'z'})`,
	`CREATE (:Repository {id: '` + growthT + `', name: 't'})`,
	depMarkerEdge("Repository", growthA, "Repository", growthT),
	depMarkerEdge("Repository", growthB, "Repository", growthT),
	depMarkerEdge("Repository", growthZ, "Repository", growthT),
	`CREATE (:Workload {id: 'workload:6786-growth-1'})-[:DEPENDS_ON]->(:Workload {id: 'workload:6786-growth-2'})`,
	`CREATE (:Workload {id: 'workload:6786-growth-3'})-[:DEPENDS_ON]->(:Workload {id: 'workload:6786-growth-4'})`,
	`CREATE (:Workload {id: 'workload:6786-growth-5'})-[:DEPENDS_ON]->(:Workload {id: 'workload:6786-growth-6'})`,
	`CREATE (:Workload {id: 'workload:6786-growth-7'})-[:DEPENDS_ON]->(:Workload {id: 'workload:6786-growth-8'})`,
}

// growingReader writes one statement to the backend right after the
// group-size read returns, and passes every other statement through.
type growingReader struct {
	repositoryLiveReader
	t     *testing.T
	write string
	wrote bool
}

func (r *growingReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	rows, err := r.repositoryLiveReader.Run(ctx, cypher, params)
	if cypher == RepositoryDependencyGroupSizeCypher && !r.wrote {
		r.wrote = true
		r.repositoryLiveReader.write(ctx, r.t, r.write)
	}
	return rows, err
}

// TestLiveRepositoryDependencyEdgesCappedConcurrentGrowth proves on a live
// backend that a group added between the capped path's two reads either
// comes back with every real group or is disclosed as truncated.
func TestLiveRepositoryDependencyEdgesCappedConcurrentGrowth(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	backend := strings.TrimSpace(strings.ToLower(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")))
	if backend == "" {
		t.Fatal("ESHU_LIVE_GRAPH_BACKEND is required (nornicdb|neo4j)")
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
	reader := repositoryLiveReader{driver: driver, database: liveGraphDatabaseName(backend)}

	schemaBackend := eshugraph.SchemaBackendNeo4j
	if backend == "nornicdb" {
		schemaBackend = eshugraph.SchemaBackendNornicDB
	}
	if err := eshugraph.EnsureSchemaWithBackend(ctx, reader, nil, schemaBackend); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	defer func() {
		for _, stmt := range growthCleanup {
			reader.write(context.Background(), t, stmt)
		}
	}()

	allEdges := []repositoryDependencyEdge{
		{Source: growthA, Target: growthT},
		{Source: growthB, Target: growthT},
		{Source: growthM, Target: growthT},
		{Source: growthZ, Target: growthT},
	}
	tests := []struct {
		name          string
		limit         int
		wantEdges     []repositoryDependencyEdge
		wantTruncated bool
	}{
		{name: "grown_graph_fits_the_bound", limit: 5, wantEdges: allEdges},
		{name: "grown_graph_exceeds_the_bound", limit: 3, wantEdges: allEdges[:3], wantTruncated: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, stmt := range growthCleanup {
				reader.write(ctx, t, stmt)
			}
			sizes, err := readRepositoryDependencyGroupSizes(ctx, reader)
			if err != nil {
				t.Fatalf("read group sizes: %v", err)
			}
			if len(sizes) != 0 {
				t.Fatalf("graph already holds %d Repository DEPENDS_ON groups; run this test on a fresh container", len(sizes))
			}
			for _, stmt := range growthSeed {
				reader.write(ctx, t, stmt)
			}

			growing := &growingReader{
				repositoryLiveReader: reader,
				t:                    t,
				write:                depMarkerEdge("Repository", growthM, "Repository", growthT),
			}
			result := loadUnscopedRepositoryDependencyEdges(ctx, growing, tt.limit)

			if !growing.wrote {
				t.Fatal("the capped path did not run the group-size read; the growth write never landed")
			}
			if result.Err != nil {
				t.Fatalf("Err = %v", result.Err)
			}
			if !result.TransferCapped {
				t.Fatalf("TransferCapped = false, want the capped path (probe must exceed %d)", tt.limit)
			}
			if !reflect.DeepEqual(result.Edges, tt.wantEdges) || result.Truncated != tt.wantTruncated {
				t.Fatalf("edges = %v truncated = %v, want %v truncated = %v",
					result.Edges, result.Truncated, tt.wantEdges, tt.wantTruncated)
			}
		})
	}
}
