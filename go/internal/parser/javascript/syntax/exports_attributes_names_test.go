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

// TestReExportAttributeEntriesSkipNamesTheReducerWouldTrim: the reducer trims
// names, so a recovered specifier whose name has surrounding whitespace is
// skipped rather than recorded and resolved as a different symbol (#7461).
func TestReExportAttributeEntriesSkipNamesTheReducerWouldTrim(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "export { ' x ' as y, kept } from './x.json' with { type: 'json' };\n")
	defer closeFn()
	got := ReExportAttributeEntries(firstNodeOfKind(t, root, "ERROR"), source, "javascript")
	kept := false
	for _, item := range got {
		original, _ := item["original_name"].(string)
		name, _ := item["name"].(string)
		if original == " x " || name == " x " || name == "y" {
			t.Fatalf("recovered the whitespace-bearing specifier: %#v", item)
		}
		if name == "kept" {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("ReExportAttributeEntries dropped the plain sibling specifier: %#v", got)
	}
}

// TestReExportAttributeEntriesSkipAnEmptyOriginalName: an empty original name
// would be read by the reducer as "same as the exported name" and resolve
// export { ” as c } to the module's c, so the specifier is skipped (#7461).
func TestReExportAttributeEntriesSkipAnEmptyOriginalName(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "export { '' as c, kept } from './x.json' with { type: 'json' };\n")
	defer closeFn()
	got := ReExportAttributeEntries(firstNodeOfKind(t, root, "ERROR"), source, "javascript")
	kept := false
	for _, item := range got {
		if item["name"] == "c" {
			t.Fatalf("recorded the empty-original specifier: %#v", item)
		}
		if item["name"] == "kept" {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("dropped the plain sibling specifier: %#v", got)
	}
}
