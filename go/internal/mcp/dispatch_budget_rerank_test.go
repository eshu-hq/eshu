// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/contentread"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// reorderRanker is a deterministic stand-in for the hybrid re-rankers. It
// permutes whatever window it is handed, so a page that re-ranks a different
// window on a later request, or that cuts the re-ranked order at a SQL
// offset, loses or repeats rows. It satisfies both the find_code and the
// content-search ranker interfaces.
type reorderRanker struct {
	order func(n int) []int
}

func reverseOrder(n int) []int {
	order := make([]int, n)
	for index := range order {
		order[index] = n - 1 - index
	}
	return order
}

// lastToFrontOrder moves the final row to the front and keeps the rest in
// order: the row that sits at the budget cut jumps across it.
func lastToFrontOrder(n int) []int {
	order := make([]int, 0, n)
	order = append(order, n-1)
	for index := 0; index < n-1; index++ {
		order = append(order, index)
	}
	return order
}

func (r reorderRanker) Rerank(_ context.Context, _, _ string, rows []map[string]any) ([]map[string]any, bool) {
	if len(rows) < 2 {
		return rows, false
	}
	out := make([]map[string]any, 0, len(rows))
	for _, index := range r.order(len(rows)) {
		out = append(out, rows[index])
	}
	return out, true
}

func (r reorderRanker) RerankEntities(_ context.Context, _, _ string, rows []querycontract.EntityContent) ([]querycontract.EntityContent, bool) {
	if len(rows) < 2 {
		return rows, false
	}
	out := make([]querycontract.EntityContent, 0, len(rows))
	for _, index := range r.order(len(rows)) {
		out = append(out, rows[index])
	}
	return out, true
}

func (r reorderRanker) RerankFiles(_ context.Context, _, _ string, rows []querycontract.FileContent) ([]querycontract.FileContent, bool) {
	return rows, false
}

func rankedBudgetMux(store pagedBudgetStore, ranker reorderRanker) http.Handler {
	mux := http.NewServeMux()
	(&codequery.CodeHandler{
		Profile:      querycontract.ProfileLocalAuthoritative,
		Neo4j:        graph.FakeGraphReader{},
		Content:      store,
		HybridRanker: ranker,
	}).Mount(mux)
	(&contentread.ContentHandler{
		Profile:      querycontract.ProfileLocalAuthoritative,
		Content:      store,
		HybridRanker: ranker,
	}).Mount(mux)
	return mux
}

type rankedWalk struct {
	ids   []string
	pages [][]string
}

// walkRankedPages reads every page of a tool call. After each page it advances
// by next_offset when the page carries one (a budget page) and by the page
// count when the handler truncated at the limit (a plain offset page), the
// two cursors a client has. Each page's ids are recorded.
func walkRankedPages(t *testing.T, handler http.Handler, tool string, limit, budget, window int) rankedWalk {
	t.Helper()
	var walk rankedWalk
	offset := 0
	for pages := 0; ; pages++ {
		if window > 0 && offset >= window {
			return walk
		}
		if pages > 400 {
			t.Fatalf("paging did not terminate, offset=%d", offset)
		}
		result := dispatchBudgetPage(t, handler, tool, map[string]any{
			"query": "decode", "repo_id": sourceCacheBudgetRepoID, "limit": limit, "offset": offset,
		}, budget)
		if result.IsError {
			t.Fatalf("page at offset %d is an error %+v", offset, result.Envelope.Error)
		}
		ids := pageRowIDs(t, result)
		if len(ids) == 0 {
			t.Fatalf("page at offset %d is empty", offset)
		}
		// A page is a window of the offset-ordered rows: the re-rank may order
		// rows inside it, never move a row across the page edge.
		sorted := append([]string(nil), ids...)
		sort.Strings(sorted)
		for index, id := range sorted {
			if want := fmt.Sprintf("entity-%03d", offset+index); id != want {
				t.Fatalf("page at offset %d holds %v, want the rows %d..%d in some order", offset, ids, offset, offset+len(ids)-1)
			}
		}
		walk.pages = append(walk.pages, ids)
		walk.ids = append(walk.ids, ids...)
		data := result.Envelope.Data.(map[string]any)
		if next, ok := data["next_offset"]; ok {
			if numberValue(next) != offset+len(ids) {
				t.Fatalf("next_offset = %v, want %d", next, offset+len(ids))
			}
			offset = numberValue(next)
			continue
		}
		if data["truncated"] != true {
			return walk
		}
		offset += len(ids)
	}
}

// TestRerankedBudgetPagesLoseAndRepeatNoRow is the #7725 review F1 regression.
// With a re-ranker that reorders each window, a walk by next_offset (budget
// pages) and by plain offset must return every row exactly once, in the same
// order on a second walk, for limits below and at the 200-row window.
func TestRerankedBudgetPagesLoseAndRepeatNoRow(t *testing.T) {
	t.Parallel()

	rankers := map[string]reorderRanker{
		"reverse":       {reverseOrder},
		"last_to_front": {lastToFrontOrder},
	}
	cases := []struct {
		name   string
		tool   string
		limit  int
		budget int
		want   int
	}{
		{"content_limit200_budget_pages", "search_entity_content", 200, defaultToolResponseByteBudget, 260},
		{"content_limit25_budget_pages", "search_entity_content", 25, 60 * 1024, 260},
		{"content_limit25_plain_offset", "search_entity_content", 25, defaultToolResponseByteBudget, 260},
		{"find_code_limit200_budget_pages", "find_code", 200, defaultToolResponseByteBudget, 200},
		{"find_code_limit25_budget_pages", "find_code", 25, 60 * 1024, 200},
		{"find_code_limit25_plain_offset", "find_code", 25, defaultToolResponseByteBudget, 200},
	}
	for rankerName, ranker := range rankers {
		for _, tc := range cases {
			t.Run(rankerName+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				handler := rankedBudgetMux(pagedBudgetStore{newSourceCacheStore(newBudgetPageEntities(260))}, ranker)
				first := walkRankedPages(t, handler, tc.tool, tc.limit, tc.budget, tc.want)
				if len(first.ids) != tc.want {
					t.Fatalf("walk returned %d rows, want %d", len(first.ids), tc.want)
				}
				seen := make(map[string]int, len(first.ids))
				for _, id := range first.ids {
					seen[id]++
				}
				for id, count := range seen {
					if count != 1 {
						t.Fatalf("row %s returned %d times, want exactly once", id, count)
					}
				}
				second := walkRankedPages(t, handler, tc.tool, tc.limit, tc.budget, tc.want)
				if fmt.Sprint(first.ids) != fmt.Sprint(second.ids) {
					t.Fatalf("second walk order differs from the first: order is not stable across requests")
				}
				if tc.budget < defaultToolResponseByteBudget && len(first.pages) <= tc.want/tc.limit {
					t.Fatalf("pages = %d, want the budget to force extra trimmed pages", len(first.pages))
				}
			})
		}
	}
}

