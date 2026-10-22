// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// emptyPathIDTools lists every registered tool whose route puts a required
// string argument in the URL path, with that argument's name. An empty value
// leaves "//" in the path and the mux answers 307 (#8772).
var emptyPathIDTools = []struct {
	tool  string
	field string
	path  string
}{
	{"get_entity_context", "entity_id", "/api/v0/entities/{id}/context"},
	{"get_incident_context", "provider_incident_id", "/api/v0/incidents/{id}/context"},
	{"get_repository_coverage", "repo_id", "/api/v0/repositories/{id}/coverage"},
	{"get_workload_context", "workload_id", "/api/v0/workloads/{id}/context"},
	{"get_workload_story", "workload_id", "/api/v0/workloads/{id}/story"},
	{"get_repo_context", "repo_id", "/api/v0/repositories/{id}/context"},
	{"get_repo_story", "repo_id", "/api/v0/repositories/{id}/story"},
	{"get_repository_freshness", "repo_id", "/api/v0/repositories/{id}/freshness"},
	{"get_documentation_evidence_packet", "finding_id", "/api/v0/documentation/findings/{id}/evidence-packet"},
	{"check_documentation_evidence_packet_freshness", "packet_id", "/api/v0/documentation/evidence-packets/{id}/freshness"},
}

// countingPathMux serves a 200 JSON body for every emptyPathIDTools route and
// counts handler runs, so a test can prove an empty id never reached a handler.
func countingPathMux(runs *atomic.Int64) *http.ServeMux {
	mux := http.NewServeMux()
	for _, tc := range emptyPathIDTools {
		mux.HandleFunc("GET "+tc.path, func(w http.ResponseWriter, _ *http.Request) {
			runs.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
		})
	}
	return mux
}

func callToolDirect(t *testing.T, handler http.Handler, tool string, args map[string]any) mcpToolResult {
	t.Helper()
	params, err := json.Marshal(map[string]any{"name": tool, "arguments": args})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	server := NewServer(handler, slog.New(slog.NewTextHandler(io.Discard, nil)))
	resp := server.handleMessage(context.Background(), &jsonrpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params,
	}, "")
	result, ok := resp.Result.(mcpToolResult)
	if !ok {
		t.Fatalf("%s result type = %T, want mcpToolResult", tool, resp.Result)
	}
	return result
}

func TestDirectCallEmptyPathIDReturnsMissingArgumentError(t *testing.T) {
	t.Parallel()
	for _, tc := range emptyPathIDTools {
		t.Run(tc.tool, func(t *testing.T) {
			t.Parallel()
			var runs atomic.Int64
			result := callToolDirect(t, countingPathMux(&runs), tc.tool, map[string]any{})
			if !result.IsError {
				t.Fatalf("isError = false, want true; content = %+v", result.Content)
			}
			if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, tc.field+" is required") {
				t.Fatalf("content = %+v, want text naming %q is required", result.Content, tc.field)
			}
			if got := runs.Load(); got != 0 {
				t.Fatalf("handler ran %d times, want 0 for an empty path id", got)
			}
		})
	}
}

func TestDirectCallWithPathIDStillSucceeds(t *testing.T) {
	t.Parallel()
	for _, tc := range emptyPathIDTools {
		t.Run(tc.tool, func(t *testing.T) {
			t.Parallel()
			var runs atomic.Int64
			result := callToolDirect(t, countingPathMux(&runs), tc.tool, map[string]any{tc.field: "abc"})
			if result.IsError {
				t.Fatalf("isError = true, want false; content = %+v", result.Content)
			}
			if got := runs.Load(); got != 1 {
				t.Fatalf("handler ran %d times, want 1", got)
			}
		})
	}
}

// TestDispatchRedirectStatusIsNotSuccess pins that a real 3xx from a handler
// is never reported as success. No MCP-reachable route redirects on purpose:
// the only http.Redirect calls in go/internal/query are the OIDC and SAML
// browser login handlers, which no tool routes to (#8772).
func TestDispatchRedirectStatusIsNotSuccess(t *testing.T) {
	t.Parallel()
	for _, status := range []int{
		http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusNotModified, http.StatusTemporaryRedirect, http.StatusPermanentRedirect,
	} {
		for _, body := range []string{"", `{"data":null,"truth":null,"error":null}`} {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", "/elsewhere")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, body)
			})
			result, err := dispatchTool(
				context.Background(), handler, "get_workload_story",
				map[string]any{"workload_id": "w1"}, "",
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			)
			if err == nil && (result == nil || !result.IsError) {
				t.Fatalf("status %d body %q: dispatch reported success, want error", status, body)
			}
			if err != nil && !strings.Contains(err.Error(), "HTTP "+strconv.Itoa(status)) {
				t.Fatalf("status %d: error = %v, want it to name the HTTP status", status, err)
			}
		}
	}
}
