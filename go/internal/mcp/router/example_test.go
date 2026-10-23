// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// TestExampleArgumentsUsesFirstEnumValueForEitherEnumType covers the #8755
// review note: registered schemas declare enums as []string as well as []any,
// including on required properties, and the example must be directly callable.
func TestExampleArgumentsUsesFirstEnumValueForEitherEnumType(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query_type": map[string]any{"type": "string", "enum": []string{"what_depends_on", "blast_radius"}},
			"view":       map[string]any{"type": "string", "enum": []any{"summary", "full"}},
			"optional":   map[string]any{"type": "string", "enum": []string{"x"}},
		},
		"required": []string{"query_type", "view"},
	}
	example := ExampleArguments(schema)
	if example["query_type"] != "what_depends_on" || example["view"] != "summary" {
		t.Fatalf("ExampleArguments() = %v, want the first enum value of each required property", example)
	}
	if _, ok := example["optional"]; ok {
		t.Fatalf("ExampleArguments() = %v, want optional properties left out", example)
	}
	if errs := ValidateArguments(schema, example); len(errs) != 0 {
		t.Fatalf("example %v fails its own schema: %v", example, errs)
	}
	for _, bad := range []map[string]any{{"query_type": "nope", "view": "summary"}, {"query_type": "blast_radius", "view": "nope"}} {
		if errs := ValidateArguments(schema, bad); len(errs) != 1 || errs[0].Keyword != "enum" {
			t.Fatalf("ValidateArguments(%v) = %v, want one enum error for either enum type", bad, errs)
		}
	}
}

// TestEngineExampleFillsAOneOfSelector covers tools such as get_service_story
// whose route needs one of several optional selectors: the engine adds the
// first property, in name order, that makes the route resolve.
func TestEngineExampleFillsAOneOfSelector(t *testing.T) {
	t.Parallel()

	tool := toolcontract.ToolDefinition{Name: "story", InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"environment":  map[string]any{"type": "string"},
			"service_name": map[string]any{"type": "string"},
			"workload_id":  map[string]any{"type": "string"},
		},
	}}
	resolve := func(_ string, args map[string]any) (*routecontract.Request, error) {
		if args["service_name"] == nil && args["workload_id"] == nil {
			return nil, errors.New("requires workload_id or service_name")
		}
		return &routecontract.Request{Method: "GET", Path: "/api/v0/story"}, nil
	}
	engine := NewEngine(Config{Registry: []toolcontract.ToolDefinition{tool}, Resolve: resolve, Entries: map[string]catalog.Entry{}})
	example, ok := engine.Example("story")
	if !ok || len(example) != 1 || example["service_name"] == nil {
		t.Fatalf("Example(story) = %v, want only service_name filled", example)
	}
	example["mutated"] = true
	if again, _ := engine.Example("story"); again["mutated"] != nil {
		t.Fatalf("Example returned a shared map")
	}
}
