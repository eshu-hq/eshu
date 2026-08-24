// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudtools

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"
)

func TestToolsPreserveCloudRegistrationContract(t *testing.T) {
	t.Parallel()

	tools := append(InventoryTools(), RuntimeDriftTools()...)
	wantNames := []string{
		"list_cloud_resource_inventory",
		"list_cloud_runtime_drift_findings",
	}
	gotNames := make([]string, 0, len(tools))
	for _, tool := range tools {
		gotNames = append(gotNames, tool.Name)
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("cloud tool names = %#v, want %#v", gotNames, wantNames)
	}

	encoded, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal cloud tools: %v", err)
	}
	const wantDefinitionsHash = "460ff89408273b10319f5656568df06241b10b137c3d77b3f8c8eba8c709e9d6"
	if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != wantDefinitionsHash {
		t.Fatalf("cloud tool definitions hash = %s, want %s", got, wantDefinitionsHash)
	}
}

func TestToolsReturnIndependentDefinitions(t *testing.T) {
	t.Parallel()

	first := append(InventoryTools(), RuntimeDriftTools()...)
	second := append(InventoryTools(), RuntimeDriftTools()...)
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("cloud tools are empty")
	}
	first[0] = toolcontract.ToolDefinition{Name: "mutated"}
	if second[0].Name == "mutated" {
		t.Fatal("cloud tool constructors share slice storage")
	}
	firstSchema := first[1].InputSchema.(map[string]any)
	secondSchema := second[1].InputSchema.(map[string]any)
	firstSchema["type"] = "mutated"
	if secondSchema["type"] == "mutated" {
		t.Fatal("cloud tool constructors share schema storage")
	}
}
