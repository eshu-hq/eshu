// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// routerResult decodes a router tools/call result.
type routerResult struct {
	IsError           bool           `json:"isError"`
	StructuredContent map[string]any `json:"structuredContent"`
}

func decodeRouterResult(t *testing.T, raw []byte) routerResult {
	t.Helper()
	var out routerResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode router result %s: %v", raw, err)
	}
	return out
}

func offeredNames(t *testing.T, raw []byte) []string {
	t.Helper()
	result := decodeRouterResult(t, raw)
	if result.IsError {
		t.Fatalf("find_tool failed: %s", raw)
	}
	candidates, _ := result.StructuredContent["candidates"].([]any)
	names := make([]string, 0, len(candidates))
	for _, c := range candidates {
		names = append(names, c.(map[string]any)["name"].(string))
	}
	return names
}

func scopedTokenContext() context.Context {
	return query.ContextWithAuthContext(context.Background(), query.AuthContext{
		Mode: query.AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "ws-a",
		AllowedRepositoryIDs: []string{"repository:r_1"},
	})
}

// findAll collects find_tool candidates over the whole registry: the stub
// ranker returns tools in input order, so it narrows the visible list to one
// MaxLimit-sized window per call.
func findAll(t *testing.T, s *Server, ctx context.Context) []string {
	t.Helper()
	var all []string
	saved := s.tools
	defer func() { s.tools = saved }()
	for start := 0; start < len(saved); start += router.MaxLimit {
		end := min(start+router.MaxLimit, len(saved))
		s.tools = saved[start:end]
		raw := toolCall(t, s, ctx, router.FindToolName, map[string]any{"intent": "anything", "limit": router.MaxLimit})
		all = append(all, offeredNames(t, raw)...)
	}
	return all
}

// TestScopedTokenNeverSeesOrCallsExecuteCypher is the #8757 scoped-token
// acceptance proof against the real registry and the real route ledgers.
func TestScopedTokenNeverSeesOrCallsExecuteCypher(t *testing.T) {
	t.Parallel()

	s := newRouterTestServer(t, echoEnvelopeHandler())
	offered := findAll(t, s, scopedTokenContext())
	for _, banned := range []string{"execute_cypher_query", "visualize_graph_query"} {
		if slices.Contains(offered, banned) {
			t.Fatalf("scoped find_tool offered %s", banned)
		}
	}
	if !slices.Contains(offered, "find_code") {
		t.Fatalf("scoped find_tool offered %v, want allowlisted find_code kept", offered)
	}

	raw := toolCall(t, s, scopedTokenContext(), router.CallToolName, map[string]any{
		"name": "execute_cypher_query", "arguments": map[string]any{"cypher_query": "MATCH (n) RETURN n LIMIT 1"},
	})
	result := decodeRouterResult(t, raw)
	errObj, _ := result.StructuredContent["error"].(map[string]any)
	if !result.IsError || errObj["code"] != "mcp_router_refused" {
		t.Fatalf("scoped call_tool(execute_cypher_query) = %s, want mcp_router_refused", raw)
	}
	if bytes.Contains(raw, []byte("/api/v0/code/cypher")) {
		t.Fatalf("scoped call_tool reached the cypher route: %s", raw)
	}
}

// TestFindWithoutAuthContextReturnsUnfilteredFlaggedResults covers stdio and
// ESHU_MCP_ALLOW_UNAUTHENTICATED: no AuthContext, no filter, and a flag.
func TestFindWithoutAuthContextReturnsUnfilteredFlaggedResults(t *testing.T) {
	t.Parallel()

	s := newRouterTestServer(t, echoEnvelopeHandler())
	offered := findAll(t, s, context.Background())
	if !slices.Contains(offered, "execute_cypher_query") {
		t.Fatalf("unauthenticated find_tool omitted execute_cypher_query; it must not filter")
	}
	raw := toolCall(t, s, context.Background(), router.FindToolName, map[string]any{"intent": "anything"})
	access, _ := decodeRouterResult(t, raw).StructuredContent["access"].(map[string]any)
	if access["filtered"] != false || access["reason"] != "no_auth_context" {
		t.Fatalf("access = %v, want filtered=false reason=no_auth_context", access)
	}
}

// TestCallToolRefusesRecursiveTargetsThroughServer proves the recursion
// guard holds on the real intercept, before any dispatch.
func TestCallToolRefusesRecursiveTargetsThroughServer(t *testing.T) {
	t.Parallel()

	s := newRouterTestServer(t, echoEnvelopeHandler())
	for _, target := range []string{router.FindToolName, router.CallToolName, "ask"} {
		raw := toolCall(t, s, context.Background(), router.CallToolName, map[string]any{
			"name": target, "arguments": map[string]any{"intent": "x", "question": "q"},
		})
		errObj, _ := decodeRouterResult(t, raw).StructuredContent["error"].(map[string]any)
		if errObj["code"] != "mcp_router_refused" {
			t.Fatalf("call_tool(%s) = %s, want mcp_router_refused", target, raw)
		}
		if bytes.Contains(raw, []byte("/api/v0/")) {
			t.Fatalf("call_tool(%s) dispatched: %s", target, raw)
		}
	}
}

// TestRouterRouteClassMatchesDispatchArguments proves the probe arguments the
// engine classifies routes with resolve for every registered tool and land on the same auth class as
// the dispatch tests' own minimal arguments, for every registered tool.
func TestRouterRouteClassMatchesDispatchArguments(t *testing.T) {
	t.Parallel()

	for _, tool := range ReadOnlyTools() {
		want, err := resolveRoute(tool.Name, minimalDispatchRouteArgs(tool.Name))
		if err != nil {
			t.Fatalf("%s: minimal args do not resolve: %v", tool.Name, err)
		}
		var method, path string
		for _, args := range router.RouteProbeArguments(tool.InputSchema) {
			if method, path, err = routerRoute(tool.Name, args); err == nil {
				break
			}
		}
		if err != nil {
			t.Fatalf("%s: no route probe resolves: %v", tool.Name, err)
		}
		if got, wantClass := routeAuthClass(method, path), routeAuthClass(want.Method, want.Path); got != wantClass {
			t.Fatalf("%s: example route %s %s class %v, minimal route %s %s class %v",
				tool.Name, method, path, got, want.Method, want.Path, wantClass)
		}
	}
}

func routeAuthClass(method, path string) [3]bool {
	req := httptest.NewRequest(method, path, nil)
	return [3]bool{
		query.ScopedHTTPRouteSupportsTenantFilter(req),
		query.IsSharedKeyOnlyRoute(req),
		query.IsPendingRowFilteringRoute(req),
	}
}
