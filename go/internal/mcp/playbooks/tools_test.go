// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package playbooktools

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"
)

func TestToolsPreserveQueryPlaybookRegistrationContract(t *testing.T) {
	t.Parallel()

	tools := Tools()
	if got, want := len(tools), 2; got != want {
		t.Fatalf("query playbook tool count = %d, want %d", got, want)
	}
	wantNames := []string{"list_query_playbooks", "resolve_query_playbook"}
	for i, want := range wantNames {
		if got := tools[i].Name; got != want {
			t.Fatalf("query playbook tool %d name = %q, want %q", i, got, want)
		}
	}

	encoded, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal query playbook tools: %v", err)
	}
	const wantDefinitionsHash = "ec0199c133c68ffcf2d425e7db2e0faa308102599792952fc6016d590bb15a90"
	if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != wantDefinitionsHash {
		t.Fatalf("query playbook tool definitions hash = %s, want %s", got, wantDefinitionsHash)
	}
}

func TestToolsReturnIndependentDefinitions(t *testing.T) {
	t.Parallel()

	first := Tools()
	second := Tools()
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("query playbook tool counts = %d and %d, want 2 and 2", len(first), len(second))
	}
	first[0] = toolcontract.ToolDefinition{Name: "mutated"}
	if second[0].Name == "mutated" {
		t.Fatal("query playbook constructors share slice storage")
	}

	firstSchema := first[1].InputSchema.(map[string]any)
	secondSchema := second[1].InputSchema.(map[string]any)
	firstSchema["type"] = "mutated"
	if secondSchema["type"] == "mutated" {
		t.Fatal("query playbook constructors share schema storage")
	}
	firstProperties := firstSchema["properties"].(map[string]any)
	secondProperties := secondSchema["properties"].(map[string]any)
	firstPlaybookID := firstProperties["playbook_id"].(map[string]any)
	secondPlaybookID := secondProperties["playbook_id"].(map[string]any)
	firstPlaybookID["description"] = "mutated"
	if secondPlaybookID["description"] == "mutated" {
		t.Fatal("query playbook constructors share nested schema storage")
	}
	firstRequired := firstSchema["required"].([]string)
	secondRequired := secondSchema["required"].([]string)
	firstRequired[0] = "mutated"
	if secondRequired[0] == "mutated" {
		t.Fatal("query playbook constructors share required-field storage")
	}
}
