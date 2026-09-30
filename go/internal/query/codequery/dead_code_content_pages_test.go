// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

type deadCodePageKey struct {
	label  string
	offset int
}

type deadCodePageCall struct {
	repoID   string
	label    string
	language string
	limit    int
	offset   int
	allowed  []string
}

type deadCodePageStore struct {
	content.FakePortContentStore
	pages map[deadCodePageKey][]map[string]any
	calls []deadCodePageCall
	fail  *deadCodePageKey
}

// DeadCodeCandidateRows returns one scripted content page and records its query bounds.
func (s *deadCodePageStore) DeadCodeCandidateRows(_ context.Context, q codeshaping.DeadCodeCandidateQuery) ([]map[string]any, error) {
	s.calls = append(s.calls, deadCodePageCall{q.RepoID, q.Label, q.Language, q.Limit, q.Offset, q.AllowedRepositoryIDs})
	key := deadCodePageKey{q.Label, q.Offset}
	if s.fail != nil && key == *s.fail {
		return nil, errors.New("candidate read failed")
	}
	rows := s.pages[key]
	if len(rows) > q.Limit {
		return nil, fmt.Errorf("fixture page %s/%d has %d rows over limit %d", q.Label, q.Offset, len(rows), q.Limit)
	}
	return rows, nil
}

// DeadCodeIncomingEntityIDs confirms the content read found no incoming edges.
func (*deadCodePageStore) DeadCodeIncomingEntityIDs(context.Context, string, []string) (map[string]deadcode.DeadCodeIncomingEdge, error) {
	return map[string]deadcode.DeadCodeIncomingEdge{}, nil
}

func deadCodeHTTPRow(label, id, path string) map[string]any {
	return map[string]any{
		"entity_id": id, "name": "helper", "labels": []any{label},
		"file_path": path, "repo_id": "repo-1", "repo_name": "repo",
		"language": "go", "start_line": int64(1), "end_line": int64(2),
	}
}

func deadCodeHTTPRows(label string, count int, path string) []map[string]any {
	rows := make([]map[string]any, count)
	for i := range rows {
		rows[i] = deadCodeHTTPRow(label, fmt.Sprintf("%s-%03d", label, i+1), path)
	}
	return rows
}

func deadCodePageRequest(t *testing.T, store *deadCodePageStore, body string) (int, map[string]any) {
	t.Helper()
	handler := &codequery.CodeHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Content: store,
		Neo4j: graph.FakeGraphReader{
			RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				t.Fatal("unexpected graph candidate or incoming read")
				return nil, nil
			},
			RunIncomingFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				t.Fatal("unexpected graph incoming read")
				return nil, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/dead-code", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	return w.Code, response
}

func deadCodeFirstPageCalls() []deadCodePageCall {
	return []deadCodePageCall{
		{repoID: "repo-1", label: "Function", limit: 121},
		{repoID: "repo-1", label: "Class", limit: 121},
		{repoID: "repo-1", label: "Struct", limit: 121},
		{repoID: "repo-1", label: "Interface", limit: 121},
		{repoID: "repo-1", label: "Trait", limit: 121},
		{repoID: "repo-1", label: "SqlFunction", limit: 121},
	}
}

func deadCodePageResultIDs(t *testing.T, response map[string]any) []string {
	t.Helper()
	results, ok := response["results"].([]any)
	if !ok {
		t.Fatalf("results = %#v, want array", response["results"])
	}
	ids := make([]string, 0, len(results))
	for _, raw := range results {
		row, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("result = %#v, want object", raw)
		}
		id, ok := row["entity_id"].(string)
		if !ok {
			t.Fatalf("entity_id = %#v, want string", row["entity_id"])
		}
		ids = append(ids, id)
	}
	return ids
}

func checkDeadCodePageResponse(t *testing.T, response map[string]any, ids []string, pages, rows int, display, scan bool) {
	t.Helper()
	if got := deadCodePageResultIDs(t, response); !slices.Equal(got, ids) {
		t.Errorf("result IDs = %v, want %v", got, ids)
	}
	for key, want := range map[string]any{
		"repo_id": "repo-1", "language": "", "limit": float64(12),
		"candidate_scan_limit": float64(1210), "candidate_scan_limit_per_label": float64(1210),
		"candidate_scan_pages": float64(pages), "candidate_scan_rows": float64(rows),
		"display_truncated": display, "candidate_scan_truncated": scan, "truncated": display || scan,
	} {
		if got := response[key]; got != want {
			t.Errorf("%s = %#v, want %#v", key, got, want)
		}
	}
}

