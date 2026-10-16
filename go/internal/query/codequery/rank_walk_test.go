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
	"reflect"
	"slices"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
)

// windowRanker scores a walk window so relevance is the reverse of the
// lexical order: the last lexical row is the most relevant.
type windowRanker struct {
	reversingRanker
	disabled bool
	calls    *int
	// err, when set, is what ScoreCodeWindow returns, so a test can drive
	// the scoring-failure branch.
	err error
}

func (r windowRanker) CanScoreWindow() bool { return !r.disabled }

func (r windowRanker) ScoreCodeWindow(_ context.Context, _, _ string, rows []map[string]any) (map[string]float64, error) {
	if r.calls != nil {
		*r.calls++
	}
	if r.err != nil {
		return nil, r.err
	}
	scores := make(map[string]float64, len(rows))
	for i, row := range rows {
		scores[row["entity_id"].(string)] = float64(i + 1)
	}
	return scores, nil
}

var _ codemodel.CodeWindowScorer = windowRanker{}

// switchableGraph serves graphRows while graphOn is true and no rows
// otherwise, so a test can flip the graph lane between pages.
type switchableGraph struct {
	graphOn bool
	rows    []map[string]any
}

func (g *switchableGraph) reader() fakeGraphReader {
	return fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
		if !g.graphOn {
			return nil, nil
		}
		out := make([]map[string]any, 0, len(g.rows))
		for _, row := range g.rows {
			clone := make(map[string]any, len(row))
			for key, value := range row {
				clone[key] = value
			}
			out = append(out, clone)
		}
		return out, nil
	}}
}

func graphRows(count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for index := 0; index < count; index++ {
		rows = append(rows, map[string]any{
			"entity_id": fmt.Sprintf("graph-%03d", index), "name": fmt.Sprintf("decode%03d", index),
			"repo_id": "repo-a", "file_path": fmt.Sprintf("g/%03d.go", index),
		})
	}
	return rows
}

func walkCodeHandler(rows int, ranker codemodel.CodeResultReranker) (*CodeHandler, *recordingCodeSearchContentStore) {
	handler, store, _ := walkCodeHandlerWithGraph(rows, ranker, &switchableGraph{})
	return handler, store
}

func walkCodeHandlerWithGraph(rows int, ranker codemodel.CodeResultReranker, graph *switchableGraph) (*CodeHandler, *recordingCodeSearchContentStore, *switchableGraph) {
	entities := make([]EntityContent, 0, rows)
	for index := 0; index < rows; index++ {
		entities = append(entities, EntityContent{
			RepoID: "repo-a", EntityID: fmt.Sprintf("entity-%03d", index), EntityName: "decode", EntityType: "Function",
			SourceCache: fmt.Sprintf("func decode%03d() {}", index),
		})
	}
	store := &recordingCodeSearchContentStore{byRepo: map[string][]EntityContent{"repo-a": entities}}
	return &CodeHandler{
		Neo4j:        graph.reader(),
		Content:      store,
		Profile:      ProfileLocalAuthoritative,
		HybridRanker: ranker,
	}, store, graph
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
		next, _ := walk["next_cursor"].(string)
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

func TestCodeSearchRankWalkMarksRowsOnTheContentLane(t *testing.T) {
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
	if walk["position"] != float64(10) || walk["window_rows"] != float64(30) ||
		walk["lane"] != rank.LaneContent || walk["order"] != rank.OrderHybrid || walk["ranked"] != true {
		t.Fatalf("rank_walk = %v", walk)
	}
}

func TestCodeSearchRankWalkBasisChangeIsConflict(t *testing.T) {
	t.Parallel()
	handler, store := walkCodeHandler(30, windowRanker{reversingRanker: reversingRanker{applied: true}})
	_, first := postCodeSearch(t, handler, map[string]any{
		"query": "decode", "repo_id": "repo-a", "limit": 10, "rank_cursor": rank.StartToken,
	})
	next := first["rank_walk"].(map[string]any)["next_cursor"].(string)
	store.byRepo["repo-a"] = store.byRepo["repo-a"][1:]
	recorder, _ := postCodeSearch(t, handler, map[string]any{
		"query": "decode", "repo_id": "repo-a", "limit": 10, "rank_cursor": next,
	})
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), rank.ReasonBasisChanged) {
		t.Fatalf("status = %d body = %s, want 409 %s", recorder.Code, recorder.Body.String(), rank.ReasonBasisChanged)
	}
}

