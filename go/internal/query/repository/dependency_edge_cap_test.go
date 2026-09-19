// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestLoadUnscopedRepositoryDependencyEdgesCapsTransfer proves the grouped
// read's transfer stays bounded when the graph may hold more DEPENDS_ON edges
// than the bound (#6786 review R3-F2). LIMIT on the grouped read bounds
// source groups, not edges, so without a cap every Repository DEPENDS_ON edge
// would cross the wire. When the probe count exceeds the bound, or the probe
// is unreadable, the loader first reads per-source group sizes and asks the
// grouped read for only the smallest group prefix whose sizes exceed the
// bound. That prefix still holds the first `limit` edges in (source, target)
// order, so the clipped edge list is unchanged.
func TestLoadUnscopedRepositoryDependencyEdgesCapsTransfer(t *testing.T) {
	t.Parallel()

	const limit = 3
	sizes := []map[string]any{
		{"source_id": "repository:a", "target_count": int64(2)},
		{"source_id": "repository:b", "target_count": int64(2)},
		{"source_id": "repository:c", "target_count": int64(1)},
	}
	groups := []map[string]any{
		{"source_id": "repository:a", "target_ids": []any{"repository:y", "repository:x"}},
		{"source_id": "repository:b", "target_ids": []any{"repository:z", "repository:x"}},
		{"source_id": "repository:c", "target_ids": []any{"repository:x"}},
	}
	tests := []struct {
		name      string
		probeRows []map[string]any
		probeErr  error
	}{
		{name: "probe count exceeds the bound", probeRows: []map[string]any{{"edge_count": int64(40)}}},
		{name: "probe fails", probeErr: errors.New("probe unavailable")},
		{name: "probe count unreadable", probeRows: []map[string]any{{"edge_count": "many"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var ran []string
			var groupLimit any
			reader := querytestutil.FakeRepoGraphReader{
				RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
					ran = append(ran, cypher)
					switch cypher {
					case RepositoryDependencyEdgeCountCypher:
						return tt.probeRows, tt.probeErr
					case RepositoryDependencyGroupSizeCypher:
						return sizes, nil
					case RepositoryDependencyGroupedEdgeCypher:
						groupLimit = params["group_limit"]
						n, _ := groupLimit.(int)
						return groups[:min(n, len(groups))], nil
					default:
						t.Fatalf("unexpected statement %q", cypher)
						return nil, nil
					}
				},
			}

			result := loadUnscopedRepositoryDependencyEdges(context.Background(), reader, limit)

			wantRan := []string{RepositoryDependencyEdgeCountCypher, RepositoryDependencyGroupSizeCypher, RepositoryDependencyGroupedEdgeCypher}
			if !reflect.DeepEqual(ran, wantRan) {
				t.Fatalf("statements = %q, want probe, group sizes, grouped read", ran)
			}
			if groupLimit != 2 {
				t.Fatalf("group_limit = %#v, want 2 (sizes 2+2 exceed the bound of 3)", groupLimit)
			}
			want := []repositoryDependencyEdge{
				{Source: "repository:a", Target: "repository:x"},
				{Source: "repository:a", Target: "repository:y"},
				{Source: "repository:b", Target: "repository:x"},
			}
			if !reflect.DeepEqual(result.Edges, want) {
				t.Fatalf("edges = %v, want %v", result.Edges, want)
			}
			if !result.Truncated || !result.TransferCapped || result.Err != nil {
				t.Fatalf("result = %+v, want a truncated, capped read with no error", result)
			}
			if (tt.probeErr != nil) != (result.ProbeErr != nil) {
				t.Fatalf("ProbeErr = %v, want probe error %v carried for telemetry", result.ProbeErr, tt.probeErr)
			}
		})
	}
}