func TestDeadCodeHTTPBlankLanguageContentPageContract(t *testing.T) {
	t.Parallel()
	first := deadCodeFirstPageCalls()
	live := func(label, id, path string) []map[string]any {
		return []map[string]any{deadCodeHTTPRow(label, id, path)}
	}
	funcRows := deadCodeHTTPRows("Function", 12, "internal/a.go")
	wantTwelve := make([]string, 12)
	for i := range wantTwelve {
		wantTwelve[i] = fmt.Sprintf("Function-%03d", i+1)
	}
	excluded := deadCodeHTTPRows("Function", 120, "internal/fixtures/example_test.go")
	excluded = append(excluded, deadCodeHTTPRow("Function", "Function-001", "internal/fixtures/example_test.go"))
	budgetPages := make(map[deadCodePageKey][]map[string]any)
	for _, label := range []string{"Function", "Class", "Struct", "Interface", "Trait"} {
		budgetPages[deadCodePageKey{label, 0}] = deadCodeHTTPRows(label, 121, "internal/fixtures/example_test.go")
		secondPage := deadCodeHTTPRows(label, 121, "internal/fixtures/example_test.go")
		for i, row := range secondPage {
			row["entity_id"] = fmt.Sprintf("%s-%03d", label, i+122)
		}
		budgetPages[deadCodePageKey{label, 121}] = secondPage
	}
	budgetCalls := append([]deadCodePageCall(nil), first...)
	for _, label := range []string{"Function", "Class", "Struct", "Interface", "Trait"} {
		budgetCalls = append(budgetCalls, deadCodePageCall{repoID: "repo-1", label: label, limit: 121, offset: 121})
	}
	tests := []struct {
		name    string
		body    string
		pages   map[deadCodePageKey][]map[string]any
		calls   []deadCodePageCall
		ids     []string
		rows    int
		display bool
		scan    bool
	}{
		{name: "omitted language", body: `{"repo_id":"repo-1","limit":12}`, calls: first},
		{name: "empty language", body: `{"repo_id":"repo-1","limit":12,"language":""}`, calls: first},
		{name: "whitespace language", body: `{"repo_id":"repo-1","limit":12,"language":" \t "}`, calls: first},
		{name: "all empty", body: `{"repo_id":"repo-1","limit":12}`, calls: first},
		{
			name: "mixed labels", body: `{"repo_id":"repo-1","limit":12}`,
			pages: map[deadCodePageKey][]map[string]any{
				{"Function", 0}:  live("Function", "fn-01", "internal/a.go"),
				{"Class", 0}:     live("Class", "class-01", "internal/b.go"),
				{"Struct", 0}:    live("Struct", "struct-01", "internal/c.go"),
				{"Interface", 0}: live("Interface", "interface-01", "internal/d.go"),
				{"Trait", 0}:     live("Trait", "trait-01", "internal/e.go"),
			}, calls: first, ids: []string{"fn-01", "class-01", "struct-01", "interface-01", "trait-01"}, rows: 5,
		},
		{
			name: "later page after exclusions and duplicate", body: `{"repo_id":"repo-1","limit":12}`,
			pages: map[deadCodePageKey][]map[string]any{
				{"Function", 0}:   excluded,
				{"Function", 121}: {deadCodeHTTPRow("Function", "Function-001", "internal/fixtures/example_test.go"), deadCodeHTTPRow("Function", "late-helper", "internal/late.go")},
			}, calls: append(append([]deadCodePageCall(nil), first...), deadCodePageCall{repoID: "repo-1", label: "Function", limit: 121, offset: 121}),
			ids: []string{"late-helper"}, rows: 123,
		},
		{
			name: "exact display cap", body: `{"repo_id":"repo-1","limit":12}`,
			pages: map[deadCodePageKey][]map[string]any{{"Function", 0}: funcRows},
			calls: first, ids: wantTwelve, rows: 12,
		},
		{
			name: "display cap plus one", body: `{"repo_id":"repo-1","limit":12}`,
			pages: map[deadCodePageKey][]map[string]any{{"Function", 0}: funcRows, {"Class", 0}: live("Class", "class-extra", "internal/b.go")},
			calls: first[:2], ids: wantTwelve, rows: 13, display: true,
		},
		{
			name: "candidate budget boundary", body: `{"repo_id":"repo-1","limit":12}`,
			pages: budgetPages, calls: budgetCalls, rows: 1210, scan: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &deadCodePageStore{pages: tc.pages}
			status, response := deadCodePageRequest(t, store, tc.body)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200; response=%#v", status, response)
			}
			if !reflect.DeepEqual(store.calls, tc.calls) {
				t.Errorf("candidate calls = %#v, want %#v", store.calls, tc.calls)
			}
			checkDeadCodePageResponse(t, response, tc.ids, len(tc.calls), tc.rows, tc.display, tc.scan)
		})
	}
}

func TestDeadCodeHTTPCandidateReadErrorDiscardsPartialResults(t *testing.T) {
	t.Parallel()
	fail := deadCodePageKey{"Class", 0}
	store := &deadCodePageStore{
		pages: map[deadCodePageKey][]map[string]any{
			{"Function", 0}: {deadCodeHTTPRow("Function", "fn-01", "internal/a.go")},
		},
		fail: &fail,
	}
	status, response := deadCodePageRequest(t, store, `{"repo_id":"repo-1","limit":12}`)
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; response=%#v", status, response)
	}
	if want := deadCodeFirstPageCalls()[:2]; !reflect.DeepEqual(store.calls, want) {
		t.Errorf("candidate calls = %#v, want %#v", store.calls, want)
	}
	if got, want := response["error"], http.StatusText(http.StatusInternalServerError); got != want {
		t.Errorf("error = %#v, want %#v", got, want)
	}
	if got, want := response["detail"], "candidate read failed"; got != want {
		t.Errorf("detail = %#v, want %#v", got, want)
	}
	if _, ok := response["results"]; ok {
		t.Errorf("error response contains partial results: %#v", response)
	}
	if _, ok := response["data"]; ok {
		t.Errorf("error response contains success data: %#v", response)
	}
}
