// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergencetools

import (
	"testing"

	routecontract "github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

// TestRouteMapsDivergenceTools pins the tool-to-path contract both API and
// MCP answers share: each tool routes to its POST path with the handler's
// body keys, and nothing outside the family is claimed.
func TestRouteMapsDivergenceTools(t *testing.T) {
	t.Parallel()

	find, ok := Route("find_code_divergence", routecontract.Arguments{
		"repo_id": "repo-x", "kind": "exact", "limit": 5, "offset": 2, "include_tests": true,
	})
	if !ok {
		t.Fatal("find_code_divergence must route")
	}
	if find.Method != "POST" || find.Path != "/api/v0/code/divergence/findings" {
		t.Fatalf("find route = %s %s, want POST /api/v0/code/divergence/findings", find.Method, find.Path)
	}
	findBody, ok := find.Body.(map[string]any)
	if !ok {
		t.Fatalf("find body type = %T, want map[string]any", find.Body)
	}
	for _, key := range []string{"repo_id", "kind", "limit", "offset", "include_tests"} {
		if _, present := findBody[key]; !present {
			t.Fatalf("find body must carry %q, got %v", key, findBody)
		}
	}

	report, ok := Route("report_code_divergence", routecontract.Arguments{
		"repo_id": "repo-x", "top_per_kind": 2, "include_tests": true,
	})
	if !ok {
		t.Fatal("report_code_divergence must route")
	}
	if report.Method != "POST" || report.Path != "/api/v0/code/divergence/report" {
		t.Fatalf("report route = %s %s, want POST /api/v0/code/divergence/report", report.Method, report.Path)
	}
	reportBody, ok := report.Body.(map[string]any)
	if !ok {
		t.Fatalf("report body type = %T, want map[string]any", report.Body)
	}
	for _, key := range []string{"repo_id", "top_per_kind", "include_tests"} {
		if _, present := reportBody[key]; !present {
			t.Fatalf("report body must carry %q, got %v", key, reportBody)
		}
	}

	investigate, ok := Route("investigate_code_divergence", routecontract.Arguments{
		"repo_id": "repo-x", "kind": "renamed", "fingerprint": "fp-x",
	})
	if !ok {
		t.Fatal("investigate_code_divergence must route")
	}
	if investigate.Method != "POST" || investigate.Path != "/api/v0/code/divergence/investigate" {
		t.Fatalf("investigate route = %s %s, want POST /api/v0/code/divergence/investigate", investigate.Method, investigate.Path)
	}

	if _, ok := Route("find_most_complex_functions", nil); ok {
		t.Fatal("divergence family must not claim quality tools")
	}
	if _, ok := Route("find_code_divergence_alias", nil); ok {
		t.Fatal("divergence family must not claim aliases")
	}
}

// TestToolsPinsRegistrationShape pins the three-tool registration: names,
// required args, and the limit default the handler agrees with.
func TestToolsPinsRegistrationShape(t *testing.T) {
	t.Parallel()

	defs := Tools()
	if len(defs) != 3 {
		t.Fatalf("tools = %d, want 3", len(defs))
	}
	byName := map[string]int{}
	for _, def := range defs {
		byName[def.Name]++
	}
	if byName["find_code_divergence"] != 1 || byName["investigate_code_divergence"] != 1 || byName["report_code_divergence"] != 1 {
		t.Fatalf("tool names = %v, want one of each", byName)
	}
	for i, want := range []string{"find_code_divergence", "investigate_code_divergence", "report_code_divergence"} {
		if defs[i].Name != want {
			t.Fatalf("tools[%d] = %q, want %q (find/investigate adjacency is pinned)", i, defs[i].Name, want)
		}
	}
}
