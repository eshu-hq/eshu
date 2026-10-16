// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
)

// windowRanker scores a walk window so relevance is the reverse of the
// lexical order: the last lexical row is the most relevant.
type windowRanker struct {
	reversingRanker
	disabled bool
	calls    *int
}

func (r windowRanker) CanScoreWindow() bool { return !r.disabled }

func (r windowRanker) ScoreCodeWindow(_ context.Context, _, _ string, rows []map[string]any) (map[string]float64, error) {
	if r.calls != nil {
		*r.calls++
	}
	scores := make(map[string]float64, len(rows))
	for i, row := range rows {
		scores[row["entity_id"].(string)] = float64(i + 1)
	}
	return scores, nil
}

var _ codemodel.CodeWindowScorer = windowRanker{}

func walkCodeHandler(rows int, ranker codemodel.CodeResultReranker) (*CodeHandler, *recordingCodeSearchContentStore) {
	entities := make([]EntityContent, 0, rows)
	for index := 0; index < rows; index++ {
		entities = append(entities, EntityContent{
			RepoID: "repo-a", EntityID: fmt.Sprintf("entity-%03d", index), EntityName: "decode", EntityType: "Function",
			SourceCache: fmt.Sprintf("func decode%03d() {}", index),
		})
	}
	store := &recordingCodeSearchContentStore{byRepo: map[string][]EntityContent{"repo-a": entities}}
	return &CodeHandler{
		// The graph lane has rows; a walk must still read the content lane.
		Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{{"entity_id": "graph-1", "name": "decode", "repo_id": "repo-a"}}, nil
		}},
		Content:      store,
		Profile:      ProfileLocalAuthoritative,
		HybridRanker: ranker,
	}, store
}

func postCodeSearch(t *testing.T, handler *CodeHandler, body map[string]any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.handleSearch(recorder, httptest.NewRequest(http.MethodPost, "/api/v0/code/search", bytes.NewReader(encoded)))
	var response map[string]any
	if recorder.Code == http.StatusOK {
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return recorder, response
}

func walkCode(t *testing.T, handler *CodeHandler, limit int) []string {
	t.Helper()
	var served []string
	token := rank.StartToken
	for page := 0; page < 100; page++ {
		recorder, response := postCodeSearch(t, handler, map[string]any{
			"query": "decode", "repo_id": "repo-a", "limit": limit, "rank_cursor": token,
		})
		if recorder.Code != http.StatusOK {
			t.Fatalf("page %d status = %d body = %s", page, recorder.Code, recorder.Body.String())
		}
		served = append(served, resultIDs(t, response)...)
		walk, _ := response["rank_walk"].(map[string]any)
		next, _ := walk["next_rank_cursor"].(string)
		if next == "" {
			if walk["window_end"] != true {
				t.Fatalf("page %d ended without window_end: %v", page, walk)
			}
			return served
		}
		token = next
	}
	t.Fatal("walk did not end")
	return nil
}

func TestCodeSearchRankWalkSpansPagesInGlobalOrder(t *testing.T) {
	t.Parallel()
	calls := 0
	handler, _ := walkCodeHandler(90, windowRanker{reversingRanker: reversingRanker{applied: true}, calls: &calls})
	served := walkCode(t, handler, 25)
	want := make([]string, 0, 90)
	for index := 89; index >= 0; index-- {
		want = append(want, fmt.Sprintf("entity-%03d", index))
	}
	if !slices.Equal(served, want) {
		t.Fatalf("walk order:\n got %v\nwant %v", served, want)
	}
	if calls != 4 {
		t.Fatalf("window scored %d times, want once per page (4)", calls)
	}
}

func TestCodeSearchRankWalkMarksRowsAndSkipsTheGraphLane(t *testing.T) {
	t.Parallel()
	handler, _ := walkCodeHandler(30, windowRanker{reversingRanker: reversingRanker{applied: true}})
	recorder, response := postCodeSearch(t, handler, map[string]any{
		"query": "decode", "repo_id": "repo-a", "limit": 10, "rank_cursor": rank.StartToken,
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if response["source"] != "content" || response["source_backend"] != "hybrid_content_store" || response["truncated"] != true {
		t.Fatalf("source %v backend %v truncated %v, want content hybrid_content_store true",
			response["source"], response["source_backend"], response["truncated"])
	}
	for _, item := range response["results"].([]any) {
		row := item.(map[string]any)
		if row["search_backend"] != "hybrid" {
			t.Fatalf("row %v lacks search_backend=hybrid", row)
		}
		if _, present := row["page_position"]; present {
			t.Fatalf("walk row %v carries page_position", row)
		}
	}
	walk := response["rank_walk"].(map[string]any)
	if walk["served"] != float64(10) || walk["window_rows"] != float64(30) {
		t.Fatalf("rank_walk = %v", walk)
	}
}

func TestCodeSearchRankWalkBasisChangeIsConflict(t *testing.T) {
	t.Parallel()
	handler, store := walkCodeHandler(30, windowRanker{reversingRanker: reversingRanker{applied: true}})
	_, first := postCodeSearch(t, handler, map[string]any{
		"query": "decode", "repo_id": "repo-a", "limit": 10, "rank_cursor": rank.StartToken,
	})
	next := first["rank_walk"].(map[string]any)["next_rank_cursor"].(string)
	store.byRepo["repo-a"] = store.byRepo["repo-a"][1:]
	recorder, _ := postCodeSearch(t, handler, map[string]any{
		"query": "decode", "repo_id": "repo-a", "limit": 10, "rank_cursor": next,
	})
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), rank.ReasonCandidateSetChanged) {
		t.Fatalf("status = %d body = %s, want 409 %s", recorder.Code, recorder.Body.String(), rank.ReasonCandidateSetChanged)
	}
}

func TestCodeSearchRankWalkRefusesRequestsItCannotServe(t *testing.T) {
	t.Parallel()
	enabled := windowRanker{reversingRanker: reversingRanker{applied: true}}
	cases := []struct {
		name   string
		ranker codemodel.CodeResultReranker
		body   map[string]any
		status int
		reason string
	}{
		{"exact", enabled, map[string]any{"query": "decode", "repo_id": "repo-a", "exact": true, "rank_cursor": "start"}, http.StatusBadRequest, rank.ReasonExactSearch},
		{"offset", enabled, map[string]any{"query": "decode", "repo_id": "repo-a", "offset": 10, "rank_cursor": "start"}, http.StatusBadRequest, rank.ReasonOffsetConflict},
		{"no repository", enabled, map[string]any{"query": "decode", "rank_cursor": "start"}, http.StatusBadRequest, rank.ReasonScopeNotSingleRepo},
		{"no ranker", nil, map[string]any{"query": "decode", "repo_id": "repo-a", "rank_cursor": "start"}, http.StatusBadRequest, rank.ReasonRankerDisabled},
		{"page-only ranker", reversingRanker{applied: true}, map[string]any{"query": "decode", "repo_id": "repo-a", "rank_cursor": "start"}, http.StatusBadRequest, rank.ReasonRankerDisabled},
		{"disabled ranker", windowRanker{disabled: true}, map[string]any{"query": "decode", "repo_id": "repo-a", "rank_cursor": "start"}, http.StatusBadRequest, rank.ReasonRankerDisabled},
		{"real disabled ranker", codemodel.NewCodeHybridRanker(false), map[string]any{"query": "decode", "repo_id": "repo-a", "rank_cursor": "start"}, http.StatusBadRequest, rank.ReasonRankerDisabled},
		{"garbage cursor", enabled, map[string]any{"query": "decode", "repo_id": "repo-a", "rank_cursor": "zzz"}, http.StatusBadRequest, rank.ReasonMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler, _ := walkCodeHandler(5, tc.ranker)
			recorder, _ := postCodeSearch(t, handler, tc.body)
			if recorder.Code != tc.status || !strings.Contains(recorder.Body.String(), tc.reason) {
				t.Fatalf("status = %d body = %s, want %d %s", recorder.Code, recorder.Body.String(), tc.status, tc.reason)
			}
		})
	}
}

