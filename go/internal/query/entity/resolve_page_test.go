// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import "testing"

// TestResolvedEntityResponseDoesNotEmitMatchesAlias pins #7173: every resolve
// producer (graph, canonical content handle, global content name, workload)
// builds its body through resolvedEntityResponse, and `matches` was a
// byte-identical copy of `entities`. The canonical field stays, the alias is
// gone, and the paging fields are unchanged.
func TestResolvedEntityResponseDoesNotEmitMatchesAlias(t *testing.T) {
	t.Parallel()

	entities := []map[string]any{{"id": "e1", "name": "handler"}}
	got := resolvedEntityResponse(entities, 10, true)

	if _, present := got["matches"]; present {
		t.Fatalf("resolvedEntityResponse emits the removed matches alias: %#v", got["matches"])
	}
	if rows, ok := got["entities"].([]map[string]any); !ok || len(rows) != 1 {
		t.Fatalf("entities = %#v, want the one resolved row", got["entities"])
	}
	if got["count"] != 1 || got["limit"] != 10 || got["truncated"] != true {
		t.Fatalf("paging fields = count:%v limit:%v truncated:%v, want 1/10/true", got["count"], got["limit"], got["truncated"])
	}
}
