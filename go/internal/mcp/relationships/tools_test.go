// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshiptools

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
)

func TestToolPreservesRelationshipEdgesRegistrationContract(t *testing.T) {
	t.Parallel()

	tool := Tool()
	if got, want := tool.Name, "list_relationship_edges"; got != want {
		t.Fatalf("relationship-edge tool name = %q, want %q", got, want)
	}

	encoded, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal relationship-edge tool: %v", err)
	}
	const wantDefinitionHash = "d3c56a788ae3818221a05c3ccb28a7a7a278c27ffdb8aa3722bcfe785e657ca3"
	if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != wantDefinitionHash {
		t.Fatalf("relationship-edge tool definition hash = %s, want %s", got, wantDefinitionHash)
	}
}

func TestToolReturnsIndependentDefinition(t *testing.T) {
	t.Parallel()

	first := Tool()
	second := Tool()
	first.Name = "mutated"
	if second.Name == "mutated" {
		t.Fatal("relationship-edge constructors share top-level storage")
	}

	firstSchema := first.InputSchema.(map[string]any)
	secondSchema := second.InputSchema.(map[string]any)
	firstSchema["type"] = "mutated"
	if secondSchema["type"] == "mutated" {
		t.Fatal("relationship-edge constructors share schema storage")
	}
	firstProperties := firstSchema["properties"].(map[string]any)
	secondProperties := secondSchema["properties"].(map[string]any)
	firstSourceTool := firstProperties["source_tool"].(map[string]any)
	secondSourceTool := secondProperties["source_tool"].(map[string]any)
	firstEnum := firstSourceTool["enum"].([]any)
	secondEnum := secondSourceTool["enum"].([]any)
	firstEnum[0] = "mutated"
	if secondEnum[0] == "mutated" {
		t.Fatal("relationship-edge constructors share nested enum storage")
	}
	firstRequired := firstSchema["required"].([]string)
	secondRequired := secondSchema["required"].([]string)
	firstRequired[0] = "mutated"
	if secondRequired[0] == "mutated" {
		t.Fatal("relationship-edge constructors share required-field storage")
	}
}
