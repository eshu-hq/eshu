// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeinteltools

import "testing"

// TestAdvertisedLimitDefaultMatchesRouteDefault pins each row-returning tool's
// advertised schema `limit.default` to the fallback its route selection sends,
// so a client reading the schema and a call that omits limit agree. The two
// source_cache-bearing tools default to 20 rows (#7171) so a default-args page
// of clipped rows stays inside the dispatch response budget.
func TestAdvertisedLimitDefaultMatchesRouteDefault(t *testing.T) {
	t.Parallel()

	for tool, want := range map[string]int{"find_symbol": 20, "inspect_code_inventory": 20} {
		var schemaDefault any
		found := false
		for _, def := range Tools() {
			if def.Name != tool {
				continue
			}
			found = true
			props, _ := def.InputSchema.(map[string]any)["properties"].(map[string]any)
			limit, _ := props["limit"].(map[string]any)
			schemaDefault = limit["default"]
		}
		if !found {
			t.Fatalf("tool %s not registered", tool)
		}
		if schemaDefault != want {
			t.Errorf("%s schema limit.default = %#v, want %d", tool, schemaDefault, want)
		}
		route, ok := Route(tool, nil)
		if !ok {
			t.Fatalf("Route(%s) not handled", tool)
		}
		if got := route.Body.(map[string]any)["limit"]; got != want {
			t.Errorf("%s route default limit = %#v, want %d", tool, got, want)
		}
	}
}
