// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imports_test

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
)

// cappedCycleEdges returns edges forming 1,001 disjoint reciprocal pairs, one
// past the 1,000-cycle enumeration cap, so every response over them reports a
// capped enumeration.
func cappedCycleEdges() []map[string]any {
	edges := make([]map[string]any, 0, 2002)
	for index := 0; index < 1001; index++ {
		left := fmt.Sprintf("k%04d", index)
		right := left + "z"
		edges = append(edges,
			importDependencyCycleProofEdge("src/"+left+".py", left+".py", right, 1),
			importDependencyCycleProofEdge("src/"+right+".py", right+".py", left, 2),
		)
	}
	return edges
}

// TestCycleResponsePagingHasATerminalPageOnACappedRun pins the #7346 paging
// contract. `truncated` means the answer is incomplete (more pages, or the
// enumeration stopped early) and stays true on every page of a capped run, so a
// client cannot mistake a capped list for a complete one. `has_more` is the
// paging signal: it is true only while another page exists, and `next_offset` is
// a cursor only then. Before this, a capped run echoed the request offset as
// `next_offset` on the terminal empty page, so a client paging until
// `truncated` was false looped on a fixed cursor forever.
func TestCycleResponsePagingHasATerminalPageOnACappedRun(t *testing.T) {
	t.Parallel()

	edges := cappedCycleEdges()
	tests := []struct {
		name         string
		offset       int
		limit        int
		wantCount    int
		wantHasMore  bool
		wantNextNil  bool
		wantNextWant int
	}{
		{name: "first page", offset: 0, limit: 10, wantCount: 10, wantHasMore: true, wantNextWant: 10},
		{name: "middle page", offset: 500, limit: 200, wantCount: 200, wantHasMore: true, wantNextWant: 700},
		{name: "last page of the capped 1000 rows", offset: 900, limit: 200, wantCount: 100, wantHasMore: false, wantNextNil: true},
		{name: "terminal empty page past the cap", offset: 1000, limit: 10, wantCount: 0, wantHasMore: false, wantNextNil: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: tt.limit, Offset: tt.offset}
			_, resp := buildCycleResponse(t, req, edges)

			if got := resp["truncated"]; got != true {
				t.Fatalf("truncated = %#v, want true on every page of a capped run", got)
			}
			if got := resp["has_more"]; got != tt.wantHasMore {
				t.Fatalf("has_more = %#v, want %v", got, tt.wantHasMore)
			}
			cycles, _ := resp["cycles"].([]map[string]any)
			if len(cycles) != tt.wantCount {
				t.Fatalf("len(cycles) = %d, want %d", len(cycles), tt.wantCount)
			}
			if tt.wantNextNil {
				if resp["next_offset"] != nil {
					t.Fatalf("next_offset = %#v, want nil so a pager stops", resp["next_offset"])
				}
				return
			}
			if got := resp["next_offset"]; got != tt.wantNextWant {
				t.Fatalf("next_offset = %#v, want %d", got, tt.wantNextWant)
			}
		})
	}
}

// TestCycleResponsePagerTerminatesOnACappedRun drives a pager the way a client
// would: follow next_offset until it is nil, with a hard iteration bound so a
// fixed-cursor loop fails the test instead of hanging it.
func TestCycleResponsePagerTerminatesOnACappedRun(t *testing.T) {
	t.Parallel()

	edges := cappedCycleEdges()
	offset, seen := 0, 0
	for page := 0; ; page++ {
		if page > 50 {
			t.Fatalf("pager did not terminate after %d pages; next_offset kept returning a cursor (offset=%d, seen=%d)", page, offset, seen)
		}
		req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 200, Offset: offset}
		_, resp := buildCycleResponse(t, req, edges)
		cycles, _ := resp["cycles"].([]map[string]any)
		seen += len(cycles)
		next, more := resp["next_offset"].(int)
		if !more {
			break
		}
		if next <= offset {
			t.Fatalf("next_offset = %d does not advance past offset %d", next, offset)
		}
		offset = next
	}
	if seen != 1000 {
		t.Fatalf("pager saw %d cycles, want the 1000 the enumeration cap allows", seen)
	}
}

// TestCycleResponsePagingUncappedRunEndsWithoutTruncation keeps the ordinary
// case unchanged: a run that fits its pages reports no truncation and no cursor
// on the last page.
func TestCycleResponsePagingUncappedRunEndsWithoutTruncation(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		importDependencyCycleProofEdge("src/a.py", "a.py", "b", 1),
		importDependencyCycleProofEdge("src/b.py", "b.py", "a", 2),
	}
	req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 10}
	_, resp := buildCycleResponse(t, req, edges)

	if got := resp["truncated"]; got != false {
		t.Fatalf("truncated = %#v, want false", got)
	}
	if got := resp["has_more"]; got != false {
		t.Fatalf("has_more = %#v, want false", got)
	}
	if resp["next_offset"] != nil {
		t.Fatalf("next_offset = %#v, want nil", resp["next_offset"])
	}
}
