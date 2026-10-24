// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import "testing"

// A mistyped version reference is a Gradle type error, the same as a mistyped
// plain version (`version = 123`): the entry yields no row and is counted as
// unsupported, never as an unresolved rich version.
func TestScanMistypedVersionRefIsUnsupportedLikeAMistypedVersion(t *testing.T) {
	t.Parallel()
	entries := map[string]string{
		"dotted ref integer":    `x = { module = "g:x", version.ref = 123 }`,
		"dotted ref bool":       `x = { module = "g:x", version.ref = true }`,
		"dotted ref array":      `x = { module = "g:x", version.ref = [1] }`,
		"table ref integer":     `x = { module = "g:x", version = { ref = 123 } }`,
		"table ref bool":        `x = { module = "g:x", version = { ref = true } }`,
		"table ref array":       `x = { module = "g:x", version = { ref = [1] } }`,
		"group name dotted ref": `x = { group = "g", name = "x", version.ref = 123 }`,
		"mistyped version":      `x = { module = "g:x", version = 123 }`,
	}
	for name, entry := range entries {
		result := Scan([]byte("[libraries]\n" + entry + "\n"))
		if result.State != StateParsed || len(result.Rows) != 0 || result.Counts.Unsupported != 1 || result.Counts.Unresolved != 0 {
			t.Errorf("%s: State=%q rows=%v Counts=%+v, want parsed, no rows, Unsupported=1", name, result.State, result.Rows, result.Counts)
		}
	}
	plugin := Scan([]byte("[plugins]\nx = { id = \"p.x\", version.ref = 123 }\n"))
	if len(plugin.Rows) != 0 || plugin.Counts.Unsupported != 1 {
		t.Errorf("plugin: rows=%v Counts=%+v, want no rows, Unsupported=1", plugin.Rows, plugin.Counts)
	}
}

// A legal string reference keeps its meaning: it resolves against [versions].
func TestScanStringVersionRefStillResolves(t *testing.T) {
	t.Parallel()
	for name, entry := range map[string]string{
		"dotted": `x = { module = "g:x", version.ref = "v" }`,
		"table":  `x = { module = "g:x", version = { ref = "v" } }`,
	} {
		result := Scan([]byte("[versions]\nv = \"1.2\"\n[libraries]\n" + entry + "\n"))
		if len(result.Rows) != 1 || result.Rows[0]["value"] != "1.2" || result.Counts.Unsupported != 0 {
			t.Errorf("%s: rows=%v Counts=%+v, want one resolved row 1.2", name, result.Rows, result.Counts)
		}
	}
}
