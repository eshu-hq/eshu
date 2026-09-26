// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctools

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

func TestToolsPreserveDocumentationRegistrationContract(t *testing.T) {
	t.Parallel()

	tools := append(Tools(), FindingAggregateTools()...)
	wantNames := []string{
		"list_documentation_findings",
		"list_documentation_facts",
		"get_documentation_evidence_packet",
		"check_documentation_evidence_packet_freshness",
		"count_documentation_findings",
		"get_documentation_finding_inventory",
	}
	gotNames := make([]string, 0, len(tools))
	for _, tool := range tools {
		gotNames = append(gotNames, tool.Name)
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("documentation tool names = %#v, want %#v", gotNames, wantNames)
	}

	encoded, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal documentation tools: %v", err)
	}
	// Re-pinned for #7128: list_documentation_facts now documents that a call
	// without generation_id reads the scope's active generation.
	const wantDefinitionsHash = "2d1925a9c2459764c9d535c54e9786c2a4509a7ddc472d5a77ac4ca3085c7717"
	if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != wantDefinitionsHash {
		t.Fatalf("documentation tool definitions hash = %s, want %s", got, wantDefinitionsHash)
	}
}

func TestToolsReturnIndependentDefinitions(t *testing.T) {
	t.Parallel()

	first := append(Tools(), FindingAggregateTools()...)
	second := append(Tools(), FindingAggregateTools()...)
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("documentation tools are empty")
	}
	first[0] = toolcontract.ToolDefinition{Name: "mutated"}
	if second[0].Name == "mutated" {
		t.Fatal("documentation tool constructors share slice storage")
	}
	firstSchema := first[1].InputSchema.(map[string]any)
	secondSchema := second[1].InputSchema.(map[string]any)
	firstSchema["type"] = "mutated"
	if secondSchema["type"] == "mutated" {
		t.Fatal("documentation tool constructors share schema storage")
	}
}
