// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	toolcontract "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// routerOrderRanker is the stub ranker the dispatch lane tests with until the
// lexical ranker (#8755) lands: every allowed tool, in input order.
type routerOrderRanker struct{}

func (routerOrderRanker) Rank(_ context.Context, q router.Query, tools []toolcontract.ToolDefinition,
	_ map[string]catalog.Entry, allowed func(string) bool,
) ([]router.Candidate, error) {
	var out []router.Candidate
	for i, tool := range tools {
		if len(out) >= q.Limit {
			break
		}
		if allowed(tool.Name) {
			out = append(out, router.Candidate{Name: tool.Name, Why: "stub", Score: float64(len(tools) - i)})
		}
	}
	return out, nil
}

// echoEnvelopeHandler answers every route with a canonical envelope that
// echoes the request, so two dispatches compare equal only when they sent the
// same request and got the same rendering.
func echoEnvelopeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"method": r.Method,
			"path":   r.URL.Path,
			"query":  r.URL.RawQuery,
			"body":   string(body),
			"auth":   r.Header.Get("Authorization"),
		}, query.BuildTruthEnvelope(query.ProfileLocalAuthoritative, "code_search.fuzzy_symbol", query.TruthBasisContentIndex, "echo"))
	})
}

func newRouterTestServer(t *testing.T, handler http.Handler) *Server {
	t.Helper()
	s := NewServer(handler, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.routerRanker = routerOrderRanker{}
	return s
}

// toolCall runs one tools/call through handleMessage and returns the
// marshaled JSON-RPC result.
func toolCall(t *testing.T, s *Server, ctx context.Context, name string, args map[string]any) []byte {
	t.Helper()
	params, err := json.Marshal(map[string]any{"name": name, "arguments": args})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	resp := s.handleMessage(ctx, &jsonrpcRequest{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params}, "Bearer test-token")
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/call %s = %+v, want a result", name, resp)
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	return raw
}

// sweepArguments loads the catalog sweep corpus: real argument maps for
// every registered tool.
func sweepArguments(t *testing.T) map[string][]map[string]any {
	t.Helper()
	raw, err := os.ReadFile(catalogSweepArgsPath)
	if err != nil {
		t.Fatalf("read %s: %v", catalogSweepArgsPath, err)
	}
	var corpus struct {
		Tools map[string][]struct {
			Arguments map[string]any `json:"arguments"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatalf("decode %s: %v", catalogSweepArgsPath, err)
	}
	out := make(map[string][]map[string]any, len(corpus.Tools))
	for name, cases := range corpus.Tools {
		for _, c := range cases {
			out[name] = append(out[name], c.Arguments)
		}
	}
	return out
}

// TestRoutedCallMatchesDirectCallBytes is the #8757 acceptance proof: a
// call_tool result is byte-identical to a direct tools/call of the same tool
// and arguments, for tools across the epic's families.
func TestRoutedCallMatchesDirectCallBytes(t *testing.T) {
	t.Parallel()

	families := map[string][]string{
		"code lookup":        {"find_code", "find_symbol", "search_entity_content"},
		"repository":         {"list_indexed_repositories", "get_repo_summary"},
		"change and impact":  {"analyze_pre_change_impact", "investigate_change_surface"},
		"supply chain":       {"list_package_registry_packages", "explain_supply_chain_impact"},
		"drift":              {"list_aws_runtime_drift_findings"},
		"service":            {"get_service_story", "get_service_context"},
		"dead code":          {"find_dead_code"},
		"status and runtime": {"get_index_status"},
	}
	corpus := sweepArguments(t)
	s := newRouterTestServer(t, echoEnvelopeHandler())
	checked := 0
	for family, tools := range families {
		for _, name := range tools {
			cases := corpus[name]
			if len(cases) == 0 {
				t.Fatalf("%s/%s has no catalog sweep arguments", family, name)
			}
			args := cases[0]
			direct := toolCall(t, s, context.Background(), name, args)
			routed := toolCall(t, s, context.Background(), router.CallToolName, map[string]any{"name": name, "arguments": args})
			if !bytes.Equal(direct, routed) {
				t.Fatalf("%s/%s routed result differs from direct\ndirect: %s\nrouted: %s", family, name, direct, routed)
			}
			if bytes.Contains(direct, []byte(`"isError":true`)) {
				t.Fatalf("%s/%s direct call failed, the comparison proves nothing: %s", family, name, direct)
			}
			checked++
		}
	}
	if checked < 10 || len(families) < 5 {
		t.Fatalf("compared %d tools across %d families, want >= 10 across >= 5", checked, len(families))
	}
}

// TestDirectToolsCallStillReachesRegisteredTools proves the intercept only
// claims the two router names.
func TestDirectToolsCallStillReachesRegisteredTools(t *testing.T) {
	t.Parallel()

	s := newRouterTestServer(t, echoEnvelopeHandler())
	raw := toolCall(t, s, context.Background(), "get_index_status", map[string]any{})
	if !bytes.Contains(raw, []byte(`/api/v0/index-status`)) {
		t.Fatalf("direct get_index_status = %s, want the echoed route", raw)
	}
}
