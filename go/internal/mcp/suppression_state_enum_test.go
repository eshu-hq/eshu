// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"encoding/json"
	"testing"
)

// TestSuppressionStateEnumsListAdvisoryRetracted proves every MCP tool schema
// that lists the reducer suppression states also lists advisory_retracted
// (#9695). Without it a client that validates against the schema could not
// filter for a retracted finding, though the API accepts the value.
func TestSuppressionStateEnumsListAdvisoryRetracted(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(ReadOnlyTools())
	if err != nil {
		t.Fatalf("marshal tools: %v", err)
	}
	var tools any
	if err := json.Unmarshal(raw, &tools); err != nil {
		t.Fatalf("unmarshal tools: %v", err)
	}
	var found, missing int
	var walk func(any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			if values, ok := value["enum"].([]any); ok && enumHas(values, "scope_mismatch") {
				found++
				if !enumHas(values, "advisory_retracted") {
					missing++
				}
			}
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(tools)
	if found == 0 {
		t.Fatal("no suppression_state enum found in the MCP tools; the test is vacuous")
	}
	if missing != 0 {
		t.Fatalf("%d of %d suppression-state enums lack advisory_retracted", missing, found)
	}
}

func enumHas(values []any, want string) bool {
	for _, value := range values {
		if text, ok := value.(string); ok && text == want {
			return true
		}
	}
	return false
}
