// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

const walkRepoID = "repo-walk"

// walkRows returns count entity rows in lexical order. Relevance grows with
// the row index: row i repeats the query terms i/10 times, so the most
// relevant rows sit at the end of the lexical order.
func walkRows(count int) []querycontract.EntityContent {
	rows := make([]querycontract.EntityContent, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, querycontract.EntityContent{
			EntityID:     fmt.Sprintf("entity-%03d", i),
			RepoID:       walkRepoID,
			RelativePath: fmt.Sprintf("svc/handler_%03d.go", i),
			EntityType:   "function",
			EntityName:   fmt.Sprintf("handler%03d", i),
			Language:     "go",
			SourceCache:  "func body order invoice " + strings.Repeat("payment refund ", i/10),
		})
	}
	return rows
}

func walkHandler(store querycontract.ContentStore) *ContentHandler {
	return &ContentHandler{
		Content:      store,
		Profile:      querycontract.ProfileLocalAuthoritative,
		HybridRanker: NewContentHybridRanker(true),
	}
}

func walkContext() context.Context {
	return auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{walkRepoID},
	})
}

type walkResponse struct {
	Results []querycontract.EntityContent `json:"results"`
	Count   int                           `json:"count"`
	Trunc   bool                          `json:"truncated"`
	Walk    map[string]any                `json:"rank_walk"`
}

func postWalk(t *testing.T, h *ContentHandler, path string, body map[string]any) (*httptest.ResponseRecorder, walkResponse) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded)).WithContext(walkContext())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var resp walkResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v; body = %s", err, rec.Body.String())
		}
	}
	return rec, resp
}

