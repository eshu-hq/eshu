// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualizationtools

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"
)

func TestToolsPreserveVisualizationRegistrationContract(t *testing.T) {
	t.Parallel()

	tools := Tools()
	if got, want := len(tools), 1; got != want {
		t.Fatalf("visualization tool count = %d, want %d", got, want)
	}
	if got, want := tools[0].Name, "derive_visualization_packet"; got != want {
		t.Fatalf("visualization tool name = %q, want %q", got, want)
	}

	encoded, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal visualization tools: %v", err)
	}
	const wantDefinitionsHash = "9dc648490f77df7c635e5548c7bc1c1a32bb5748ba230e09e78a284509209c9e"
	if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != wantDefinitionsHash {
		t.Fatalf("visualization tool definitions hash = %s, want %s", got, wantDefinitionsHash)
	}
}

func TestToolsReturnIndependentDefinitions(t *testing.T) {
	t.Parallel()

	first := Tools()
	second := Tools()
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("visualization tools are empty")
	}
	first[0] = toolcontract.ToolDefinition{Name: "mutated"}
	if second[0].Name == "mutated" {
		t.Fatal("visualization tool constructors share slice storage")
	}
	firstSchema := Tools()[0].InputSchema.(map[string]any)
	secondSchema := Tools()[0].InputSchema.(map[string]any)
	firstSchema["type"] = "mutated"
	if secondSchema["type"] == "mutated" {
		t.Fatal("visualization tool constructors share schema storage")
	}
}
