// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package toolcontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestApicontractGoldenMatchesLiveType locks the published SDK tool-definition
// golden to this package's ToolDefinition. The MCP server builds tools/list
// from these definitions, so a tag rename here changes the advertised wire
// shape: this gate fails until the SDK golden and the sdk/go/apicontract
// shape move in the same change.
func TestApicontractGoldenMatchesLiveType(t *testing.T) {
	t.Parallel()
	raw, err := json.MarshalIndent(ToolDefinition{
		Name:        "search_code",
		Description: "Search code across repositories",
		InputSchema: map[string]any{"type": "object"},
	}, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw = append(raw, '\n')
	golden := filepath.Join("..", "..", "..", "..", "..", "sdk", "go", "apicontract", "testdata", "golden", "tool_definition.json")
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden %s: %v", golden, err)
	}
	if string(raw) != string(want) {
		t.Fatalf("live ToolDefinition drifted from golden:\n got: %s\nwant: %s", raw, want)
	}
}
