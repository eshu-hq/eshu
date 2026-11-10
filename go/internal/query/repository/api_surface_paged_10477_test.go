// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// pagedEndpointFixture returns count endpoint rows in (path, endpoint_id)
// order, matching the detail query's ORDER BY so the fake can slice by
// limit/offset exactly like the backend would.
func pagedEndpointFixture(count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for index := range count {
		rows = append(rows, map[string]any{
			"endpoint_id": fmt.Sprintf("endpoint-%03d", index),
			"path":        fmt.Sprintf("/v1/resource-%03d", index),
			"methods":     []any{"GET"},
		})
	}
	return rows
}

// pagedEndpointReader answers the endpoint count with the fixture total and
// the detail read with the limit/offset window, proving the query carries
// both params instead of slicing a capped read in Go.
func pagedEndpointReader(t *testing.T, rows []map[string]any) querycontract.GraphQuery {
	t.Helper()
	return graph.FakeRepoGraphReader{
		RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "count(endpoint) AS endpoint_count"):
				return []map[string]any{{"endpoint_count": len(rows)}}, nil
			case strings.Contains(cypher, "endpoint.id AS endpoint_id"):
				if !strings.Contains(cypher, "OFFSET $offset") && !strings.Contains(cypher, "SKIP $offset") {
					t.Errorf("endpoint detail query carries no offset clause:\n%s", cypher)
				}
				limit, _ := params["limit"].(int)
				offset, _ := params["offset"].(int)
				if offset < 0 {
					offset = 0
				}
				if offset > len(rows) {
					offset = len(rows)
				}
				end := len(rows)
				if limit > 0 && offset+limit < end {
					end = offset + limit
				}
				return append([]map[string]any(nil), rows[offset:end]...), nil
			default:
				t.Fatalf("unexpected graph query:\n%s", cypher)
				return nil, nil
			}
		},
	}
}

// TestQueryRepoAPISurfacePagedHonorsLimitOffset is the #10477 Q1 RED test:
// the offset applies at the Cypher read, so pages are disjoint and cover the
// full count-ordered set with detail_truncated naming the cut.
func TestQueryRepoAPISurfacePagedHonorsLimitOffset(t *testing.T) {
	t.Parallel()

	rows := pagedEndpointFixture(60)
	reader := pagedEndpointReader(t, rows)

	var got []map[string]any
	for _, offset := range []int{0, 25, 50} {
		surface, err := QueryRepoAPISurfacePaged(context.Background(), reader, map[string]any{"repo_id": "repo-paged"}, 25, offset)
		if err != nil {
			t.Fatalf("QueryRepoAPISurfacePaged(offset=%d) error = %v, want a page", offset, err)
		}
		if total := querycontract.IntVal(surface, "endpoint_count"); total != 60 {
			t.Fatalf("offset %d: endpoint_count = %d, want 60", offset, total)
		}
		got = append(got, querycontract.MapSliceValue(surface, "endpoints")...)
		last, _ := surface["detail_truncated"].(bool)
		if offset < 50 && !last {
			t.Fatalf("offset %d: detail_truncated = false, want true (more rows past the page)", offset)
		}
		if offset == 50 && last {
			t.Fatalf("offset 50: detail_truncated = true, want false (last page holds the final 10)")
		}
	}
	if len(got) != 60 {
		t.Fatalf("paged endpoints = %d rows, want 60", len(got))
	}
	for index, row := range got {
		if want := fmt.Sprintf("/v1/resource-%03d", index); querycontract.StringVal(row, "path") != want {
			t.Fatalf("row %d path = %q, want %q", index, querycontract.StringVal(row, "path"), want)
		}
	}
}
