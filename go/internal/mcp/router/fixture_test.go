// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// fixtureRegistry is a small registry whose routes are real Eshu API paths, so
// the access filter runs against the real query route classifiers.
func fixtureRegistry() []toolcontract.ToolDefinition {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	return []toolcontract.ToolDefinition{
		{Name: "find_code", Description: "Search code.", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": str("Text to find."),
				"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50, "default": 10},
				"mode":  map[string]any{"type": "string", "enum": []string{"exact", "fuzzy"}},
			},
			"required": []string{"query"},
		}},
		{Name: "execute_cypher_query", Description: "Run read-only Cypher.", InputSchema: map[string]any{
			"type": "object", "properties": map[string]any{"cypher_query": str("Cypher.")}, "required": []string{"cypher_query"},
		}},
		{Name: "get_repo_summary", Description: "Summarize a repository.", InputSchema: map[string]any{
			"type": "object", "properties": map[string]any{"repo_id": str("Repository id.")},
		}},
		{Name: "ask", Description: "Ask Eshu.", InputSchema: map[string]any{
			"type": "object", "properties": map[string]any{"question": str("Question.")}, "required": []string{"question"},
		}},
		{Name: "get_service_story", Description: "Tell a service story.", InputSchema: map[string]any{
			"type": "object", "properties": map[string]any{"workload_id": str("Workload id.")}, "required": []string{"workload_id"},
		}},
		{Name: "build_evidence_citation_packet", Description: "Cite evidence.", InputSchema: map[string]any{
			"type": "object", "properties": map[string]any{"limit": map[string]any{"type": "integer"}},
		}},
		{Name: "find_divergent_code", Description: "Hidden on production.", InputSchema: map[string]any{
			"type": "object", "properties": map[string]any{},
		}},
	}
}

// fixtureVisible is the profile-filtered list: find_divergent_code is hidden.
func fixtureVisible() []toolcontract.ToolDefinition {
	all := fixtureRegistry()
	return all[:len(all)-1]
}

func fixtureResolve(name string, _ map[string]any) (*routecontract.Request, error) {
	switch name {
	case "find_code":
		return &routecontract.Request{Method: "POST", Path: "/api/v0/code/search"}, nil
	case "execute_cypher_query":
		return &routecontract.Request{Method: "POST", Path: "/api/v0/code/cypher"}, nil
	case "get_repo_summary":
		return &routecontract.Request{Method: "GET", Path: "/api/v0/repositories/placeholder/stats"}, nil
	case "ask":
		return &routecontract.Request{Method: "POST", Path: "/api/v0/ask"}, nil
	case "get_service_story":
		return &routecontract.Request{Method: "GET", Path: "/api/v0/services/placeholder/story"}, nil
	case "build_evidence_citation_packet":
		return &routecontract.Request{Method: "POST", Path: "/api/v0/evidence/citations"}, nil
	case "find_divergent_code":
		return &routecontract.Request{Method: "POST", Path: "/api/v0/code/divergence"}, nil
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

// orderRanker returns every allowed tool in input order. It is the stub the
// dispatch lane uses until the lexical ranker (#8755) lands.
type orderRanker struct{}

func (orderRanker) Rank(_ context.Context, q Query, tools []toolcontract.ToolDefinition,
	_ map[string]catalog.Entry, allowed func(string) bool,
) ([]Candidate, error) {
	var out []Candidate
	for i, tool := range tools {
		if len(out) >= q.Limit {
			break
		}
		if !allowed(tool.Name) {
			continue
		}
		out = append(out, Candidate{Name: tool.Name, Why: "stub", Cost: catalog.CostLow, Score: float64(len(tools) - i)})
	}
	return out, nil
}

// spyRanker records the query it was handed.
type spyRanker struct{ got Query }

func (s *spyRanker) Rank(_ context.Context, q Query, _ []toolcontract.ToolDefinition,
	_ map[string]catalog.Entry, _ func(string) bool,
) ([]Candidate, error) {
	s.got = q
	return nil, nil
}

// sliceSpyRanker records the tool slices it was handed.
type sliceSpyRanker struct {
	mu    sync.Mutex
	lists [][]toolcontract.ToolDefinition
}

func (s *sliceSpyRanker) Rank(_ context.Context, _ Query, tools []toolcontract.ToolDefinition,
	_ map[string]catalog.Entry, _ func(string) bool,
) ([]Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lists = append(s.lists, tools)
	return nil, nil
}

// leakyRanker ignores allowed and returns every tool, to prove the engine
// re-checks access on the ranker's output.
type leakyRanker struct{}

func (leakyRanker) Rank(_ context.Context, q Query, tools []toolcontract.ToolDefinition,
	_ map[string]catalog.Entry, _ func(string) bool,
) ([]Candidate, error) {
	out := make([]Candidate, 0, len(tools))
	for _, tool := range tools {
		out = append(out, Candidate{Name: tool.Name, Why: "leak"})
	}
	return out, nil
}

// recordingDispatcher records calls and returns a sentinel value.
type recordingDispatcher struct {
	mu    sync.Mutex
	calls []string
	args  []map[string]any
	value any
	err   error
}

func (d *recordingDispatcher) dispatch(_ context.Context, name string, args map[string]any) (any, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, name)
	d.args = append(d.args, args)
	return d.value, d.err
}

func newFixtureEngine(t *testing.T, ranker Ranker) *Engine {
	t.Helper()
	return NewEngine(Config{
		Registry: fixtureRegistry(),
		Resolve:  fixtureResolve,
		Ranker:   ranker,
		Entries:  map[string]catalog.Entry{},
	})
}

func fixtureRequest(d *recordingDispatcher) Request {
	return Request{Visible: fixtureVisible(), Session: "s1", Dispatch: d.dispatch}
}

func scopedContext() context.Context {
	return query.ContextWithAuthContext(context.Background(), query.AuthContext{
		Mode: query.AuthModeScoped, TenantID: "t", WorkspaceID: "w", AllowedRepositoryIDs: []string{"r1"},
	})
}

func sharedContext() context.Context {
	return query.ContextWithAuthContext(context.Background(), query.AuthContext{Mode: query.AuthModeShared})
}

// structured decodes a ToolResult's structured content through JSON, the
// way a client sees it.
func structured(t *testing.T, result ToolResult) map[string]any {
	t.Helper()
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	return out
}

func candidateNames(t *testing.T, result ToolResult) []string {
	t.Helper()
	body := structured(t, result)
	raw, _ := body["candidates"].([]any)
	names := make([]string, 0, len(raw))
	for _, c := range raw {
		names = append(names, c.(map[string]any)["name"].(string))
	}
	return names
}

func errorCode(t *testing.T, result ToolResult) string {
	t.Helper()
	body := structured(t, result)
	errObj, _ := body["error"].(map[string]any)
	code, _ := errObj["code"].(string)
	return code
}