func TestCodeSearchRankWalkRefusesRequestsItCannotServe(t *testing.T) {
	t.Parallel()
	enabled := windowRanker{reversingRanker: reversingRanker{applied: true}}
	cases := []struct {
		name   string
		body   map[string]any
		status int
		reason string
	}{
		{"offset", map[string]any{"query": "decode", "repo_id": "repo-a", "offset": 10, "rank_cursor": "start"}, http.StatusBadRequest, rank.ReasonOffsetConflict},
		{"no repository", map[string]any{"query": "decode", "rank_cursor": "start"}, http.StatusBadRequest, rank.ReasonScopeNotSingleRepo},
		{"garbage cursor", map[string]any{"query": "decode", "repo_id": "repo-a", "rank_cursor": "zzz"}, http.StatusBadRequest, rank.ReasonMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler, _ := walkCodeHandler(5, enabled)
			recorder, _ := postCodeSearch(t, handler, tc.body)
			if recorder.Code != tc.status || !strings.Contains(recorder.Body.String(), tc.reason) {
				t.Fatalf("status = %d body = %s, want %d %s", recorder.Code, recorder.Body.String(), tc.status, tc.reason)
			}
		})
	}
}

// TestCodeSearchRankWalkWithoutRankingServesTheLexicalOrder pins the
// content-lane walks that have no ranker to apply: exact=true, no ranker, a
// page-only ranker, and a disabled ranker. Each pages the content window in
// its own order (order=lexical, ranked=false), the order an offset walk reads.
func TestCodeSearchRankWalkWithoutRankingServesTheLexicalOrder(t *testing.T) {
	t.Parallel()
	enabled := windowRanker{reversingRanker: reversingRanker{applied: true}}
	cases := []struct {
		name   string
		ranker codemodel.CodeResultReranker
		exact  bool
	}{
		{"exact", enabled, true},
		{"no ranker", nil, false},
		{"page-only ranker", reversingRanker{applied: true}, false},
		{"disabled ranker", windowRanker{disabled: true}, false},
		{"real disabled ranker", codemodel.NewCodeHybridRanker(false), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler, _ := walkCodeHandler(12, tc.ranker)
			recorder, response := postCodeSearch(t, handler, map[string]any{
				"query": "decode", "repo_id": "repo-a", "limit": 5, "exact": tc.exact, "rank_cursor": "start",
			})
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
			}
			walk := response["rank_walk"].(map[string]any)
			if walk["lane"] != rank.LaneContent || walk["order"] != rank.OrderLexical || walk["ranked"] != false {
				t.Fatalf("rank_walk = %v, want content lane, lexical order, not ranked", walk)
			}
			want := []string{"entity-000", "entity-001", "entity-002", "entity-003", "entity-004"}
			if got := resultIDs(t, response); !slices.Equal(got, want) {
				t.Fatalf("rows = %v, want the window order %v", got, want)
			}
		})
	}
}

