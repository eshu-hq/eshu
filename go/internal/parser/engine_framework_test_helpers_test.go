// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import "testing"

func assertNoFrameworkOrNoRoutes(t *testing.T, payload map[string]any, section string) {
	t.Helper()

	semantics := frameworkSemanticsMap(t, payload)
	nested, ok := semantics[section].(map[string]any)
	if !ok {
		// Framework not present at all — acceptable
		return
	}
	entries, _ := nested["route_entries"].([]map[string]string)
	if len(entries) > 0 {
		t.Fatalf("framework_semantics.%s.route_entries = %#v, want empty or absent", section, entries)
	}
}
