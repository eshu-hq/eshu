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
	const wantHash = "9c3db90d43244691be41aed4e55a8682ec367c756df1ceb3d664269ef9b1cdf2"

	// This hash covers names and registration order only. It is not a metadata
	// or input-schema compatibility guard.
	hash := sha256.New()
	tools := ReadOnlyTools()
	for _, tool := range tools {
		_, _ = fmt.Fprintf(hash, "%d:%s\n", len(tool.Name), tool.Name)
	}
	if got, want := len(tools), 166; got != want {
		t.Fatalf("ReadOnlyTools count = %d, want %d", got, want)
	}
	if got := fmt.Sprintf("%x", hash.Sum(nil)); got != wantHash {
		t.Fatalf("ReadOnlyTools ordered-name hash = %s, want %s", got, wantHash)
	}
}
