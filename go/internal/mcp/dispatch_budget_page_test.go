// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/contentread"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// pagedBudgetStore adds the offset-aware entity search the real Postgres
// content store has, over the same oversized rows the source_cache budget
// fixtures use.
type pagedBudgetStore struct {
	*sourceCacheBudgetStore
}

func (s pagedBudgetStore) SearchEntities(
	_ context.Context,
	_ string,
	_ []string,
	_ string,
	limit, offset int,
) ([]querycontract.EntityContent, error) {
	if offset > len(s.entities) {
		offset = len(s.entities)
	}
	end := offset + limit
	if end > len(s.entities) {
		end = len(s.entities)
	}
	return s.entities[offset:end], nil
}

func (s pagedBudgetStore) SearchFiles(context.Context, string, []string, string, int, int) ([]querycontract.FileContent, error) {
	return nil, nil
}

func pagedBudgetMux(store pagedBudgetStore) http.Handler {
	mux := http.NewServeMux()
	(&codequery.CodeHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Neo4j:   graph.FakeGraphReader{},
		Content: store,
	}).Mount(mux)
	(&contentread.ContentHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Content: store,
	}).Mount(mux)
	return mux
}

// newBudgetPageEntities builds count rows shaped like a common-name hit: a
// source body and a docstring that the read-time clips cut to 4,096 and 512
// bytes, so each row still carries several KiB after the clips.
func newBudgetPageEntities(count int) []querycontract.EntityContent {
	entities := make([]querycontract.EntityContent, 0, count)
	for index := 0; index < count; index++ {
		entities = append(entities, querycontract.EntityContent{
			EntityID:     fmt.Sprintf("entity-%03d", index),
			RepoID:       sourceCacheBudgetRepoID,
			RelativePath: fmt.Sprintf("src/decode%03d.php", index),
			EntityType:   "Function",
			EntityName:   "decode",
			StartLine:    10,
			EndLine:      40,
			Language:     "php",
			SourceCache:  strings.Repeat("a", sourceCacheBudgetTypicalLen),
			Metadata:     map[string]any{"docstring": strings.Repeat("d", 2048)},
		})
	}
	return entities
}

var budgetPageCalls = []struct {
	tool string
	args func(offset int) map[string]any
}{
	{"search_entity_content", func(offset int) map[string]any {
		return map[string]any{"query": "decode", "repo_id": sourceCacheBudgetRepoID, "limit": 200, "offset": offset}
	}},
	{"find_code", func(offset int) map[string]any {
		return map[string]any{"query": "decode", "repo_id": sourceCacheBudgetRepoID, "limit": 200, "offset": offset}
	}},
}

func dispatchBudgetPage(t *testing.T, handler http.Handler, tool string, args map[string]any, budget int) *dispatchResult {
	t.Helper()
	result, err := dispatchToolWithOptions(
		context.Background(), handler, tool, args, "",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		dispatchOptions{responseByteBudget: budget},
	)
	if err != nil {
		t.Fatalf("dispatch %s error = %v, want nil", tool, err)
	}
	return result
}

func pageRowIDs(t *testing.T, result *dispatchResult) []string {
	t.Helper()
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", result.Envelope.Data)
	}
	rows, _ := data["results"].([]any)
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.(map[string]any)["entity_id"].(string))
	}
	return ids
}

// TestBudgetPageAtAdvertisedMaximumReturnsAPage is the #7725 regression: a
// schema-valid request at limit 200 over rows that cannot all fit the response
// budget returns a success page of whole rows with the truncation markers and
// the offset that reaches the remainder, not mcp_response_over_budget.
func TestBudgetPageAtAdvertisedMaximumReturnsAPage(t *testing.T) {
	t.Parallel()

	for _, call := range budgetPageCalls {
		t.Run(call.tool, func(t *testing.T) {
			t.Parallel()

			handler := pagedBudgetMux(pagedBudgetStore{newSourceCacheStore(newBudgetPageEntities(260))})
			result := dispatchBudgetPage(t, handler, call.tool, call.args(0), defaultToolResponseByteBudget)
			if result.IsError {
				t.Fatalf("limit 200 reply is an error %+v, want a budget page", result.Envelope.Error)
			}
			ids := pageRowIDs(t, result)
			if len(ids) == 0 || len(ids) >= 200 {
				t.Fatalf("page rows = %d, want a non-empty page smaller than the 200 requested", len(ids))
			}
			if size := estimateResponseBytes(result); size > defaultToolResponseByteBudget {
				t.Fatalf("page is %d bytes, want <= %d", size, defaultToolResponseByteBudget)
			}
			data := result.Envelope.Data.(map[string]any)
			if data["truncated"] != true {
				t.Fatalf("truncated = %v, want true", data["truncated"])
			}
			if got := numberValue(data["count"]); got != len(ids) {
				t.Fatalf("count = %d, want %d", got, len(ids))
			}
			if got := numberValue(data["next_offset"]); got != len(ids) {
				t.Fatalf("next_offset = %v, want %d", data["next_offset"], len(ids))
			}
			page, _ := data["budget_page"].(map[string]any)
			if numberValue(page["rows_returned"]) != len(ids) || numberValue(page["budget_bytes"]) != defaultToolResponseByteBudget ||
				numberValue(page["rows_available"]) <= len(ids) || page["reason"] != "response_byte_budget" {
				t.Fatalf("budget_page = %#v, want rows_returned %d, the budget, more rows available, reason response_byte_budget", page, len(ids))
			}
			if !result.Envelope.Truth.Truncated {
				t.Fatalf("truth.truncated = false, want true on a budget page")
			}
			omissions := result.Envelope.Truth.Omissions
			if len(omissions) != 1 || omissions[0].Section != "results" || omissions[0].Detail != "response_byte_budget" ||
				omissions[0].Total != numberValue(page["rows_available"]) {
				t.Fatalf("truth.omissions = %#v, want one results/response_byte_budget entry", omissions)
			}
			if got := numberValue(data["source_cache_clipped_rows"]); got != len(ids) {
				t.Fatalf("source_cache_clipped_rows = %d, want %d (recounted over the kept rows)", got, len(ids))
			}
			if got := numberValue(data["docstring_clipped_rows"]); got != len(ids) {
				t.Fatalf("docstring_clipped_rows = %d, want %d (recounted over the kept rows)", got, len(ids))
			}
		})
	}
}

