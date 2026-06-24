// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package askwiring

import (
	"testing"
)

// TestToolsetExcludingAsk_NoAskTool asserts that toolsetExcludingAsk never
// includes a tool named "ask" in the engine toolset. Including "ask" would
// allow the engine to call POST /api/v0/ask in-process during an Ask session,
// recursively invoking itself until the provider context deadline fires.
func TestToolsetExcludingAsk_NoAskTool(t *testing.T) {
	t.Parallel()

	tools := toolsetExcludingAsk(nil)

	for _, tool := range tools {
		if tool.Name == "ask" {
			t.Fatalf("engine toolset contains tool %q; toolsetExcludingAsk must exclude it to prevent recursive engine invocation", tool.Name)
		}
	}
}

// TestToolsetExcludingAsk_ContainsReadTools asserts that toolsetExcludingAsk
// returns a non-empty toolset with expected read-only tool names present.
// This guards against accidentally filtering too broadly.
func TestToolsetExcludingAsk_ContainsReadTools(t *testing.T) {
	t.Parallel()

	tools := toolsetExcludingAsk(nil)

	if len(tools) == 0 {
		t.Fatal("toolsetExcludingAsk returned an empty toolset; expected all read-only tools except ask")
	}

	// Spot-check a few well-known read-only tools are still present.
	required := []string{"find_code", "find_symbol", "get_capability_catalog"}
	byName := make(map[string]bool, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = true
	}
	for _, name := range required {
		if !byName[name] {
			t.Errorf("toolsetExcludingAsk: expected read tool %q to be present, but it is missing", name)
		}
	}
}
