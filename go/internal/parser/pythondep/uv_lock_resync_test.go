// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"strings"
	"testing"
)

// TestParseUVLockUnterminatedOuterArrayIsNeverComplete pins the resync rule
// for a multi-line value opened outside any [[package]] entry (a root key, the
// [manifest] members, or a sub-table array). The swallowed lines are invalid
// TOML and may hide package data, so the state must not read complete, and a
// package header in any classifiable form (a trailing comment included) must
// end the swallow so the entries after it are still read.
func TestParseUVLockUnterminatedOuterArrayIsNeverComplete(t *testing.T) {
	t.Parallel()

	evil := "\n[[package]] # c\nname = \"evil\"\nversion = \"1.0\"\nsource = { path = \"../x\" }\n"
	ok := "\n[[package]]\nname = \"ok\"\nversion = \"1.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n"
	cases := []struct {
		name string
		src  string
		rows []string
	}{
		{"members swallow ends at a commented header", "version = 1\n[manifest]\nmembers = [\n    \"a\",\n" + evil + ok, []string{"evil", "ok"}},
		{"root array swallow ends at a commented header", "version = 1\nresolution-markers = [\n  \"x\",\n" + evil + ok, []string{"evil", "ok"}},
		{"root array open at end of file", "version = 1\nresolution-markers = [\n  \"x\",\n", nil},
		{"swallowed package table at end of file", "version = 1\nresolution-markers = [\n  \"x\",\n[package]\nname = \"a\"\nversion = \"1.0\"\n", nil},
		{"swallowed package table before an entry", "version = 1\nresolution-markers = [\n  \"x\",\n[package]\nname = \"a\"\n" + ok, []string{"ok"}},
		{"swallowed package table closed later", "version = 1\nresolution-markers = [\n  \"x\",\n[package]\nname = \"a\"\nversion = \"1.0\"\nsource = { path = \"../x\" }\n]\n", nil},
		{"swallowed spaced array header closed later", "version = 1\nresolution-markers = [\n  \"x\",\n[ [package]]\nname = \"a\"\n]\n", nil},
		{"swallowed over-closed header", "version = 1\nresolution-markers = [\n  \"x\",\n[[package]]]\nname = \"evil\"\nversion = \"1.0\"\nsource = { path = \"../x\" }\n" + ok, []string{"ok"}},
		{"sub-table array open at end of file", "version = 1\n" + ok + "\n[package.optional-dependencies]\nextra = [\n    { name = \"more\" },\n", []string{"ok"}},
		{"swallow ends at a header with 15 inner spaces", "version = 1\nresolution-markers = [\n  \"x\",\n[[" + strings.Repeat(" ", 15) + "package]]\nname = \"evil\"\nversion = \"1.0\"\nsource = { path = \"../x\" }\n" + ok, []string{"evil", "ok"}},
		{"swallow ends at a header with 300 inner spaces", "version = 1\nresolution-markers = [\n  \"x\",\n[[" + strings.Repeat(" ", 300) + "package]]\nname = \"evil\"\nversion = \"1.0\"\nsource = { path = \"../x\" }\n" + ok, []string{"evil", "ok"}},
		{"swallow ends at a header with 15 inner tabs", "version = 1\nresolution-markers = [\n  \"x\",\n[[" + strings.Repeat("\t", 15) + "package]]\nname = \"evil\"\nversion = \"1.0\"\nsource = { path = \"../x\" }\n" + ok, []string{"evil", "ok"}},
		{"sub-table array swallow ends at a header", "version = 1\n" + ok + "\n[package.optional-dependencies]\nextra = [\n    { name = \"more\" },\n" + evil, []string{"ok", "evil"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := parseUVLockBytes([]byte(tc.src))
			var got []string
			for _, row := range result.Rows {
				got = append(got, row["name"].(string))
			}
			if strings.Join(got, ",") != strings.Join(tc.rows, ",") {
				t.Fatalf("rows = %v, want %v (state %+v)\n%s", got, tc.rows, result.State, tc.src)
			}
			s := result.State
			if s.State() == "complete" {
				t.Fatalf("state = %+v reads complete although a value was never closed:\n%s", s, tc.src)
			}
			if sum := s.Resolved + s.Unresolved + s.Overflow + s.RootPackages + s.WorkspaceMembers; sum != s.Entries {
				t.Fatalf("accounted %d != entries %d in %+v", sum, s.Entries, s)
			}
			uvAssertResolvedRowsAreWrittenVerbatim(t, tc.src, result)
		})
	}
}

// TestParseUVLockHeaderTrailingCommentStillReadsTheEntry proves a commented
// [[package]] header outside a swallow starts an ordinary entry.
func TestParseUVLockHeaderTrailingCommentStillReadsTheEntry(t *testing.T) {
	t.Parallel()

	src := "version = 1\n\n[[package]]   # pinned\nname = \"idna\"\nversion = \"3.7\"\nsource = { registry = \"https://pypi.org/simple\" }\n"
	result := parseUVLockBytes([]byte(src))
	if len(result.Rows) != 1 || result.Rows[0]["config_kind"] != "dependency" || result.State.State() != "complete" {
		t.Fatalf("rows = %#v state = %+v, want one complete dependency row", result.Rows, result.State)
	}
	uvAssertResolvedRowsAreWrittenVerbatim(t, src, result)
}
