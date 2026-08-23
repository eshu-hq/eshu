// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"
)

func TestToolDefinitionAliasPreservesNeutralContractIdentity(t *testing.T) {
	neutral := toolcontract.ToolDefinition{
		Name:        "sample_tool",
		Description: "sample description",
		InputSchema: map[string]any{"type": "object"},
	}

	rootDefinitions := []ToolDefinition{neutral}
	roundTripDefinitions := []toolcontract.ToolDefinition{rootDefinitions[0]}
	roundTrip := roundTripDefinitions[0]
	if reflect.TypeOf(rootDefinitions[0]) != reflect.TypeOf(neutral) {
		t.Fatalf("root ToolDefinition type = %v, want %v", reflect.TypeOf(rootDefinitions[0]), reflect.TypeOf(neutral))
	}
	if !reflect.DeepEqual(roundTrip, neutral) {
		t.Fatalf("round-trip ToolDefinition = %#v, want %#v", roundTrip, neutral)
	}
}
