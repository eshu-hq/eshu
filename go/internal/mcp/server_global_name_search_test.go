// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestHTTPTransportGlobalNameToolsForwardExactContracts(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v0/code/search", func(w http.ResponseWriter, r *http.Request) {
		body := decodeMCPForwardedBody(t, r)
		if body["query"] != "Run" || body["language"] != "go" || body["exact"] != true || body["limit"] != float64(10) {
			t.Fatalf("find_code forwarded body = %#v", body)
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"source": "content", "source_backend": "postgres_content_name_index",
			"query": "Run", "repo_id": "", "results": []any{}, "matches": []any{},
			"count": 0, "limit": 10, "truncated": false,
		}, query.BuildTruthEnvelope(query.ProfileLocalAuthoritative, "code_search.exact_symbol", query.TruthBasisContentIndex, "exact name proof"))
	})
	mux.HandleFunc("POST /api/v0/entities/resolve", func(w http.ResponseWriter, r *http.Request) {
		body := decodeMCPForwardedBody(t, r)
		if body["name"] != "Run" || body["type"] != "function" {
			t.Fatalf("resolve_entity forwarded body = %#v", body)
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"entities": []any{}, "matches": []any{}, "count": 0, "limit": 10, "truncated": false,
		}, query.BuildTruthEnvelope(query.ProfileLocalAuthoritative, "code_search.exact_symbol", query.TruthBasisContentIndex, "exact typed proof"))
	})
	server := NewServer(mux, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, request := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_code","arguments":{"query":"Run","language":"go","exact":true,"limit":10}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"resolve_entity","arguments":{"query":"Run","type":"function","limit":10}}}`,
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(request))
		req.Header.Set("Content-Type", "application/json")
		server.handleHTTPMessage(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("MCP HTTP status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var response map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode MCP response: %v", err)
		}
		result, _ := response["result"].(map[string]any)
		if result == nil || result["isError"] == true {
			t.Fatalf("MCP result = %#v, want successful envelope", result)
		}
		structured, _ := result["structuredContent"].(map[string]any)
		truth, _ := structured["truth"].(map[string]any)
		if truth["basis"] != string(query.TruthBasisContentIndex) {
			t.Fatalf("MCP structured truth = %#v, want content_index", truth)
		}
	}
}

func TestHTTPTransportPreservesExactAndOverflowCodeSearchPages(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v0/code/search", func(w http.ResponseWriter, r *http.Request) {
		body := decodeMCPForwardedBody(t, r)
		if body["exact"] != true || body["limit"] != float64(1) {
			t.Fatalf("find_code forwarded body = %#v, want exact public limit 1", body)
		}
		truncated := body["query"] == "Overflow"
		row := map[string]any{"entity_id": "entity-a", "name": body["query"], "labels": []any{"Function"}}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"source": "content", "source_backend": "postgres_content_name_index",
			"query": body["query"], "repo_id": "", "results": []any{row}, "matches": []any{row},
			"count": 1, "limit": 1, "truncated": truncated,
		}, query.BuildTruthEnvelope(
			query.ProfileLocalAuthoritative,
			"code_search.exact_symbol",
			query.TruthBasisContentIndex,
			"exact name proof",
		))
	})
	server := NewServer(mux, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, tc := range []struct {
		query         string
		wantTruncated bool
	}{
		{query: "Exact"},
		{query: "Overflow", wantTruncated: true},
	} {
		t.Run(tc.query, func(t *testing.T) {
			request := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_code","arguments":{"query":"` + tc.query + `","exact":true,"limit":1}}}`
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(request))
			req.Header.Set("Content-Type", "application/json")

			server.handleHTTPMessage(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("MCP HTTP status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
			}
			var response map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode MCP response: %v", err)
			}
			result, _ := response["result"].(map[string]any)
			structured, _ := result["structuredContent"].(map[string]any)
			data, _ := structured["data"].(map[string]any)
			results, _ := data["results"].([]any)
			if len(results) != 1 || data["count"] != float64(1) || data["limit"] != float64(1) ||
				data["truncated"] != tc.wantTruncated {
				t.Fatalf("MCP structured page = %#v", structured)
			}
		})
	}
}

func decodeMCPForwardedBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode forwarded HTTP body: %v", err)
	}
	return body
}
