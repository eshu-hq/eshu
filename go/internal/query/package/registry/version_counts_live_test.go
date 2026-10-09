// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live backend-parity proof for #7816: the package version-count read
// returns no row for a package id with no versions.
//
// On the pinned NornicDB build, the grouping aggregate
// (RETURN v.package_id, count(v)) over an empty match emits a phantom
// (NULL, 0) group row instead of zero rows, so VersionCountsByPackageID
// came back with a "" key and the backend-diff quorum tripped 1-vs-0 on
// every version-less id (github.com/acme/synthetic-dep in the corpus).
// The post-aggregation null-key filter in packageRegistryVersionCountsCypher
// drops that row; Neo4j never emitted it. Both legs green with identical
// expectations is the cross-backend proof.
//
// Run against an isolated container per backend:
//
//	docker run -d --name eshu-live-nornic -p 127.0.0.1:27940:7687 \
//	  -e NORNICDB_NO_AUTH=true -e NORNICDB_EMBEDDING_ENABLED=false \
//	  ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1
//
//	docker run -d --name eshu-live-neo4j -p 127.0.0.1:27950:7687 -e NEO4J_AUTH=none \
//	  neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27940 go test ./internal/query/package/registry \
//	  -tags live_nornicdb_answer_truth -run TestLiveVersionCountsEmptyGroupIsAbsent -count=1 -v
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27950 ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  go test ./internal/query/package/registry -tags live_nornicdb_answer_truth \
//	  -run TestLiveVersionCountsEmptyGroupIsAbsent -count=1 -v
//
// ESHU_LIVE_GRAPH_BACKEND is nornicdb (default) or neo4j. ESHU_LIVE_GRAPH_DATABASE
// defaults to "nornic" for NornicDB and "neo4j" for Neo4j.
package registry

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const liveVersionCountsPrefix = "zz7816:"

// TestLiveVersionCountsEmptyGroupIsAbsent seeds one PackageVersion node and
// counts two single-id pages: the seeded id resolves to 1, and the
// version-less id resolves to an empty map with no "" key from the pinned
// NornicDB's phantom empty-group row.
func TestLiveVersionCountsEmptyGroupIsAbsent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	live := openLiveVersionCountsGraph(ctx, t)

	presentID := liveVersionCountsPrefix + "lib-common"
	absentID := liveVersionCountsPrefix + "synthetic-dep"
	cleanup := `MATCH (v:PackageVersion) WHERE v.package_id STARTS WITH '` + liveVersionCountsPrefix + `' DETACH DELETE v`
	live.write(ctx, t, cleanup, nil)
	defer live.write(context.Background(), t, cleanup, nil)

	live.write(ctx, t, `CREATE (:PackageVersion:PackageRegistryPackageVersion {uid: $uid, id: $uid, package_id: $package_id, version: '1.0.0'})`, map[string]any{
		"uid":        presentID + "@1.0.0",
		"package_id": presentID,
	})

	counts, err := VersionCountsByPackageID(ctx, live.reader(), []string{presentID})
	if err != nil {
		t.Fatalf("VersionCountsByPackageID: %v", err)
	}
	if counts[presentID] != 1 || len(counts) != 1 {
		t.Fatalf("present page counts on %s = %v, want exactly {present:1}", live.backend, counts)
	}

	// The absent id rides alone, as in the corpus recording: the phantom
	// appears only when the whole match input is empty, never beside a
	// real group.
	counts, err = VersionCountsByPackageID(ctx, live.reader(), []string{absentID})
	if err != nil {
		t.Fatalf("VersionCountsByPackageID: %v", err)
	}
	if counts[absentID] != 0 {
		t.Fatalf("counts[%q] on %s = %d, want 0 (%v)", absentID, live.backend, counts[absentID], counts)
	}
	if _, ok := counts[""]; ok {
		t.Fatalf("counts on %s carries a phantom empty-key row (%v); the null group key must be filtered", live.backend, counts)
	}
	if len(counts) != 0 {
		t.Fatalf("len(counts) on %s = %d, want 0 (%v)", live.backend, len(counts), counts)
	}
}

// liveVersionCountsGraph is a minimal live Bolt session adapter: raw writes
// plus a querycontract.GraphQuery reader over the same session config.
type liveVersionCountsGraph struct {
	driver   neo4jdriver.DriverWithContext
	database string
	backend  string
}

func openLiveVersionCountsGraph(ctx context.Context, t *testing.T) *liveVersionCountsGraph {
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
	return &liveVersionCountsGraph{driver: driver, database: database, backend: backend}
}

func (g *liveVersionCountsGraph) write(ctx context.Context, t *testing.T, cypher string, params map[string]any) {
	t.Helper()
	s := g.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: g.database})
	defer func() { _ = s.Close(ctx) }()
	if _, err := s.Run(ctx, cypher, params); err != nil {
		t.Fatalf("seed write failed: %v\ncypher=%s", err, cypher)
	}
}

// reader adapts the live driver to querycontract.GraphQuery for the
// production read path under test.
func (g *liveVersionCountsGraph) reader() querycontract.GraphQuery {
	return liveVersionCountsReader{graph: g}
}

// liveVersionCountsReader runs reads through short-lived Bolt sessions.
type liveVersionCountsReader struct {
	graph *liveVersionCountsGraph
}

func (r liveVersionCountsReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	s := r.graph.driver.NewSession(ctx, neo4jdriver.SessionConfig{DatabaseName: r.graph.database})
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
	for _, record := range records {
		rows = append(rows, record.AsMap())
	}
	return rows, nil
}

func (r liveVersionCountsReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}
