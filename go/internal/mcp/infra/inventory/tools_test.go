// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package infrainventorytools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

func TestToolsOwnsAggregateDefinitionsInRegistrationOrder(t *testing.T) {
	t.Parallel()

	var got []string
	for _, definition := range Tools() {
		got = append(got, definition.Name)
	}
	want := []string{"count_infra_resources", "get_infra_resource_inventory"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tools() names = %q, want %q", got, want)
	}
}

func TestToolsReturnIndependentDefinitions(t *testing.T) {
	t.Parallel()

	first := Tools()
	second := Tools()
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("infra inventory tools are empty")
	}
	first[0] = toolcontract.ToolDefinition{Name: "mutated"}
	if second[0].Name == "mutated" {
		t.Fatal("infra inventory tool constructors share slice storage")
	}
	firstSchema := Tools()[0].InputSchema.(map[string]any)
	secondSchema := Tools()[0].InputSchema.(map[string]any)
	firstSchema["type"] = "mutated"
	if secondSchema["type"] == "mutated" {
		t.Fatal("infra inventory tool constructors share schema storage")
	}
}
