// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// recordingClusterGraph is a GraphQuery fake that answers the DEPENDS_ON
// cardinality probe and the edge pre-pass separately and records which
// statements ran, so tests can prove the edge scan is skipped or executed.
type recordingClusterGraph struct {
	edgeCount int64
	countErr  error
	edges     []map[string]any
	edgeErr   error
	ran       []string
}

func (g *recordingClusterGraph) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	g.ran = append(g.ran, cypher)
	if strings.Contains(cypher, "count(r)") {
		if g.countErr != nil {
			return nil, g.countErr
		}
		return []map[string]any{{"edge_count": g.edgeCount}}, nil
	}
	if g.edgeErr != nil {
		return nil, g.edgeErr
	}
	return g.edges, nil
}

func (g *recordingClusterGraph) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (g *recordingClusterGraph) ranEdgeScan() bool {
	for _, c := range g.ran {
		if strings.Contains(c, "RETURN s.id AS source_id") {
			return true
		}
	}
	return false
}

// TestLoadRepositoryDependencyClustersSkipsEdgeScanWhenNoEdges proves that
// when the graph holds zero DEPENDS_ON relationships the Repository-anchored
// edge scan does not run. On NornicDB that scan expands every Repository's
// full adjacency even when no DEPENDS_ON edge exists (seconds at hundreds of
// repositories), while the bare relationship-type count is answered from the
// type index. Zero edges means the cluster map is empty by definition, so the
// skip is exact, not a heuristic.
func TestLoadRepositoryDependencyClustersSkipsEdgeScanWhenNoEdges(t *testing.T) {
	t.Parallel()

	graph := &recordingClusterGraph{edgeCount: 0}
	result := loadRepositoryDependencyClusters(context.Background(), graph, querycontract.RepositoryAccessFilter{AllScopes: true})

	if graph.ranEdgeScan() {
		t.Fatalf("edge scan ran with zero DEPENDS_ON edges; statements: %v", graph.ran)
	}
	if len(result.clusters) != 0 {
		t.Fatalf("clusters = %v, want empty", result.clusters)
	}
	if !result.skipped {
		t.Fatalf("skipped = false, want true when edge count is zero")
	}
}

// TestLoadRepositoryDependencyClustersRunsEdgeScanWhenEdgesExist proves the
// probe never hides real edges: a nonzero count runs the unchanged edge scan
// and clusters are built from its rows.
func TestLoadRepositoryDependencyClustersRunsEdgeScanWhenEdgesExist(t *testing.T) {
	t.Parallel()

	graph := &recordingClusterGraph{
		edgeCount: 2,
		edges: []map[string]any{
			{"source_id": "repository:a", "target_id": "repository:b"},
			{"source_id": "repository:b", "target_id": "repository:c"},
		},
	}
	result := loadRepositoryDependencyClusters(context.Background(), graph, querycontract.RepositoryAccessFilter{AllScopes: true})

	if !graph.ranEdgeScan() {
		t.Fatalf("edge scan did not run with %d edges", graph.edgeCount)
	}
	if result.skipped {
		t.Fatalf("skipped = true, want false when edges exist")
	}
	for _, id := range []string{"repository:a", "repository:b", "repository:c"} {
		if result.clusters[id] != "repository:a" {
			t.Errorf("cluster[%s] = %q, want repository:a", id, result.clusters[id])
		}
	}
}

// TestLoadRepositoryDependencyClustersCountErrorStillRunsEdgeScan proves the
// probe is only an optimization: if the cardinality probe fails, the edge scan
// still runs so cluster evidence is not silently dropped, and the probe error
// is surfaced on the result for telemetry.
func TestLoadRepositoryDependencyClustersCountErrorStillRunsEdgeScan(t *testing.T) {
	t.Parallel()

	graph := &recordingClusterGraph{
		countErr: errors.New("probe failed"),
		edges:    []map[string]any{{"source_id": "repository:a", "target_id": "repository:b"}},
	}
	result := loadRepositoryDependencyClusters(context.Background(), graph, querycontract.RepositoryAccessFilter{AllScopes: true})

	if !graph.ranEdgeScan() {
		t.Fatalf("edge scan did not run after probe error")
	}
	if result.clusters["repository:b"] != "repository:a" {
		t.Fatalf("clusters = %v, want a/b clustered", result.clusters)
	}
	if result.probeErr == nil {
		t.Fatalf("probeErr = nil, want the probe failure surfaced")
	}
}

// TestLoadRepositoryDependencyClustersScopedCallerSkipsUnscopedProbe proves a
// scoped caller never issues the unscoped cardinality probe: every statement
// on the repository list path must carry the caller's grant predicate.
func TestLoadRepositoryDependencyClustersScopedCallerSkipsUnscopedProbe(t *testing.T) {
	t.Parallel()

	graph := &recordingClusterGraph{edgeCount: 0}
	access := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-a"}}
	if !access.Scoped() {
		t.Fatalf("test setup: access filter is not scoped")
	}
	loadRepositoryDependencyClusters(context.Background(), graph, access)

	for _, stmt := range graph.ran {
		if strings.Contains(stmt, "count(r)") {
			t.Fatalf("scoped caller issued unscoped probe: %s", stmt)
		}
	}
	if !graph.ranEdgeScan() {
		t.Fatalf("scoped caller did not run the scoped edge scan")
	}
}

// TestDependencyEdgeCountIsZeroRejectsUnreadableProbe proves the skip only
// fires on a present, recognized zero: a missing column or an unknown value
// type must not be mistaken for "no edges".
func TestDependencyEdgeCountIsZeroRejectsUnreadableProbe(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		row  map[string]any
		want bool
	}{
		{"int64 zero", map[string]any{"edge_count": int64(0)}, true},
		{"int zero", map[string]any{"edge_count": 0}, true},
		{"float zero", map[string]any{"edge_count": float64(0)}, true},
		{"nonzero", map[string]any{"edge_count": int64(3)}, false},
		{"missing column", map[string]any{}, false},
		{"nil value", map[string]any{"edge_count": nil}, false},
		{"unknown type", map[string]any{"edge_count": "0"}, false},
	}
	for _, tc := range cases {
		if got := dependencyEdgeCountIsZero(tc.row); got != tc.want {
			t.Errorf("%s: dependencyEdgeCountIsZero = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestLoadRepositoryDependencyClustersSurfacesEdgeScanError proves an edge
// scan failure is reported on the result instead of being swallowed, while the
// list still degrades to non-cluster grouping.
func TestLoadRepositoryDependencyClustersSurfacesEdgeScanError(t *testing.T) {
	t.Parallel()

	graph := &recordingClusterGraph{edgeCount: 1, edgeErr: errors.New("scan failed")}
	result := loadRepositoryDependencyClusters(context.Background(), graph, querycontract.RepositoryAccessFilter{AllScopes: true})

	if len(result.clusters) != 0 {
		t.Fatalf("clusters = %v, want empty after scan error", result.clusters)
	}
	if result.edgeErr == nil {
		t.Fatalf("edgeErr = nil, want the scan failure surfaced")
	}
}
