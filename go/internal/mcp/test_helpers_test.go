// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func assertMCPToolCount(t *testing.T, tools []any, want int) {
	t.Helper()
	if len(tools) != want {
		t.Errorf("expected %d tools, got %d", want, len(tools))
	}
}
