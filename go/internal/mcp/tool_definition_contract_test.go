// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"crypto/sha256"
	"fmt"
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

func TestReadOnlyToolsRegistrationOrderContract(t *testing.T) {
	const wantHash = "8256c2bf64a304185a32bfb1924a6ffd8b3439e9d7d82078ba223382360aa45b"

	// This hash covers names and registration order only. B-7 and the MCP
	// schema-drift gate protect tool descriptions and input schemas.
	hash := sha256.New()
	tools := ReadOnlyTools()
	for _, tool := range tools {
		_, _ = fmt.Fprintf(hash, "%d:%s\n", len(tool.Name), tool.Name)
	}
	if got, want := len(tools), 162; got != want {
		t.Fatalf("ReadOnlyTools count = %d, want %d", got, want)
	}
	if got := fmt.Sprintf("%x", hash.Sum(nil)); got != wantHash {
		t.Fatalf("ReadOnlyTools ordered-name hash = %s, want %s", got, wantHash)
	}
}
