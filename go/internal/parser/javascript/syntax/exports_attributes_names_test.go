// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import "testing"

// TestReExportAttributeEntriesReadStringLiteralNamesByValue pins the recovered
// attribute re-export rows for string-literal specifier names (#7461): the
// quotes are dropped, and an alias written as an empty string is still an alias
// rather than "no alias".
func TestReExportAttributeEntriesReadStringLiteralNamesByValue(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		body         string
		wantName     string
		wantOriginal string
	}{
		{name: "spaced original and alias", body: "export { 'a b' as \"c d\" } from './x.json' with { type: 'json' };\n", wantName: "c d", wantOriginal: "a b"},
		{name: "empty alias is an alias", body: "export { value as '' } from './x.json' with { type: 'json' };\n", wantName: "", wantOriginal: "value"},
		{name: "plain names unchanged", body: "export { a as b } from './x.json' with { type: 'json' };\n", wantName: "b", wantOriginal: "a"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, source, closeFn := parseRootForTest(t, tc.body)
			defer closeFn()
			got := ReExportAttributeEntries(firstNodeOfKind(t, root, "ERROR"), source, "javascript")
			for _, item := range got {
				name, _ := item["name"].(string)
				original, _ := item["original_name"].(string)
				if name == tc.wantName && original == tc.wantOriginal {
					return
				}
			}
			t.Fatalf("ReExportAttributeEntries(%q) = %#v, want a row named %q from %q", tc.body, got, tc.wantName, tc.wantOriginal)
		})
	}
}
