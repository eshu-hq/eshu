// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// complexityListData posts a list-mode complexity request and returns the
// envelope's data object.
func complexityListData(t *testing.T, rows []map[string]any, auth *AuthContext) map[string]any {
	t.Helper()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) { return rows, nil },
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, newCodeGrantRouteRequest(t, "/api/v0/code/complexity", map[string]any{"limit": 3}, auth))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, rec.Body.String())
	}
	return envelope.Data
}

func dataKeys(data map[string]any) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// TestComplexityListClipsRowDocstrings pins the #7234 sibling clip on the
// complexity list: an over-long docstring is cut to the clip ceiling in every
// echo AttachSemanticSummary derived from it, and the row and response carry
// the markers.
func TestComplexityListClipsRowDocstrings(t *testing.T) {
	t.Parallel()

	doc := "MARK " + strings.Repeat("d", 4096)
	data := complexityListData(t, []map[string]any{{
		"id": "function-1", "name": "search", "labels": []any{"Function"}, "file_path": "src/search.js",
		"repo_id": "repo-1", "repo_name": "catalog", "language": "javascript",
		"start_line": int64(8), "end_line": int64(21), "complexity": int64(13),
		"docstring": doc, "method_kind": "getter",
	}}, nil)

	if got := data[querycontract.DocstringClipBytesKey]; got != float64(querycontract.DocstringClipBytes) {
		t.Fatalf("docstring_clip_bytes = %v, want %d", got, querycontract.DocstringClipBytes)
	}
	if got := data[querycontract.DocstringClippedRowsKey]; got != float64(1) {
		t.Fatalf("docstring_clipped_rows = %v, want 1", got)
	}
	results, _ := data["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results = %v, want one row", data["results"])
	}
	row, _ := results[0].(map[string]any)
	if row[querycontract.DocstringClippedKey] != true {
		t.Fatalf("row docstring_clipped = %v, want true", row[querycontract.DocstringClippedKey])
	}
	if got, want := row[querycontract.DocstringTotalBytesKey], float64(len(doc)); got != want {
		t.Fatalf("row docstring_total_bytes = %v, want %v", got, want)
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal row: %v", err)
	}
	if strings.Contains(string(encoded), strings.Repeat("d", querycontract.DocstringClipBytes+1)) {
		t.Fatalf("row still carries more than %d docstring bytes in one echo", querycontract.DocstringClipBytes)
	}
}

// TestComplexityEmptyGrantAnswerHasTheShapeOfARealEmptyAnswer pins the
// fail-closed indistinguishability rule: a scoped caller with no grants must
// see the same response keys, docstring markers included, as an unscoped
// caller whose repository simply has no complexity rows.
func TestComplexityEmptyGrantAnswerHasTheShapeOfARealEmptyAnswer(t *testing.T) {
	t.Parallel()

	auth := testutil.CodeGrantScopedAuthContext(nil)
	emptyGrant := complexityListData(t, nil, &auth)
	emptyIndex := complexityListData(t, nil, nil)

	for name, data := range map[string]map[string]any{"empty grant": emptyGrant, "empty index": emptyIndex} {
		if got := data[querycontract.DocstringClipBytesKey]; got != float64(querycontract.DocstringClipBytes) {
			t.Errorf("%s: docstring_clip_bytes = %v, want %d", name, got, querycontract.DocstringClipBytes)
		}
		if got := data[querycontract.DocstringClippedRowsKey]; got != float64(0) {
			t.Errorf("%s: docstring_clipped_rows = %v, want 0", name, got)
		}
	}
	if got, want := strings.Join(dataKeys(emptyGrant), ","), strings.Join(dataKeys(emptyIndex), ","); got != want {
		t.Fatalf("empty-grant keys = %s, empty-index keys = %s; an ungranted caller can tell them apart", got, want)
	}
}
