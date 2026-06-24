// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import "testing"

const sampleInventory = `{
  "version": "v1",
  "surfaces": [
    {"category": "api_route", "name": "GET /api/v0/code/symbols", "readiness": "implemented"},
    {"category": "mcp_tool", "name": "find_symbol", "readiness": "implemented"},
    {"category": "mcp_tool", "name": "draft_tool", "readiness": "draft"},
    {"category": "command", "name": "eshu", "readiness": "implemented"},
    {"category": "reducer_domain", "name": "code_calls", "readiness": "implemented"}
  ]
}`

func TestParseKeepsOnlyImplementedRoutesAndTools(t *testing.T) {
	t.Parallel()
	cat, err := Parse([]byte(sampleInventory))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	got := cat.Entries()
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(got), got)
	}
	// Sorted by (Kind, Name): api_route before mcp_tool.
	if got[0].Kind != KindAPIRoute || got[0].Name != "GET /api/v0/code/symbols" {
		t.Fatalf("entry[0] = %+v", got[0])
	}
	if got[1].Kind != KindMCPTool || got[1].Name != "find_symbol" {
		t.Fatalf("entry[1] = %+v", got[1])
	}
	// Unannotated defaults are conservative.
	if got[0].Backend != BackendUnknown || got[0].Cost != CostHigh {
		t.Fatalf("expected conservative defaults, got %+v", got[0])
	}
}

func TestParseRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	if _, err := Parse([]byte("{not json")); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestParseEmptyInventoryYieldsNoEntries(t *testing.T) {
	t.Parallel()
	cat, err := Parse([]byte(`{"version":"v1","surfaces":[]}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(cat.Entries()) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(cat.Entries()))
	}
}