// TestBudgetPageOffsetsReachTheRemainderWithoutLossOrRepeat walks every page
// by next_offset and requires the concatenated ids to equal the ranked rows
// exactly: no row lost, repeated, or split across a page boundary.
func TestBudgetPageOffsetsReachTheRemainderWithoutLossOrRepeat(t *testing.T) {
	t.Parallel()

	for _, call := range budgetPageCalls {
		t.Run(call.tool, func(t *testing.T) {
			t.Parallel()

			entities := newBudgetPageEntities(260)
			// find_code pages inside its 200-row ranked window; the content
			// search pages through every row.
			want := len(entities)
			if call.tool == "find_code" {
				want = 200
			}
			handler := pagedBudgetMux(pagedBudgetStore{newSourceCacheStore(entities)})
			var got []string
			offset, pages := 0, 0
			for {
				pages++
				if pages > 80 {
					t.Fatalf("paging did not terminate after %d pages, offset=%d", pages, offset)
				}
				result := dispatchBudgetPage(t, handler, call.tool, call.args(offset), defaultToolResponseByteBudget)
				if result.IsError {
					t.Fatalf("page at offset %d is an error %+v", offset, result.Envelope.Error)
				}
				ids := pageRowIDs(t, result)
				if len(ids) == 0 {
					t.Fatalf("page at offset %d is empty", offset)
				}
				got = append(got, ids...)
				next, ok := result.Envelope.Data.(map[string]any)["next_offset"]
				if !ok {
					break
				}
				if numberValue(next) != offset+len(ids) {
					t.Fatalf("next_offset = %v, want %d", next, offset+len(ids))
				}
				offset = numberValue(next)
			}
			if len(got) != want {
				t.Fatalf("paged rows = %d, want %d", len(got), want)
			}
			for index, id := range got {
				if id != fmt.Sprintf("entity-%03d", index) {
					t.Fatalf("row %d = %s, want entity-%03d (lost, repeated, or reordered)", index, id, index)
				}
			}
			if pages < 3 {
				t.Fatalf("pages = %d, want the fixture to need at least 3", pages)
			}
		})
	}
}

// TestBudgetPageSingleOversizedRowKeepsTheOverBudgetEnvelope proves the error
// stays for the one case a page cannot help: the first row alone is over the
// budget.
func TestBudgetPageSingleOversizedRowKeepsTheOverBudgetEnvelope(t *testing.T) {
	t.Parallel()

	for _, call := range budgetPageCalls {
		t.Run(call.tool, func(t *testing.T) {
			t.Parallel()

			entities := newBudgetPageEntities(5)
			// metadata other than the docstring is not clipped, so this row
			// alone exceeds the budget.
			entities[0].Metadata = map[string]any{"blob": strings.Repeat("x", 2*defaultToolResponseByteBudget)}
			handler := pagedBudgetMux(pagedBudgetStore{newSourceCacheStore(entities)})
			result := dispatchBudgetPage(t, handler, call.tool, call.args(0), defaultToolResponseByteBudget)
			if !result.IsError || result.Envelope == nil || result.Envelope.Error == nil ||
				result.Envelope.Error.Code != errorCodeResponseOverBudget {
				t.Fatalf("result = %+v, want the mcp_response_over_budget error envelope", result)
			}
		})
	}
}

// TestBudgetPageLeavesFittingResponsesUnchanged proves a response that fits,
// including one the handler itself truncated at the limit, is byte-identical
// to the unbudgeted reply and carries no budget-page marker.
func TestBudgetPageLeavesFittingResponsesUnchanged(t *testing.T) {
	t.Parallel()

	for _, call := range budgetPageCalls {
		t.Run(call.tool, func(t *testing.T) {
			t.Parallel()

			handler := pagedBudgetMux(pagedBudgetStore{newSourceCacheStore(newBudgetPageEntities(30))})
			args := call.args(0)
			args["limit"] = 4
			budgeted := dispatchBudgetPage(t, handler, call.tool, args, defaultToolResponseByteBudget)
			plain := dispatchBudgetPage(t, handler, call.tool, args, 0)
			gotBytes, _ := json.Marshal(renderToolResult(call.tool, budgeted))
			wantBytes, _ := json.Marshal(renderToolResult(call.tool, plain))
			if string(gotBytes) != string(wantBytes) {
				t.Fatalf("fitting response changed under the budget guard")
			}
			data := budgeted.Envelope.Data.(map[string]any)
			if _, present := data["budget_page"]; present {
				t.Fatalf("fitting response carries budget_page %v", data["budget_page"])
			}
			if _, present := data["next_offset"]; present {
				t.Fatalf("fitting response carries next_offset %v", data["next_offset"])
			}
			if budgeted.Envelope.Truth.Truncated {
				t.Fatalf("fitting response carries truth.truncated")
			}
			if len(budgeted.Envelope.Truth.Omissions) != 0 {
				t.Fatalf("fitting response carries truth.omissions %#v", budgeted.Envelope.Truth.Omissions)
			}
		})
	}
}
