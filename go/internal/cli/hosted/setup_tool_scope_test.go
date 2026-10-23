// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hosted

import (
	"strings"
	"testing"
)

// TestSetupToolCountNamesItsScope proves the MCP tools stage says the count
// comes from this build's registered tools, and that a deployment can advertise
// fewer, so a router-surface user is not misled by the number.
func TestSetupToolCountNamesItsScope(t *testing.T) {
	t.Parallel()
	result, err := ExecuteSetup(okDeps(), baseSetupOptions())
	if err != nil {
		t.Fatalf("ExecuteSetup err = %v, want nil", err)
	}
	for _, s := range result.Stages {
		if s.Name != StageMCPTools {
			continue
		}
		for _, want := range []string{"registered in this build", "ESHU_MCP_TOOL_SURFACE"} {
			if !strings.Contains(s.Detail, want) {
				t.Fatalf("mcp tools detail = %q, want it to contain %q", s.Detail, want)
			}
		}
		return
	}
	t.Fatal("no mcp tools stage in the setup result")
}
