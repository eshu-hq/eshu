// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcpsetup

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp"
)

// TestVerificationToolCountNamesItsScope proves the tools-visible detail says
// the count comes from this build's registered tools, and that a deployment can
// advertise fewer, so a router-surface user is not misled by the number.
func TestVerificationToolCountNamesItsScope(t *testing.T) {
	t.Parallel()
	report := RunVerification("snippet", mcp.ReadOnlyTools, nil, nil, "")
	detail := stageMap(report)[StageToolsVisible].Detail
	for _, want := range []string{"registered in this build", "ESHU_MCP_TOOL_SURFACE"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("tools visible detail = %q, want it to contain %q", detail, want)
		}
	}
}