func syntheticPageResult(tool string, offset int, positions []any) *dispatchResult {
	rows := make([]any, 0, 8)
	for index := 0; index < 8; index++ {
		row := map[string]any{"entity_id": fmt.Sprintf("entity-%03d", index), "pad": index}
		for pad := 0; pad < 40; pad++ {
			row[fmt.Sprintf("field_%02d", pad)] = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
		}
		if positions != nil {
			row["page_position"] = positions[index]
		}
		rows = append(rows, row)
	}
	envelope := &query.ResponseEnvelope{
		Data:  map[string]any{"results": rows, "count": len(rows), "offset": float64(offset), "truncated": true},
		Truth: &querycontract.TruthEnvelope{},
	}
	return &dispatchResult{Value: envelope, Envelope: envelope, ToolName: tool}
}

// TestBudgetPageRefusesAnInconsistentPagePosition proves the trim never cuts a
// re-ranked page at a position it cannot prove: a duplicate, out-of-range or
// partial page_position keeps the over-budget error envelope instead of a page
// that could lose rows.
func TestBudgetPageRefusesAnInconsistentPagePosition(t *testing.T) {
	t.Parallel()

	cases := map[string][]any{
		"duplicate":    {float64(0), float64(0), float64(2), float64(3), float64(4), float64(5), float64(6), float64(7)},
		"out_of_range": {float64(0), float64(1), float64(2), float64(3), float64(4), float64(5), float64(6), float64(8)},
		"fractional":   {float64(0), 1.5, float64(2), float64(3), float64(4), float64(5), float64(6), float64(7)},
		"not_a_number": {float64(0), "1", float64(2), float64(3), float64(4), float64(5), float64(6), float64(7)},
	}
	for name, positions := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, ok := trimToBudgetPage(syntheticPageResult("search_entity_content", 0, positions), "search_entity_content", 3000); ok {
				t.Fatalf("trim accepted an inconsistent page_position %v", positions)
			}
		})
	}
}

// TestBudgetPageLeavesOutANextOffsetThePageRouteWouldReject proves a page cut
// near the content-search offset cap does not advertise a next_offset the
// tool answers with a 400 (#7725 review F6). find_code has no such cap in the
// dispatcher: its page window already bounds the offset.
func TestBudgetPageLeavesOutANextOffsetThePageRouteWouldReject(t *testing.T) {
	t.Parallel()

	nearCap := syntheticPageResult("search_entity_content", query.ContentSearchMaxOffset, nil)
	page, ok := trimToBudgetPage(nearCap, "search_entity_content", 3000)
	if !ok {
		t.Fatalf("trim refused a page that fits one row")
	}
	data := page.result.Envelope.Data.(map[string]any)
	if next, present := data["next_offset"]; present {
		t.Fatalf("next_offset = %v, want none past the %d offset cap", next, query.ContentSearchMaxOffset)
	}
	if data["truncated"] != true {
		t.Fatalf("truncated = %v, want true", data["truncated"])
	}

	inRange := syntheticPageResult("search_entity_content", 100, nil)
	page, ok = trimToBudgetPage(inRange, "search_entity_content", 3000)
	if !ok {
		t.Fatalf("trim refused an in-range page")
	}
	next := page.result.Envelope.Data.(map[string]any)["next_offset"]
	if numberValue(next) != 100+page.rowsReturned {
		t.Fatalf("next_offset = %v, want %d", next, 100+page.rowsReturned)
	}
}

// BenchmarkTrimToBudgetPage measures the trim over the worst response a paged
// tool can return: 200 clipped rows (about 5 KiB each) against the default
// budget. The binary search renders and marshals the page about log2(200)+2
// times; the trim only runs on a response that returned an error before #7725.
func BenchmarkTrimToBudgetPage(b *testing.B) {
	rows := make([]any, 0, 200)
	for _, entity := range newBudgetPageEntities(200) {
		row := querycontract.EntityContentSearchRow(entity)
		querycontract.ClipRowSourceCache(row)
		rows = append(rows, row)
	}
	envelope := &query.ResponseEnvelope{
		Data:  map[string]any{"results": rows, "count": len(rows), "truncated": true},
		Truth: &querycontract.TruthEnvelope{},
	}
	result := &dispatchResult{Value: envelope, Envelope: envelope, ToolName: "search_entity_content", ResourceOnly: true}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := trimToBudgetPage(result, "search_entity_content", defaultToolResponseByteBudget); !ok {
			b.Fatal("trim refused the benchmark page")
		}
	}
}