func walkEntities(t *testing.T, h *ContentHandler, limit int) []string {
	t.Helper()
	var served []string
	token := rank.StartToken
	for page := 0; page < 100; page++ {
		rec, resp := postWalk(t, h, "/api/v0/content/entities/search", map[string]any{
			"query": "payment refund", "repo_id": walkRepoID, "limit": limit, "rank_cursor": token,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("page %d status = %d, body = %s", page, rec.Code, rec.Body.String())
		}
		for _, row := range resp.Results {
			served = append(served, row.EntityID)
		}
		next, _ := resp.Walk["next_cursor"].(string)
		if next == "" {
			if resp.Walk["window_end"] != true {
				t.Fatalf("page %d: walk ended without window_end; block = %v", page, resp.Walk)
			}
			return served
		}
		token = next
	}
	t.Fatal("walk did not end")
	return nil
}

func TestSearchEntitiesRankWalkSpansPagesInGlobalOrder(t *testing.T) {
	t.Parallel()
	store := &recordingContentAuthzStore{byEntityRepo: map[string][]querycontract.EntityContent{walkRepoID: walkRows(150)}}
	h := walkHandler(store)

	whole := walkEntities(t, h, 200)
	if len(whole) != 150 {
		t.Fatalf("one-page walk served %d rows, want 150", len(whole))
	}
	paged := walkEntities(t, h, 40)
	if !slices.Equal(paged, whole) {
		t.Fatalf("40-row pages differ from the single-page relevance order:\n got %v\nwant %v", paged, whole)
	}
	seen := map[string]bool{}
	for _, id := range paged {
		if seen[id] {
			t.Fatalf("row %s served twice", id)
		}
		seen[id] = true
	}
	// The ranker moved rows across the old page edge: offset paging would
	// serve entity-000..039 first; the walk serves the most relevant rows.
	if slices.Contains(paged[:40], "entity-000") || !slices.Contains(paged[:40], "entity-149") {
		t.Fatalf("first walk page %v does not hold the most relevant rows", paged[:40])
	}
}

func TestSearchEntitiesRankWalkMarksRowsAndReportsTheWindow(t *testing.T) {
	t.Parallel()
	store := &recordingContentAuthzStore{byEntityRepo: map[string][]querycontract.EntityContent{walkRepoID: walkRows(30)}}
	rec, resp := postWalk(t, walkHandler(store), "/api/v0/content/entities/search", map[string]any{
		"query": "payment refund", "repo_id": walkRepoID, "limit": 10, "rank_cursor": rank.StartToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if resp.Count != 10 || !resp.Trunc {
		t.Fatalf("count %d truncated %v, want 10 true", resp.Count, resp.Trunc)
	}
	for _, row := range resp.Results {
		if row.SearchBackend != "hybrid" || row.PagePosition != nil {
			t.Fatalf("row %s backend %q position %v, want hybrid and no page_position", row.EntityID, row.SearchBackend, row.PagePosition)
		}
	}
	if resp.Walk["position"] != float64(10) || resp.Walk["window_rows"] != float64(30) || resp.Walk["lexical_truncated"] != false {
		t.Fatalf("rank_walk = %v, want position 10 of 30, not lexically truncated", resp.Walk)
	}
	if resp.Walk["lane"] != rank.LaneContent || resp.Walk["order"] != rank.OrderHybrid || resp.Walk["ranked"] != true {
		t.Fatalf("rank_walk = %v, want lane content, order hybrid, ranked", resp.Walk)
	}
	if calls := store.entityRepoCalls; len(calls) != 1 {
		t.Fatalf("store calls = %v, want one window read", calls)
	}
}

func TestSearchEntitiesRankWalkReportsLexicalTruncationPastTheWindow(t *testing.T) {
	t.Parallel()
	store := &recordingContentAuthzStore{byEntityRepo: map[string][]querycontract.EntityContent{walkRepoID: walkRows(rank.WindowSize + 5)}}
	h := walkHandler(store)
	served := walkEntities(t, h, 200)
	if len(served) != rank.WindowSize {
		t.Fatalf("walk served %d rows, want the %d-row window", len(served), rank.WindowSize)
	}
	if slices.Contains(served, fmt.Sprintf("entity-%03d", rank.WindowSize)) {
		t.Fatal("walk served a row past the lexical window")
	}
	rec, resp := postWalk(t, h, "/api/v0/content/entities/search", map[string]any{
		"query": "payment refund", "repo_id": walkRepoID, "limit": 200, "rank_cursor": rank.StartToken,
	})
	if rec.Code != http.StatusOK || resp.Walk["lexical_truncated"] != true || resp.Walk["window_end"] != true || !resp.Trunc {
		t.Fatalf("status %d block %v truncated %v, want window end with lexical_truncated", rec.Code, resp.Walk, resp.Trunc)
	}
}

func TestSearchEntitiesRankWalkBasisChangeIsConflict(t *testing.T) {
	t.Parallel()
	store := &recordingContentAuthzStore{byEntityRepo: map[string][]querycontract.EntityContent{walkRepoID: walkRows(30)}}
	h := walkHandler(store)
	_, first := postWalk(t, h, "/api/v0/content/entities/search", map[string]any{
		"query": "payment refund", "repo_id": walkRepoID, "limit": 10, "rank_cursor": rank.StartToken,
	})
	next, _ := first.Walk["next_cursor"].(string)
	store.byEntityRepo[walkRepoID][3].SourceCache = "rewritten between pages"

	rec, _ := postWalk(t, h, "/api/v0/content/entities/search", map[string]any{
		"query": "payment refund", "repo_id": walkRepoID, "limit": 10, "rank_cursor": next,
	})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), string(rank.ErrorCodeBasisMismatch)) ||
		!strings.Contains(rec.Body.String(), rank.ReasonBasisChanged) {
		t.Fatalf("status = %d body = %s, want 409 %s %s", rec.Code, rec.Body.String(), rank.ErrorCodeBasisMismatch, rank.ReasonBasisChanged)
	}
}

func TestSearchRankWalkRefusesRequestsItCannotServe(t *testing.T) {
	t.Parallel()
	rows := map[string][]querycontract.EntityContent{walkRepoID: walkRows(5)}
	cases := []struct {
		name   string
		h      *ContentHandler
		path   string
		body   map[string]any
		status int
		code   querycontract.ErrorCode
		reason string
	}{
		{
			"ranker disabled", &ContentHandler{Content: &recordingContentAuthzStore{byEntityRepo: rows}, HybridRanker: NewContentHybridRanker(false)},
			"/api/v0/content/entities/search",
			map[string]any{"query": "x", "repo_id": walkRepoID, "rank_cursor": "start"},
			http.StatusBadRequest, rank.ErrorCodeUnsupported, rank.ReasonRankerDisabled,
		},
		{
			"no ranker", &ContentHandler{Content: &recordingContentAuthzStore{byEntityRepo: rows}},
			"/api/v0/content/entities/search",
			map[string]any{"query": "x", "repo_id": walkRepoID, "rank_cursor": "start"},
			http.StatusBadRequest, rank.ErrorCodeUnsupported, rank.ReasonRankerDisabled,
		},
		{
			"no repository", walkHandler(&recordingContentAuthzStore{byEntityRepo: rows}),
			"/api/v0/content/entities/search",
			map[string]any{"query": "xyz", "rank_cursor": "start"},
			http.StatusBadRequest, rank.ErrorCodeUnsupported, rank.ReasonScopeNotSingleRepo,
		},
		{
			"offset", walkHandler(&recordingContentAuthzStore{byEntityRepo: rows}),
			"/api/v0/content/entities/search",
			map[string]any{"query": "x", "repo_id": walkRepoID, "offset": 5, "rank_cursor": "start"},
			http.StatusBadRequest, rank.ErrorCodeUnsupported, rank.ReasonOffsetConflict,
		},
		{
			"unscoped cursor", walkHandler(&recordingContentAuthzStore{byEntityRepo: rows}),
			"/api/v0/content/entities/search",
			map[string]any{"query": "x", "repo_id": walkRepoID, "cursor": map[string]any{"entity_id": "e"}, "rank_cursor": "start"},
			http.StatusBadRequest, rank.ErrorCodeUnsupported, rank.ReasonCursorConflict,
		},
		{
			"file route", walkHandler(&recordingContentAuthzStore{byEntityRepo: rows}),
			"/api/v0/content/files/search",
			map[string]any{"query": "x", "repo_id": walkRepoID, "rank_cursor": "start"},
			http.StatusBadRequest, rank.ErrorCodeUnsupported, rank.ReasonFileSearch,
		},
		{
			"garbage cursor", walkHandler(&recordingContentAuthzStore{byEntityRepo: rows}),
			"/api/v0/content/entities/search",
			map[string]any{"query": "x", "repo_id": walkRepoID, "rank_cursor": "page-2"},
			http.StatusBadRequest, rank.ErrorCodeInvalid, rank.ReasonMalformed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.h.Profile = querycontract.ProfileLocalAuthoritative
			rec, _ := postWalk(t, tc.h, tc.path, tc.body)
			body := rec.Body.String()
			if rec.Code != tc.status || !strings.Contains(body, string(tc.code)) || !strings.Contains(body, tc.reason) {
				t.Fatalf("status = %d body = %s, want %d %s %s", rec.Code, body, tc.status, tc.code, tc.reason)
			}
		})
	}
}

