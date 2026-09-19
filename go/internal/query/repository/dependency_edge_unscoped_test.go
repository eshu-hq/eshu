// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestLoadRepositoryDependencyEdgesUnscopedUsesGroupedRead proves an
// unscoped caller reads repository dependency edges through the grouped
// `RETURN s.id, collect(t.id)` shape, which NornicDB v1.3.3 answers from the
// DEPENDS_ON relationship-type index instead of expanding every Repository's
// adjacency (#6786 review R2-F6), and that the grouped rows flatten into the
// same (source, target)-ordered edge list the per-edge read returns. The
// probe's whole-graph DEPENDS_ON count (3) is within the bound, so every
// Repository DEPENDS_ON edge fits in the bound too: the grouped read runs
// directly at the fetch-limit group bound and the group-size read is skipped.
func TestLoadRepositoryDependencyEdgesUnscopedUsesGroupedRead(t *testing.T) {
	t.Parallel()

	var ran []string
	var groupLimit any
	reader := querytestutil.FakeRepoGraphReader{
		RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			ran = append(ran, cypher)
			if cypher == RepositoryDependencyEdgeCountCypher {
				return []map[string]any{{"edge_count": int64(3)}}, nil
			}
			groupLimit = params["group_limit"]
			return []map[string]any{
				{"source_id": "repository:a", "target_ids": []any{"repository:c", "repository:b"}},
				{"source_id": "repository:b", "target_ids": []string{"repository:c"}},
			}, nil
		},
	}

	result := loadRepositoryDependencyEdges(context.Background(), reader, querycontract.RepositoryAccessFilter{AllScopes: true})

	wantRan := []string{RepositoryDependencyEdgeCountCypher, RepositoryDependencyGroupedEdgeCypher}
	if !reflect.DeepEqual(ran, wantRan) {
		t.Fatalf("unscoped statements = %q, want the probe then the grouped read %q", ran, wantRan)
	}
	if groupLimit != repositoryDependencyClusterEdgeFetchLimit {
		t.Fatalf("group_limit = %#v, want the fetch limit %d", groupLimit, repositoryDependencyClusterEdgeFetchLimit)
	}
	want := []repositoryDependencyEdge{
		{Source: "repository:a", Target: "repository:b"},
		{Source: "repository:a", Target: "repository:c"},
		{Source: "repository:b", Target: "repository:c"},
	}
	if !reflect.DeepEqual(result.Edges, want) {
		t.Fatalf("edges = %v, want %v", result.Edges, want)
	}
	if result.Truncated || result.Err != nil || result.Skipped || result.TransferCapped {
		t.Fatalf("result = %+v, want a complete, uncapped, untruncated read", result)
	}
}

// TestRepositoryDependencyGroupedEdgeCypherShape pins the grouped read to
// the only shape NornicDB's tryFastSingleHopAgg fast path accepts: a fixed
// 1-hop DEPENDS_ON pattern with both endpoints labelled :Repository, no
// WHERE clause, and a group key on the start variable followed by
// collect(end.prop). The parameterized LIMIT bounds source groups.
func TestRepositoryDependencyGroupedEdgeCypherShape(t *testing.T) {
	t.Parallel()

	assertFastPathGroupedShape(t, RepositoryDependencyGroupedEdgeCypher,
		"RETURN s.id AS source_id, collect(t.id) AS target_ids",
		"LIMIT $group_limit",
	)
}

func assertFastPathGroupedShape(t *testing.T, cypher string, wants ...string) {
	t.Helper()
	for _, want := range append([]string{
		"MATCH (s:Repository)-[:DEPENDS_ON]->(t:Repository)",
		"ORDER BY source_id",
	}, wants...) {
		if !strings.Contains(cypher, want) {
			t.Errorf("grouped cypher missing %q:\n%s", want, cypher)
		}
	}
	for _, forbidden := range []string{"WHERE", "allowed_repository_ids"} {
		if strings.Contains(cypher, forbidden) {
			t.Errorf("grouped cypher must not contain %q (it disables the fast path and it is unscoped):\n%s", forbidden, cypher)
		}
	}
	querytestutil.AssertCypherHasNoBrokenAndOr(t, cypher)
}

// TestLoadRepositoryDependencyEdgesScopedKeepsPerEdgeGrantRead proves a
// scoped caller never takes the grouped read: the fast path requires no
// WHERE clause, and a scoped caller's grant is a WHERE predicate on both
// endpoints, so the scoped path stays on the per-edge grant-predicated scan.
func TestLoadRepositoryDependencyEdgesScopedKeepsPerEdgeGrantRead(t *testing.T) {
	t.Parallel()

	var ran []string
	reader := querytestutil.FakeRepoGraphReader{
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			ran = append(ran, cypher)
			return []map[string]any{{"source_id": "repository:a", "target_id": "repository:b"}}, nil
		},
	}
	access := querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repository:a", "repository:b"},
		Allowed:              map[string]struct{}{"repository:a": {}, "repository:b": {}},
	}

	result := loadRepositoryDependencyEdges(context.Background(), reader, access)

	if len(ran) != 1 || strings.Contains(ran[0], "collect(") || !strings.Contains(ran[0], "s.id IN $allowed_repository_ids") {
		t.Fatalf("scoped statements = %q, want exactly the grant-predicated per-edge read", ran)
	}
	want := []repositoryDependencyEdge{{Source: "repository:a", Target: "repository:b"}}
	if !reflect.DeepEqual(result.Edges, want) {
		t.Fatalf("edges = %v, want %v", result.Edges, want)
	}
}

