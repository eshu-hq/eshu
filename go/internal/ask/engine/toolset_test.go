package engine

import (
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp"
)

// inventoryFor builds a minimal surface-inventory JSON for the given MCP tool
// names so catalog.Parse returns a Catalog containing them.
func inventoryFor(names ...string) []byte {
	surfaces := `[`
	for i, name := range names {
		if i > 0 {
			surfaces += `,`
		}
		surfaces += `{"category":"mcp_tool","name":"` + name + `","readiness":"implemented"}`
	}
	surfaces += `]`
	return []byte(`{"version":"test","surfaces":` + surfaces + `}`)
}

// TestToolset_NilCatalog verifies that a nil catalog returns tools ordered by
// Name, carrying correct Name, Description, and InputSchema values.
func TestToolset_NilCatalog(t *testing.T) {
	t.Parallel()

	schema := map[string]any{"type": "object", "properties": map[string]any{}}
	defs := []mcp.ToolDefinition{
		{Name: "zebra_tool", Description: "desc z", InputSchema: schema},
		{Name: "alpha_tool", Description: "desc a", InputSchema: schema},
		{Name: "middle_tool", Description: "desc m", InputSchema: schema},
	}

	tools := Toolset(nil, defs)

	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}

	// With nil catalog all tools are unknown-cost, so ordering is by Name.
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("expected tools sorted by Name with nil catalog, got %v", names)
	}

	// Spot-check first tool (alpha_tool) carries correct fields.
	first := tools[0]
	if first.Name != "alpha_tool" {
		t.Errorf("first tool name = %q, want %q", first.Name, "alpha_tool")
	}
	if first.Description != "desc a" {
		t.Errorf("first tool description = %q, want %q", first.Description, "desc a")
	}
	if first.InputSchema == nil {
		t.Error("InputSchema must not be nil")
	}
	if _, ok := first.InputSchema["type"]; !ok {
		t.Error("InputSchema missing 'type' key")
	}
}

// TestToolset_InputSchemaRoundTrip verifies that a map[string]any InputSchema is
// preserved exactly, and a non-map InputSchema yields an empty non-nil map
// without panicking.
func TestToolset_InputSchemaRoundTrip(t *testing.T) {
	t.Parallel()

	schema := map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}}
	defs := []mcp.ToolDefinition{
		{Name: "mapped_tool", Description: "has real schema", InputSchema: schema},
		{Name: "nonmap_tool", Description: "has bad schema", InputSchema: "not-a-map"},
		{Name: "nil_schema_tool", Description: "nil schema", InputSchema: nil},
	}

	tools := Toolset(nil, defs)

	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}

	// Find each by name.
	byName := make(map[string]interface{})
	for _, tool := range tools {
		byName[tool.Name] = tool.InputSchema
	}

	// map[string]any schema round-trips.
	mappedSchema, ok := byName["mapped_tool"].(map[string]any)
	if !ok {
		t.Fatalf("mapped_tool InputSchema is not map[string]any")
	}
	if mappedSchema["type"] != "object" {
		t.Errorf("mapped_tool InputSchema type = %v, want %q", mappedSchema["type"], "object")
	}

	// Non-map schema yields empty non-nil map.
	nonmapSchema, ok := byName["nonmap_tool"].(map[string]any)
	if !ok {
		t.Fatalf("nonmap_tool InputSchema is not map[string]any")
	}
	if len(nonmapSchema) != 0 {
		t.Errorf("nonmap_tool InputSchema should be empty, got %v", nonmapSchema)
	}

	// nil schema also yields empty non-nil map.
	nilSchema, ok := byName["nil_schema_tool"].(map[string]any)
	if !ok {
		t.Fatalf("nil_schema_tool InputSchema is not map[string]any")
	}
	if len(nilSchema) != 0 {
		t.Errorf("nil_schema_tool InputSchema should be empty, got %v", nilSchema)
	}
}

// TestToolset_UnknownCostSortsLast verifies that a tool not present in the
// catalog is placed after all catalog-known entries.
func TestToolset_UnknownCostSortsLast(t *testing.T) {
	t.Parallel()

	// Build a catalog with two known tool names (will default to CostHigh since
	// the curated overlay does not cover synthetic names).
	knownNames := []string{"alpha_known", "beta_known"}
	cat, err := catalog.Parse(inventoryFor(knownNames...))
	if err != nil {
		t.Fatalf("catalog.Parse: %v", err)
	}

	schema := map[string]any{"type": "object"}
	defs := []mcp.ToolDefinition{
		{Name: "zzz_unknown", Description: "not in catalog", InputSchema: schema},
		{Name: "alpha_known", Description: "in catalog", InputSchema: schema},
		{Name: "beta_known", Description: "in catalog", InputSchema: schema},
	}

	tools := Toolset(cat, defs)

	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}

	// The tool not in the catalog must sort after the two known tools.
	last := tools[len(tools)-1]
	if last.Name != "zzz_unknown" {
		t.Errorf("expected unknown tool last, got %q", last.Name)
	}
}

// TestCostRank verifies the full ordering: CostLow < CostModerate < CostHigh < unknown/zero CostClass.
func TestCostRank(t *testing.T) {
	t.Parallel()

	lowRank := costRank(catalog.CostLow)
	modRank := costRank(catalog.CostModerate)
	highRank := costRank(catalog.CostHigh)
	unknownRank := costRank(catalog.CostClass(""))

	if lowRank >= modRank {
		t.Errorf("CostLow rank (%d) must be less than CostModerate rank (%d)", lowRank, modRank)
	}
	if modRank >= highRank {
		t.Errorf("CostModerate rank (%d) must be less than CostHigh rank (%d)", modRank, highRank)
	}
	if highRank >= unknownRank {
		t.Errorf("CostHigh rank (%d) must be less than unknown CostClass rank (%d)", highRank, unknownRank)
	}
}

// TestToolset_EmptyDefs verifies that an empty defs slice returns an empty
// (non-nil) slice without panicking.
func TestToolset_EmptyDefs(t *testing.T) {
	t.Parallel()

	tools := Toolset(nil, nil)
	if tools == nil {
		t.Error("Toolset(nil, nil) must return non-nil slice")
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}