// TestSearchEntitiesOffsetPathBytesUnchanged pins the exact response bytes of
// an offset-paged, hybrid-ranked entity search whose page the re-rank
// reorders, so the rank walk cannot change a request that does not send
// rank_cursor. The want string was captured from main at 8f188353f, before
// the walk existed.
func TestSearchEntitiesOffsetPathBytesUnchanged(t *testing.T) {
	t.Parallel()
	all := walkRows(25)
	lexical := []querycontract.EntityContent{all[0], all[20], all[10], all[24]}
	store := &recordingContentAuthzStore{byEntityRepo: map[string][]querycontract.EntityContent{walkRepoID: lexical}}
	rec, _ := postWalk(t, walkHandler(store), "/api/v0/content/entities/search", map[string]any{
		"query": "payment refund", "repo_id": walkRepoID, "limit": 3,
	})
	const want = offsetPathGolden
	if got := rec.Body.String(); got != want {
		t.Fatalf("offset response bytes changed:\n got %s\nwant %s", got, want)
	}
}

const offsetPathGolden = `{"count":3,"docstring_clip_bytes":512,"docstring_clipped_rows":0,"limit":3,"offset":0,"results":[{"end_line":0,"entity_id":"entity-020","entity_name":"handler020","entity_type":"function","language":"go","page_position":1,"relative_path":"svc/handler_020.go","repo_id":"repo-walk","search_backend":"hybrid","source_cache":"func body order invoice payment refund payment refund ","source_handle":{"end_line":0,"file_path":"svc/handler_020.go","repo_id":"repo-walk","start_line":0},"start_line":0},{"end_line":0,"entity_id":"entity-010","entity_name":"handler010","entity_type":"function","language":"go","page_position":2,"relative_path":"svc/handler_010.go","repo_id":"repo-walk","search_backend":"hybrid","source_cache":"func body order invoice payment refund ","source_handle":{"end_line":0,"file_path":"svc/handler_010.go","repo_id":"repo-walk","start_line":0},"start_line":0},{"end_line":0,"entity_id":"entity-000","entity_name":"handler000","entity_type":"function","language":"go","page_position":0,"relative_path":"svc/handler_000.go","repo_id":"repo-walk","search_backend":"hybrid","source_cache":"func body order invoice ","source_handle":{"end_line":0,"file_path":"svc/handler_000.go","repo_id":"repo-walk","start_line":0},"start_line":0}],"source_backend":"postgres_content_store","source_cache_clip_bytes":4096,"source_cache_clipped_rows":0,"truncated":true}` + "\n"

