// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package asktools

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"
)

func TestToolsPreserveAskRegistrationContract(t *testing.T) {
	t.Parallel()

	tools := Tools()
	if got, want := len(tools), 1; got != want {
		t.Fatalf("ask tool count = %d, want %d", got, want)
	}
	if got, want := tools[0].Name, "ask"; got != want {
		t.Fatalf("ask tool name = %q, want %q", got, want)
	}

	encoded, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal ask tools: %v", err)
	}
	const wantDefinitionsHash = "01422a0903e582b18e10f8e64d784413b0cd1c571880a812117e9d1eab811ff2"
	if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != wantDefinitionsHash {
		t.Fatalf("ask tool definitions hash = %s, want %s", got, wantDefinitionsHash)
	}
}

func TestToolsReturnIndependentDefinitions(t *testing.T) {
	t.Parallel()

	first := Tools()
	second := Tools()
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("ask tools are empty")
	}
	first[0] = toolcontract.ToolDefinition{Name: "mutated"}
	if second[0].Name == "mutated" {
		t.Fatal("ask tool constructors share slice storage")
	}
	firstSchema := Tools()[0].InputSchema.(map[string]any)
	secondSchema := Tools()[0].InputSchema.(map[string]any)
	firstSchema["type"] = "mutated"
	if secondSchema["type"] == "mutated" {
		t.Fatal("ask tool constructors share schema storage")
	}
}
