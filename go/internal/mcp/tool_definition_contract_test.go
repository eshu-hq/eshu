// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
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
	const wantHash = "7c611d8bb15abdae3f27f0eb3e790d78a219f96bf7a1b1cfd6e29c0e9cab2434"

	// This hash covers names and registration order only. It is not a metadata
	// or input-schema compatibility guard.
	hash := sha256.New()
	tools := ReadOnlyTools()
	for _, tool := range tools {
		_, _ = fmt.Fprintf(hash, "%d:%s\n", len(tool.Name), tool.Name)
	}
	if got, want := len(tools), 165; got != want {
		t.Fatalf("ReadOnlyTools count = %d, want %d", got, want)
	}
	if got := fmt.Sprintf("%x", hash.Sum(nil)); got != wantHash {
		t.Fatalf("ReadOnlyTools ordered-name hash = %s, want %s", got, wantHash)
	}
}
