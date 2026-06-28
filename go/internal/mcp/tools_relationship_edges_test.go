// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/sourcetool"
)

func TestRelationshipEdgesToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_relationship_edges")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"verb", "source_tool", "limit"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestRelationshipEdgesToolRequiresVerb(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_relationship_edges")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("required type = %T, want []string", schema["required"])
	}
	found := false
	for _, f := range required {
		if f == "verb" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("required = %#v, want verb listed", required)
	}
}

func TestRelationshipEdgesToolSourceToolEnumMatchesCanonical(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_relationship_edges")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties := schema["properties"].(map[string]any)
	sourceTool := properties["source_tool"].(map[string]any)
	enum, ok := sourceTool["enum"].([]any)
	if !ok {
		t.Fatalf("source_tool enum type = %T, want []any", sourceTool["enum"])
	}
	if got, want := len(enum), len(sourcetool.Canonical); got != want {
		t.Fatalf("len(source_tool enum) = %d, want %d (sourctool.Canonical length)", got, want)
	}
	canonicalSet := make(map[string]bool, len(sourcetool.Canonical))
	for _, v := range sourcetool.Canonical {
		canonicalSet[v] = true
	}
	for _, raw := range enum {
		token, ok := raw.(string)
		if !ok {
			t.Fatalf("enum value %#v type = %T, want string", raw, raw)
		}
		if !canonicalSet[token] {
			t.Fatalf("enum contains %q which is not in sourcetool.Canonical", token)
		}
	}
}