func TestCodeSearchRankWalkWithTheShippedRanker(t *testing.T) {
	t.Parallel()
	handler, _ := walkCodeHandler(60, codemodel.NewCodeHybridRanker(true))
	served := walkCode(t, handler, 25)
	if len(served) != 60 {
		t.Fatalf("walk served %d rows, want 60", len(served))
	}
	seen := map[string]bool{}
	for _, id := range served {
		if seen[id] {
			t.Fatalf("row %s served twice", id)
		}
		seen[id] = true
	}
}

// TestCodeSearchOffsetPathBytesUnchanged pins the response bytes of an
// offset-paged find_code content-fallback page the re-rank reordered. The
// want string was captured from main at 8f188353f, before the walk existed.
func TestCodeSearchOffsetPathBytesUnchanged(t *testing.T) {
	t.Parallel()
	handler := rankedFallbackHandler(12, reversingRanker{applied: true})
	recorder := httptest.NewRecorder()
	handler.handleSearch(recorder, httptest.NewRequest(http.MethodPost, "/api/v0/code/search",
		strings.NewReader(`{"query":"decode","repo_id":"repo-a","limit":3,"offset":3}`)))
	if got := recorder.Body.String(); got != codeOffsetPathGolden {
		t.Fatalf("offset response bytes changed:\n got %s\nwant %s", got, codeOffsetPathGolden)
	}
}

const codeOffsetPathGolden = `{"count":3,"docstring_clip_bytes":512,"docstring_clipped_rows":0,"limit":3,"offset":3,"query":"decode","repo_id":"repo-a","results":[{"end_line":0,"entity_id":"entity-005","entity_name":"decode","entity_type":"Function","file_path":"","language":"","metadata":null,"page_position":2,"repo_id":"repo-a","source_cache":"","start_line":0},{"end_line":0,"entity_id":"entity-004","entity_name":"decode","entity_type":"Function","file_path":"","language":"","metadata":null,"page_position":1,"repo_id":"repo-a","source_cache":"","start_line":0},{"end_line":0,"entity_id":"entity-003","entity_name":"decode","entity_type":"Function","file_path":"","language":"","metadata":null,"page_position":0,"repo_id":"repo-a","source_cache":"","start_line":0}],"source":"content","source_backend":"hybrid_content_store","source_cache_clip_bytes":4096,"source_cache_clipped_rows":0,"truncated":true}` + "\n"
