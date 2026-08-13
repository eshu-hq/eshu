// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// fakeBearerToken is a recognizable bearer value shared by cmd/eshu's MCP
// setup wrapper tests (mcp_setup_cmd_test.go and assistant_guidance_test.go)
// to prove no raw secret survives into rendered output. It intentionally
// duplicates the same-named literal in internal/cli/mcpsetup's own tests
// rather than exporting a test-only fixture across the package boundary.
const fakeBearerToken = "eshu_live_SUPERSECRET_abc123def456ghi789"

// extractJSON returns the first balanced JSON object in a rendered snippet
// block, starting at the first '{' and ending at its matching '}'. It is
// brace-balance aware so trailing note text containing '}' does not confuse
// it. Duplicated from internal/cli/mcpsetup's own test helper for the same
// reason as fakeBearerToken above.
func extractJSON(t *testing.T, s string) string {
	t.Helper()
	start := strings.Index(s, "{")
	if start < 0 {
		t.Fatalf("no JSON object found in:\n%s", s)
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	t.Fatalf("unbalanced JSON object in:\n%s", s)
	return ""
}