// TestSearchEntitiesRankWalksRunConcurrently runs eight full walks at once
// through one handler and one shared ranker. Under -race it proves the walk
// keeps no shared mutable state; every walk must read the same order.
func TestSearchEntitiesRankWalksRunConcurrently(t *testing.T) {
	t.Parallel()
	h := walkHandler(&staticEntityStore{rows: walkRows(120)})
	want := walkEntities(t, h, 200)
	var wg sync.WaitGroup
	results := make([][]string, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = walkEntitiesQuiet(h, 30)
		}()
	}
	wg.Wait()
	for i, got := range results {
		if !slices.Equal(got, want) {
			t.Fatalf("concurrent walk %d order differs:\n got %v\nwant %v", i, got, want)
		}
	}
}

// staticEntityStore serves fixed entity rows and records nothing, so
// concurrent walks share no mutable test state.
type staticEntityStore struct {
	content.FakePortContentStore
	rows []querycontract.EntityContent
}

func (s *staticEntityStore) SearchEntityContent(_ context.Context, _ string, _ string, limit int) ([]querycontract.EntityContent, error) {
	return limitEntityContentAuthzRows(s.rows, limit), nil
}

// walkEntitiesQuiet is walkEntities without testing.T calls, for goroutines.
// It returns nil on any failure, which the caller's comparison reports.
func walkEntitiesQuiet(h *ContentHandler, limit int) []string {
	mux := http.NewServeMux()
	h.Mount(mux)
	var served []string
	token := rank.StartToken
	for page := 0; page < 100; page++ {
		encoded, _ := json.Marshal(map[string]any{
			"query": "payment refund", "repo_id": walkRepoID, "limit": limit, "rank_cursor": token,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v0/content/entities/search", bytes.NewReader(encoded)).WithContext(walkContext())
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var resp walkResponse
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &resp) != nil {
			return nil
		}
		for _, row := range resp.Results {
			served = append(served, row.EntityID)
		}
		next, _ := resp.Walk["next_cursor"].(string)
		if next == "" {
			return served
		}
		token = next
	}
	return nil
}
