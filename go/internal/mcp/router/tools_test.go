// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func schemaProps(t *testing.T, schema any) map[string]any {
	t.Helper()
	m, ok := schema.(map[string]any)
	if !ok {
		t.Fatalf("schema is %T, want map[string]any", schema)
	}
	if m["type"] != "object" {
		t.Fatalf("schema type = %v, want object", m["type"])
	}
	props, ok := m["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties is %T, want map[string]any", m["properties"])
	}
	return props
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func requiredOf(t *testing.T, schema any) []string {
	t.Helper()
	req, _ := schema.(map[string]any)["required"].([]string)
	out := append([]string(nil), req...)
	sort.Strings(out)
	return out
}

func TestToolsOrderAndNames(t *testing.T) {
	t.Parallel()
	tools := Tools()
	if len(tools) != 2 || tools[0].Name != "find_tool" || tools[1].Name != "call_tool" {
		t.Fatalf("Tools() names = %+v, want [find_tool call_tool]", tools)
	}
	if FindToolName != "find_tool" || CallToolName != "call_tool" {
		t.Fatalf("name constants = %q, %q", FindToolName, CallToolName)
	}
	for _, tool := range tools {
		if tool.Description == "" {
			t.Errorf("%s has an empty description", tool.Name)
		}
	}
}

func TestFindToolSchema(t *testing.T) {
	t.Parallel()
	schema := Tools()[0].InputSchema
	props := schemaProps(t, schema)
	if got, want := sortedKeys(props), []string{"anchors", "intent", "limit"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("find_tool properties = %v, want %v", got, want)
	}
	if got, want := requiredOf(t, schema), []string{"intent"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("find_tool required = %v, want %v", got, want)
	}
	if typ := props["intent"].(map[string]any)["type"]; typ != "string" {
		t.Fatalf("intent type = %v, want string", typ)
	}
	limit := props["limit"].(map[string]any)
	if limit["type"] != "integer" || limit["minimum"] != 1 || limit["maximum"] != MaxLimit || limit["default"] != DefaultLimit {
		t.Fatalf("limit = %v, want integer min 1 max 10 default 5", limit)
	}
	anchors := props["anchors"].(map[string]any)
	if anchors["type"] != "object" {
		t.Fatalf("anchors type = %v, want object", anchors["type"])
	}
	anchorProps := anchors["properties"].(map[string]any)
	want := []string{"environment", "repo_id", "service_id", "workload_id"}
	if got := sortedKeys(anchorProps); !reflect.DeepEqual(got, want) {
		t.Fatalf("anchor properties = %v, want %v", got, want)
	}
	for name, p := range anchorProps {
		if p.(map[string]any)["type"] != "string" {
			t.Errorf("anchor %s type = %v, want string", name, p.(map[string]any)["type"])
		}
	}
	if _, ok := anchors["required"]; ok {
		t.Fatalf("anchors must have no required fields, got %v", anchors["required"])
	}
}

func TestCallToolSchema(t *testing.T) {
	t.Parallel()
	schema := Tools()[1].InputSchema
	props := schemaProps(t, schema)
	if got, want := sortedKeys(props), []string{"arguments", "name"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("call_tool properties = %v, want %v", got, want)
	}
	if got, want := requiredOf(t, schema), []string{"arguments", "name"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("call_tool required = %v, want %v", got, want)
	}
	if props["name"].(map[string]any)["type"] != "string" {
		t.Fatalf("name type = %v, want string", props["name"].(map[string]any)["type"])
	}
	if desc := Tools()[1].Description; !strings.Contains(desc, "find_tool, call_tool, or ask") {
		t.Fatalf("call_tool description must name the refused targets find_tool, call_tool, and ask: %q", desc)
	}
	if props["arguments"].(map[string]any)["type"] != "object" {
		t.Fatalf("arguments type = %v, want object", props["arguments"].(map[string]any)["type"])
	}
}

func TestLimitConstants(t *testing.T) {
	t.Parallel()
	if DefaultLimit != 5 || MaxLimit != 10 {
		t.Fatalf("DefaultLimit, MaxLimit = %d, %d, want 5, 10", DefaultLimit, MaxLimit)
	}
}

func TestToolsReturnFreshCopies(t *testing.T) {
	t.Parallel()
	first := Tools()
	first[0].Name = "mutated"
	first[0].InputSchema.(map[string]any)["type"] = "mutated"
	second := Tools()
	if second[0].Name != "find_tool" || second[0].InputSchema.(map[string]any)["type"] != "object" {
		t.Fatalf("mutating a Tools() result leaked into a later call: %+v", second[0])
	}
}