// TestFlattenGroupedRepositoryDependencyEdges covers the grouped-row
// truncation contract. The LIMIT bounds source groups, not edges, so the
// read is truncated when either the group count or the flattened edge count
// exceeds repositoryDependencyClusterEdgeLimit. Flattened edges are sorted
// by (source, target) before clipping; because every group holds at least
// one edge, the first N source groups in source order always contain the
// first N edges in (source, target) order, so the clipped set equals what
// the per-edge `ORDER BY source_id, target_id LIMIT N` read returns.
func TestFlattenGroupedRepositoryDependencyEdges(t *testing.T) {
	t.Parallel()

	const limit = 3
	tests := []struct {
		name          string
		rows          []map[string]any
		wantEdges     []repositoryDependencyEdge
		wantTruncated bool
	}{
		{
			name: "under the bound",
			rows: []map[string]any{
				{"source_id": "repository:a", "target_ids": []any{"repository:b"}},
			},
			wantEdges: []repositoryDependencyEdge{{Source: "repository:a", Target: "repository:b"}},
		},
		{
			name: "exactly at the bound",
			rows: []map[string]any{
				{"source_id": "repository:a", "target_ids": []any{"repository:c", "repository:b"}},
				{"source_id": "repository:b", "target_ids": []any{"repository:a"}},
			},
			wantEdges: []repositoryDependencyEdge{
				{Source: "repository:a", Target: "repository:b"},
				{Source: "repository:a", Target: "repository:c"},
				{Source: "repository:b", Target: "repository:a"},
			},
		},
		{
			name: "flattened edges exceed the bound",
			rows: []map[string]any{
				{"source_id": "repository:a", "target_ids": []any{"repository:d", "repository:b"}},
				{"source_id": "repository:b", "target_ids": []any{"repository:c", "repository:a"}},
			},
			wantEdges: []repositoryDependencyEdge{
				{Source: "repository:a", Target: "repository:b"},
				{Source: "repository:a", Target: "repository:d"},
				{Source: "repository:b", Target: "repository:a"},
			},
			wantTruncated: true,
		},
		{
			name: "group count exceeds the bound",
			rows: []map[string]any{
				{"source_id": "repository:a", "target_ids": []any{"repository:x"}},
				{"source_id": "repository:b", "target_ids": []any{"repository:x"}},
				{"source_id": "repository:c", "target_ids": []any{"repository:x"}},
				{"source_id": "repository:d", "target_ids": []any{"repository:x"}},
			},
			wantEdges: []repositoryDependencyEdge{
				{Source: "repository:a", Target: "repository:x"},
				{Source: "repository:b", Target: "repository:x"},
				{Source: "repository:c", Target: "repository:x"},
			},
			wantTruncated: true,
		},
		{
			name: "duplicate parallel edges are kept like the per-edge read",
			rows: []map[string]any{
				{"source_id": "repository:a", "target_ids": []any{"repository:b", "repository:b"}},
			},
			wantEdges: []repositoryDependencyEdge{
				{Source: "repository:a", Target: "repository:b"},
				{Source: "repository:a", Target: "repository:b"},
			},
		},
		{
			name: "blank ids and unreadable target lists are skipped",
			rows: []map[string]any{
				{"source_id": "", "target_ids": []any{"repository:b"}},
				{"source_id": "repository:a", "target_ids": []any{"", 7, "repository:b"}},
				{"source_id": "repository:c", "target_ids": "repository:d"},
			},
			wantEdges: []repositoryDependencyEdge{{Source: "repository:a", Target: "repository:b"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			edges, truncated := flattenGroupedRepositoryDependencyEdges(tt.rows, limit)
			if !reflect.DeepEqual(edges, tt.wantEdges) {
				t.Errorf("edges = %v, want %v", edges, tt.wantEdges)
			}
			if truncated != tt.wantTruncated {
				t.Errorf("truncated = %v, want %v", truncated, tt.wantTruncated)
			}
		})
	}
}

// dependencyEdgeRowsForRead shapes fake graph rows for whichever
// dependency-edge statement ran: (source_id, target_count) rows for the
// unscoped RepositoryDependencyGroupSizeCypher, grouped (source_id,
// target_ids) rows for RepositoryDependencyGroupedEdgeCypher, and the given per-edge
// (source_id, target_id) rows unchanged for the scoped per-edge read. Groups
// keep first-appearance source order, as the fakes' rows already are.
func dependencyEdgeRowsForRead(cypher string, rows []map[string]any) []map[string]any {
	if cypher == RepositoryDependencyGroupSizeCypher {
		sizes := make([]map[string]any, 0, len(rows))
		for _, group := range dependencyEdgeRowsForRead(RepositoryDependencyGroupedEdgeCypher, rows) {
			sizes = append(sizes, map[string]any{"source_id": group["source_id"], "target_count": int64(len(group["target_ids"].([]any)))})
		}
		return sizes
	}
	if !strings.Contains(cypher, "collect(t.id)") {
		return rows
	}
	grouped := make([]map[string]any, 0, len(rows))
	index := map[string]int{}
	for _, row := range rows {
		source := querycontract.StringVal(row, "source_id")
		target := querycontract.StringVal(row, "target_id")
		i, ok := index[source]
		if !ok {
			i = len(grouped)
			index[source] = i
			grouped = append(grouped, map[string]any{"source_id": source, "target_ids": []any{}})
		}
		grouped[i]["target_ids"] = append(grouped[i]["target_ids"].([]any), target)
	}
	return grouped
}