// TestFindCodeRankWalkPinsLaneAndRejectsLaneFlip is the #7746 fork-6 ruling:
// page 1 picks the lane exactly as an offset request does, the cursor pins
// it, and a lane change between pages is a 409 basis_changed.
func TestFindCodeRankWalkPinsLaneAndRejectsLaneFlip(t *testing.T) {
	t.Parallel()
	ranker := windowRanker{reversingRanker: reversingRanker{applied: true}}
	walkBody := func(token string) map[string]any {
		return map[string]any{"query": "decode", "repo_id": "repo-a", "limit": 10, "rank_cursor": token}
	}
	plainBody := func(offset int) map[string]any {
		return map[string]any{"query": "decode", "repo_id": "repo-a", "limit": 10, "offset": offset}
	}

	t.Run("graph lane", func(t *testing.T) {
		t.Parallel()
		handler, _, graph := walkCodeHandlerWithGraph(40, ranker, &switchableGraph{graphOn: true, rows: graphRows(30)})
		_, plain := postCodeSearch(t, handler, plainBody(0))
		if plain["source"] != "graph" {
			t.Fatalf("plain call source = %v, want graph (parity)", plain["source"])
		}
		recorder, first := postCodeSearch(t, handler, walkBody(rank.StartToken))
		if recorder.Code != http.StatusOK {
			t.Fatalf("page 1 status = %d body = %s", recorder.Code, recorder.Body.String())
		}
		walk := first["rank_walk"].(map[string]any)
		if walk["lane"] != rank.LaneGraph || walk["order"] != rank.OrderGraphName || walk["ranked"] != false {
			t.Fatalf("page 1 rank_walk = %v, want graph lane, graph_name order", walk)
		}
		if first["source"] != "graph" || !reflect.DeepEqual(first["results"], plain["results"]) {
			t.Fatalf("page 1 rows differ from the plain offset-0 call:\n got %v\nwant %v", first["results"], plain["results"])
		}
		_, plain2 := postCodeSearch(t, handler, plainBody(10))
		recorder, second := postCodeSearch(t, handler, walkBody(walk["next_cursor"].(string)))
		if recorder.Code != http.StatusOK || !slices.Equal(resultIDs(t, second), resultIDs(t, plain2)) {
			t.Fatalf("page 2 status %d rows %v, want the plain offset-10 rows %v", recorder.Code, resultIDs(t, second), resultIDs(t, plain2))
		}
		graph.graphOn = false
		recorder, _ = postCodeSearch(t, handler, walkBody(second["rank_walk"].(map[string]any)["next_cursor"].(string)))
		if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), rank.ReasonBasisChanged) {
			t.Fatalf("page 3 after the graph emptied: status %d body %s, want 409 %s", recorder.Code, recorder.Body.String(), rank.ReasonBasisChanged)
		}
	})

	t.Run("content lane", func(t *testing.T) {
		t.Parallel()
		handler, _, graph := walkCodeHandlerWithGraph(40, ranker, &switchableGraph{rows: graphRows(30)})
		recorder, first := postCodeSearch(t, handler, walkBody(rank.StartToken))
		walk, _ := first["rank_walk"].(map[string]any)
		if recorder.Code != http.StatusOK || walk["lane"] != rank.LaneContent || walk["order"] != rank.OrderHybrid {
			t.Fatalf("page 1 status %d rank_walk %v, want content lane, hybrid order", recorder.Code, walk)
		}
		graph.graphOn = true
		recorder, _ = postCodeSearch(t, handler, walkBody(walk["next_cursor"].(string)))
		if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), rank.ReasonBasisChanged) {
			t.Fatalf("page 2 after the graph filled: status %d body %s, want 409 %s", recorder.Code, recorder.Body.String(), rank.ReasonBasisChanged)
		}
	})

	t.Run("no graph configured", func(t *testing.T) {
		t.Parallel()
		handler, _ := walkCodeHandler(20, ranker)
		handler.Neo4j = nil
		recorder, first := postCodeSearch(t, handler, walkBody(rank.StartToken))
		walk, _ := first["rank_walk"].(map[string]any)
		if recorder.Code != http.StatusOK || walk["lane"] != rank.LaneContent {
			t.Fatalf("status %d rank_walk %v, want the content lane when no graph is configured", recorder.Code, walk)
		}
	})
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

// TestCodeSearchRankWalkScoringFailureIsAFixedServerError pins the
// scoring-failure branch: a timed-out window score answers 500 with the fixed
// message, carries no rank_walk block, and records no served outcome. It
// never falls back to a lexical page, which would change the walk's order
// between pages.
func TestCodeSearchRankWalkScoringFailureIsAFixedServerError(t *testing.T) {
	t.Parallel()
	handler, _ := walkCodeHandler(20, windowRanker{reversingRanker: reversingRanker{applied: true}, err: context.DeadlineExceeded})
	recorder, spans := postCodeSearchTraced(t, handler, map[string]any{
		"query": "decode", "repo_id": "repo-a", "limit": 5, "rank_cursor": rank.StartToken,
	})
	body := recorder.Body.String()
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(body, codeSearchRankFailedMessage) {
		t.Fatalf("status = %d body = %s, want 500 %q", recorder.Code, body, codeSearchRankFailedMessage)
	}
	if strings.Contains(body, "rank_walk") || strings.Contains(body, "deadline") {
		t.Fatalf("body = %s, want the fixed message only", body)
	}
	assertNoServedOutcome(t, spans)
}

// postCodeSearchTraced runs one find_code request under a recording request
// span and returns the ended spans.
func postCodeSearchTraced(t *testing.T, handler *CodeHandler, body map[string]any) (*httptest.ResponseRecorder, []sdktrace.ReadOnlySpan) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := provider.Tracer("test").Start(context.Background(), "request")
	response := httptest.NewRecorder()
	handler.handleSearch(response, httptest.NewRequest(http.MethodPost, "/api/v0/code/search", bytes.NewReader(encoded)).WithContext(ctx))
	span.End()
	return response, recorder.Ended()
}

// assertNoServedOutcome fails when the request span recorded a served or
// window_end rank-walk outcome.
func assertNoServedOutcome(t *testing.T, spans []sdktrace.ReadOnlySpan) {
	t.Helper()
	for _, span := range spans {
		for _, kv := range span.Attributes() {
			if kv.Key == rank.AttrOutcome && (kv.Value.AsString() == rank.OutcomeServed || kv.Value.AsString() == rank.OutcomeWindowEnd) {
				t.Fatalf("span %s recorded %s=%s on a failed walk", span.Name(), kv.Key, kv.Value.AsString())
			}
		}
	}
}