// TestLoadUnscopedRepositoryDependencyEdgesCappedEdgeCases covers the capped
// path when the group sizes fit the bound, when the sizes prove more edges
// than the bound but fewer ids come back (edges removed between the two
// reads, or null ids that collect drops), when there are no Repository
// DEPENDS_ON edges at all, and when either capped statement fails. Groups cut
// off by the prefix still exist, so the read must be reported truncated even
// when the ids that came back fit the bound.
func TestLoadUnscopedRepositoryDependencyEdgesCappedEdgeCases(t *testing.T) {
	t.Parallel()

	const limit = 3
	probeOver := []map[string]any{{"edge_count": int64(40)}}
	tests := []struct {
		name          string
		sizes         []map[string]any
		sizesErr      error
		groupedErr    error
		groupedRows   []map[string]any
		wantRan       int
		wantGroups    any
		wantEdges     []repositoryDependencyEdge
		wantTruncated bool
		wantErr       bool
	}{
		{
			name: "sizes fit the bound: every group is read and the read is complete",
			sizes: []map[string]any{
				{"source_id": "repository:a", "target_count": int64(2)},
				{"source_id": "repository:b", "target_count": int64(1)},
			},
			wantRan:    3,
			wantGroups: 2,
			wantEdges: []repositoryDependencyEdge{
				{Source: "repository:a", Target: "repository:x"},
				{Source: "repository:a", Target: "repository:y"},
				{Source: "repository:b", Target: "repository:x"},
			},
		},
		{
			name: "sizes prove more edges than the bound but fewer ids come back",
			sizes: []map[string]any{
				{"source_id": "repository:a", "target_count": int64(2)},
				{"source_id": "repository:b", "target_count": int64(2)},
			},
			groupedRows: []map[string]any{
				{"source_id": "repository:a", "target_ids": []any{"repository:x"}},
				{"source_id": "repository:b", "target_ids": []any{"repository:x"}},
			},
			wantRan:    3,
			wantGroups: 2,
			wantEdges: []repositoryDependencyEdge{
				{Source: "repository:a", Target: "repository:x"},
				{Source: "repository:b", Target: "repository:x"},
			},
			wantTruncated: true,
		},
		{
			name:      "no Repository DEPENDS_ON edges: the grouped read is skipped",
			sizes:     []map[string]any{},
			wantRan:   2,
			wantEdges: []repositoryDependencyEdge{},
		},
		{
			name:     "group-size read fails",
			sizesErr: errors.New("sizes unavailable"),
			wantRan:  2,
			wantErr:  true,
		},
		{
			name:       "grouped read fails",
			sizes:      []map[string]any{{"source_id": "repository:a", "target_count": int64(1)}},
			groupedErr: errors.New("grouped unavailable"),
			wantRan:    3,
			wantGroups: 1,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ran := 0
			var groupLimit any
			reader := querytestutil.FakeRepoGraphReader{
				RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
					ran++
					switch cypher {
					case RepositoryDependencyEdgeCountCypher:
						return probeOver, nil
					case RepositoryDependencyGroupSizeCypher:
						return tt.sizes, tt.sizesErr
					default:
						groupLimit = params["group_limit"]
						if tt.groupedRows != nil {
							return tt.groupedRows, tt.groupedErr
						}
						return []map[string]any{
							{"source_id": "repository:a", "target_ids": []any{"repository:y", "repository:x"}},
							{"source_id": "repository:b", "target_ids": []any{"repository:x"}},
						}, tt.groupedErr
					}
				},
			}

			result := loadUnscopedRepositoryDependencyEdges(context.Background(), reader, limit)

			if ran != tt.wantRan {
				t.Fatalf("statements run = %d, want %d", ran, tt.wantRan)
			}
			if groupLimit != tt.wantGroups {
				t.Fatalf("group_limit = %#v, want %#v", groupLimit, tt.wantGroups)
			}
			if (result.Err != nil) != tt.wantErr {
				t.Fatalf("Err = %v, want error %v", result.Err, tt.wantErr)
			}
			if tt.wantErr {
				if result.Edges != nil {
					t.Fatalf("edges = %v, want nil on error", result.Edges)
				}
				return
			}
			if !reflect.DeepEqual(result.Edges, tt.wantEdges) {
				t.Fatalf("edges = %v, want %v", result.Edges, tt.wantEdges)
			}
			if result.Truncated != tt.wantTruncated || !result.TransferCapped {
				t.Fatalf("result = %+v, want truncated=%v and a capped read", result, tt.wantTruncated)
			}
		})
	}
}

// TestRepositoryDependencyGroupLimit covers the prefix computation that caps
// the grouped read: the smallest group count whose sizes sum past the bound,
// or every group when they never do. An unreadable size counts as one edge,
// the smallest a MATCHed group can hold, so it can only widen the prefix and
// never drop an edge inside the bound. Groups cut off by the prefix mean more
// edges exist than the bound, so the read is reported as over the bound.
func TestRepositoryDependencyGroupLimit(t *testing.T) {
	t.Parallel()

	size := func(n any) map[string]any { return map[string]any{"target_count": n} }
	tests := []struct {
		name     string
		rows     []map[string]any
		want     int
		wantOver bool
	}{
		{name: "no groups", rows: nil, want: 0},
		{name: "sizes never exceed the bound", rows: []map[string]any{size(int64(1)), size(int64(2))}, want: 2},
		{name: "sizes reach but do not exceed the bound", rows: []map[string]any{size(int64(2)), size(int64(1)), size(int64(4))}, want: 3, wantOver: true},
		{name: "first group alone exceeds the bound", rows: []map[string]any{size(int64(9)), size(int64(1))}, want: 1, wantOver: true},
		{name: "last group tips the sum over the bound", rows: []map[string]any{size(int64(1)), size(int64(1)), size(int64(2))}, want: 3, wantOver: true},
		{name: "unreadable sizes count as one edge", rows: []map[string]any{size("x"), size(nil), size(int64(1))}, want: 3},
		{name: "int and float sizes are read", rows: []map[string]any{size(2), size(float64(2))}, want: 2, wantOver: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, over := repositoryDependencyGroupLimit(tt.rows, 3)
			if got != tt.want || over != tt.wantOver {
				t.Fatalf("repositoryDependencyGroupLimit() = (%d, %v), want (%d, %v)", got, over, tt.want, tt.wantOver)
			}
		})
	}
}

// TestRepositoryDependencyGroupSizeCypherShape pins the group-size read to
// the same fast-path family with a count(end) aggregate, bounded at the
// fetch limit: a source group holds at least one edge, so more than the
// fetch limit of groups already means more edges than the bound.
func TestRepositoryDependencyGroupSizeCypherShape(t *testing.T) {
	t.Parallel()

	assertFastPathGroupedShape(t, RepositoryDependencyGroupSizeCypher,
		"RETURN s.id AS source_id, count(t) AS target_count",
		fmt.Sprintf("LIMIT %d", repositoryDependencyClusterEdgeFetchLimit),
	)
}
