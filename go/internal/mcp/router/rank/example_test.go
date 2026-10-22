// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

func exampleTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "zebra_lookup",
		Description: "Looks up a zebra.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_id":     map[string]any{"type": "string"},
				"environment": map[string]any{"type": "string"},
				"zebra":       map[string]any{"type": "string"},
				"limit":       map[string]any{"type": "integer", "default": 25},
				"verbose":     map[string]any{"type": "boolean"},
				"count":       map[string]any{"type": "integer"},
				"tags":        map[string]any{"type": "array"},
			},
			"required": []string{"zebra", "count", "tags"},
		},
	}
}

func TestExampleFromRequiredDefaultsAndAnchors(t *testing.T) {
	tools := []toolcontract.ToolDefinition{exampleTool()}
	q := router.Query{Intent: "zebra", Limit: 1}
	q.Anchors.RepoID = "repo-9"
	got := rankWith(t, tools, nil, q)
	if len(got) != 1 {
		t.Fatalf("got %v, want 1 candidate", names(got))
	}
	ex := got[0].Example
	want := map[string]any{
		"repo_id": "repo-9",  // supplied anchor the tool accepts
		"zebra":   "<zebra>", // required string placeholder
		"count":   1,         // required integer placeholder
		"tags":    []any{},   // required array placeholder
		"limit":   25,        // schema default
	}
	if fmt.Sprint(ex) != fmt.Sprint(want) {
		t.Fatalf("Example = %v, want %v", ex, want)
	}
	if _, ok := ex["environment"]; ok {
		t.Fatalf("Example includes an anchor the query did not supply: %v", ex)
	}
	if _, ok := ex["verbose"]; ok {
		t.Fatalf("Example includes an optional property with no default: %v", ex)
	}
}

func TestExampleRequiredAsAnySlice(t *testing.T) {
	def := exampleTool()
	def.InputSchema.(map[string]any)["required"] = []any{"zebra"}
	got := rankWith(t, []toolcontract.ToolDefinition{def}, nil, router.Query{Intent: "zebra", Limit: 1})
	if ex := got[0].Example; fmt.Sprint(ex) != fmt.Sprint(map[string]any{"zebra": "<zebra>", "limit": 25}) {
		t.Fatalf("Example = %v", ex)
	}
}

func TestExampleIsNilForNonMapSchemaAndEmptyForNoArgs(t *testing.T) {
	noSchema := toolcontract.ToolDefinition{Name: "zebra_a", Description: "zebra"}
	noArgs := toolcontract.ToolDefinition{Name: "zebra_b", Description: "zebra", InputSchema: map[string]any{"type": "object"}}
	got := rankWith(t, []toolcontract.ToolDefinition{noSchema, noArgs}, nil, router.Query{Intent: "zebra", Limit: 5})
	byName := map[string]router.Candidate{}
	for _, c := range got {
		byName[c.Name] = c
	}
	if byName["zebra_a"].Example != nil || byName["zebra_a"].InputSchema != nil {
		t.Fatalf("non-map schema: Example/InputSchema = %v/%v, want nil", byName["zebra_a"].Example, byName["zebra_a"].InputSchema)
	}
	if ex := byName["zebra_b"].Example; ex == nil || len(ex) != 0 {
		t.Fatalf("no-arg tool Example = %v, want an empty object", ex)
	}
}

func TestExampleDoesNotAliasSchemaDefaults(t *testing.T) {
	def := exampleTool()
	props := def.InputSchema.(map[string]any)["properties"].(map[string]any)
	props["opts"] = map[string]any{"type": "object", "default": map[string]any{"a": 1}}
	got := rankWith(t, []toolcontract.ToolDefinition{def}, nil, router.Query{Intent: "zebra", Limit: 1})
	ex := got[0].Example["opts"].(map[string]any)
	ex["a"] = 2
	if props["opts"].(map[string]any)["default"].(map[string]any)["a"] != 1 {
		t.Fatalf("mutating Example changed the registry schema default")
	}
}
