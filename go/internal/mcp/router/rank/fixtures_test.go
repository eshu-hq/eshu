// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// def builds a tool definition whose input schema lists props as optional
// string properties.
func def(name, desc string, props ...string) toolcontract.ToolDefinition {
	properties := map[string]any{}
	for _, p := range props {
		properties[p] = map[string]any{"type": "string"}
	}
	return toolcontract.ToolDefinition{
		Name:        name,
		Description: desc,
		InputSchema: map[string]any{"type": "object", "properties": properties},
	}
}

func allowAll(string) bool { return true }

// names returns the candidate names in order.
func names(cs []router.Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// rankWith builds a fresh ranker over tools/entries and ranks one query with
// every tool allowed.
func rankWith(t *testing.T, tools []toolcontract.ToolDefinition, entries map[string]catalog.Entry,
	q router.Query,
) []router.Candidate {
	t.Helper()
	got, err := New(tools, entries).Rank(context.Background(), q, tools, entries, allowAll)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	return got
}

// fixtureTools is a small registry with distinct vocabulary per tool and no
// description sentence repeated across tools.
func fixtureTools() ([]toolcontract.ToolDefinition, map[string]catalog.Entry) {
	tools := []toolcontract.ToolDefinition{
		def("find_symbol", "Locate a function or type definition by its name in indexed source code.", "repo_id", "name"),
		def("list_repositories", "Enumerate every repository known to the graph.", "limit"),
		def("get_service_context", "Return the deployment story for one service.", "service_id", "environment"),
		def("trace_deployment_chain", "Follow a workload from source repository to running environment.", "workload_id", "repo_id"),
		def("search_content", "Full text search across file bodies.", "query", "repo_id"),
		def("get_dead_code", "Report symbols with no incoming references.", "repo_id"),
	}
	entries := map[string]catalog.Entry{
		"find_symbol":            {Name: "find_symbol", Cost: catalog.CostLow, Intents: []string{"where is this function defined", "jump to a class definition"}},
		"list_repositories":      {Name: "list_repositories", Cost: catalog.CostLow, Intents: []string{"what repositories are indexed"}},
		"get_service_context":    {Name: "get_service_context", Cost: catalog.CostModerate, Intents: []string{"how is this service deployed"}},
		"trace_deployment_chain": {Name: "trace_deployment_chain", Cost: catalog.CostHigh, Intents: []string{"trace a deploy from code to environment"}},
		"search_content":         {Name: "search_content", Cost: catalog.CostModerate, Intents: []string{"grep for text in files"}},
		"get_dead_code":          {Name: "get_dead_code", Cost: catalog.CostModerate, Intents: []string{"find unused functions"}},
	}
	return tools, entries
}
