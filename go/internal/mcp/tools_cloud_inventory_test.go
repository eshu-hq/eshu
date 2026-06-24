// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

func TestCloudInventoryToolAdvertisesSanitizedFreshnessEvidence(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_cloud_resource_inventory")
	for _, want := range []string{
		"optional sanitized freshness evidence",
		"Unsupported on lightweight local runtime",
	} {
		if !strings.Contains(tool.Description, want) {
			t.Fatalf("tool description missing %q: %q", want, tool.Description)
		}
	}
}
