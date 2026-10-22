// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// exampleOf ranks one tool for "zebra" and returns its Example.
func exampleOf(t *testing.T, schema map[string]any, anchors router.Anchors) map[string]any {
	t.Helper()
	tools := []toolcontract.ToolDefinition{{Name: "zebra_tool", Description: "Zebra.", InputSchema: schema}}
	got := rankWith(t, tools, nil, router.Query{Intent: "zebra", Limit: 1, Anchors: anchors})
	if len(got) != 1 {
		t.Fatalf("got %v, want one candidate", names(got))
	}
	return got[0].Example
}

// prop builds a property schema.
func prop(typ any, extra map[string]any) map[string]any {
	out := map[string]any{"type": typ}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func TestExampleEnumFirstValue(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"kind_strings": prop("string", map[string]any{"enum": []string{"exact", "renamed"}}),
			"kind_any":     prop("string", map[string]any{"enum": []any{"keyword", "semantic"}}),
			"level_ints":   prop("integer", map[string]any{"enum": []any{3, 4}}),
			"opt_enum":     prop("string", map[string]any{"enum": []string{"never", "shown"}}),
			"empty_str":    prop("string", map[string]any{"enum": []string{}}),
			"empty_any":    prop("string", map[string]any{"enum": []any{}}),
		},
		"required": []string{"kind_strings", "kind_any", "level_ints", "empty_str", "empty_any"},
	}
	want := map[string]any{
		"kind_strings": "exact",
		"kind_any":     "keyword",
		"level_ints":   3,
		"empty_str":    "<empty_str>", // an empty enum falls back to the type placeholder
		"empty_any":    "<empty_any>",
	}
	if got := exampleOf(t, schema, router.Anchors{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("Example = %#v, want %#v", got, want)
	}
}

func TestExampleDefaultBeatsEnum(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"mode": prop("string", map[string]any{"enum": []string{"a", "b"}, "default": "b"}),
		},
		"required": []string{"mode"},
	}
	if got := exampleOf(t, schema, router.Anchors{}); got["mode"] != "b" {
		t.Fatalf("Example = %#v, want the schema default b", got)
	}
}

// TestExampleHandlesAlternateSchemaShapes: a schema built with typed Go
// containers instead of map[string]any must give the same example and the same
// anchor preference as the map[string]any form.
func TestExampleHandlesAlternateSchemaShapes(t *testing.T) {
	typedProps := map[string]any{
		"type": "object",
		"properties": map[string]map[string]any{
			"repo_id": {"type": "string"},
			"kind":    {"type": "string", "enum": []string{"exact"}},
		},
		"required": []string{"kind"},
	}
	anchors := router.Anchors{RepoID: "r-1"}
	want := map[string]any{"repo_id": "r-1", "kind": "exact"}
	if got := exampleOf(t, typedProps, anchors); !reflect.DeepEqual(got, want) {
		t.Fatalf("typed properties: Example = %#v, want %#v", got, want)
	}

	// JSON-schema style type list: ["string", "null"] is a string.
	listType := map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": prop([]any{"null", "string"}, nil), "n": prop([]string{"integer", "null"}, nil)},
		"required":   []string{"name", "n"},
	}
	if got := exampleOf(t, listType, router.Anchors{}); !reflect.DeepEqual(got, map[string]any{"name": "<name>", "n": 1}) {
		t.Fatalf("type list: Example = %#v", got)
	}
}

func TestAnchorPreferenceReadsTypedPropertyMaps(t *testing.T) {
	typed := toolcontract.ToolDefinition{Name: "gamma_b", Description: "Locates the zebra.", InputSchema: map[string]any{
		"type":       "object",
		"properties": map[string]map[string]any{"repo_id": {"type": "string"}},
	}}
	plain := def("gamma_a", "Locates the zebra.", "name")
	q := router.Query{Intent: "zebra", Limit: 5, Anchors: router.Anchors{RepoID: "r-1"}}
	got := rankWith(t, []toolcontract.ToolDefinition{plain, typed}, nil, q)
	if got[0].Name != "gamma_b" {
		t.Fatalf("order = %v, want gamma_b first: the anchor boost must see typed property maps", names(got))
	}
}

func TestExampleClonesTypedSliceDefaults(t *testing.T) {
	defaults := []string{"x", "y"}
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"tags": prop("array", map[string]any{"default": defaults})},
	}
	ex := exampleOf(t, schema, router.Anchors{})
	got, ok := ex["tags"].([]string)
	if !ok || !reflect.DeepEqual(got, []string{"x", "y"}) {
		t.Fatalf("Example tags = %#v, want [x y]", ex["tags"])
	}
	got[0] = "changed"
	if defaults[0] != "x" {
		t.Fatalf("mutating Example changed the registry default")
	}
}
