// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/contentread"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

func rankWalkBudgetMux(store pagedBudgetStore) http.Handler {
	mux := http.NewServeMux()
	(&codequery.CodeHandler{
		Profile:      querycontract.ProfileLocalAuthoritative,
		Neo4j:        graph.FakeGraphReader{},
		Content:      store,
		HybridRanker: codemodel.NewCodeHybridRanker(true),
	}).Mount(mux)
	(&contentread.ContentHandler{
		Profile:      querycontract.ProfileLocalAuthoritative,
		Content:      store,
		HybridRanker: contentread.NewContentHybridRanker(true),
	}).Mount(mux)
	return mux
}

// TestRankWalkBudgetPagesKeepTheCursorIntact is the MCP half of #7746: a rank
// walk whose pages exceed the response budget is cut to a prefix of the
// ranked page, and the trimmed reply's next_rank_cursor resumes at the first
// dropped row. Every window row is read exactly once and the trim never
// emits next_offset, which would mix offset paging into the walk.
func TestRankWalkBudgetPagesKeepTheCursorIntact(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"find_code", "search_entity_content"} {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			handler := rankWalkBudgetMux(pagedBudgetStore{newSourceCacheStore(newBudgetPageEntities(260))})
			seen := map[string]int{}
			token := rank.StartToken
			pages, trimmed := 0, 0
			for ; ; pages++ {
				if pages > 400 {
					t.Fatal("walk did not end")
				}
				result := dispatchBudgetPage(t, handler, tool, map[string]any{
					"query": "decode", "repo_id": sourceCacheBudgetRepoID, "limit": 50, "rank_cursor": token,
				}, 60*1024)
				if result.IsError {
					t.Fatalf("page %d is an error %+v", pages, result.Envelope.Error)
				}
				data := result.Envelope.Data.(map[string]any)
				if _, present := data["next_offset"]; present {
					t.Fatalf("rank page %d carries next_offset: %v", pages, data["next_offset"])
				}
				if _, present := data["budget_page"]; present {
					trimmed++
				}
				for _, id := range pageRowIDs(t, result) {
					seen[id]++
				}
				walk, ok := data[rank.BlockKey].(map[string]any)
				if !ok {
					t.Fatalf("page %d has no rank_walk block: %v", pages, data)
				}
				next, _ := walk["next_rank_cursor"].(string)
				if next == "" {
					break
				}
				token = next
			}
			if len(seen) != rank.WindowSize {
				t.Fatalf("walk read %d distinct rows, want the %d-row window", len(seen), rank.WindowSize)
			}
			for id, count := range seen {
				if count != 1 {
					t.Fatalf("row %s read %d times, want exactly once", id, count)
				}
			}
			if trimmed == 0 {
				t.Fatalf("no page was budget-trimmed; the test does not exercise the trim")
			}
		})
	}
}

// TestRankWalkBudgetTrimRefusesABrokenWalkBlock proves a rank page whose
// walk block cannot prove the trimmed cursor keeps the over-budget error
// instead of a page that would skip rows.
func TestRankWalkBudgetTrimRefusesABrokenWalkBlock(t *testing.T) {
	t.Parallel()
	result := syntheticPageResult("find_code", 0, nil)
	result.Envelope.Data.(map[string]any)[rank.BlockKey] = map[string]any{"page_cursor": "garbage"}
	if _, ok := trimToBudgetPage(result, "find_code", 3000); ok {
		t.Fatal("trim accepted a rank page with a malformed page cursor")
	}
}

// TestRankCursorIsAdvertisedOnTheWalkTools pins the #7746 tool schema: the
// two walk tools advertise rank_cursor as a string, and search_file_content
// does not.
func TestRankCursorIsAdvertisedOnTheWalkTools(t *testing.T) {
	t.Parallel()
	advertised := map[string]bool{}
	for _, tool := range ReadOnlyTools() {
		properties, _ := tool.InputSchema.(map[string]any)["properties"].(map[string]any)
		if field, ok := properties["rank_cursor"].(map[string]any); ok {
			if field["type"] != "string" {
				t.Fatalf("%s rank_cursor type = %v, want string", tool.Name, field["type"])
			}
			advertised[tool.Name] = true
		}
	}
	if !advertised["find_code"] || !advertised["search_entity_content"] || advertised["search_file_content"] || len(advertised) != 2 {
		t.Fatalf("rank_cursor advertised on %v, want exactly find_code and search_entity_content", advertised)
	}
}
