// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"reflect"
	"testing"
)

// TestCallLanguage proves explicit lang wins with extension fallback per
// language and empty for unknown shapes.
func TestCallLanguage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		call     map[string]any
		raw      string
		relative string
		want     string
	}{
		{"explicit", map[string]any{"lang": "python"}, "/repo/a.go", "a.go", "python"},
		{"go_ext", map[string]any{}, "/repo/a.go", "a.go", "go"},
		{"php_ext", map[string]any{}, "/repo/a.php", "a.php", "php"},
		{"swift_ext", map[string]any{}, "/repo/A.swift", "A.swift", "swift"},
		{"ruby_ext", map[string]any{}, "/repo/a.rb", "a.rb", "ruby"},
		{"elixir_ext", map[string]any{}, "/repo/a.exs", "a.exs", "elixir"},
		{"unknown_ext", map[string]any{}, "/repo/a.js", "a.js", ""},
		{"no_path", map[string]any{}, "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CallLanguage(tc.call, tc.raw, tc.relative); got != tc.want {
				t.Fatalf("CallLanguage = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestHasQualifiedScope proves each qualification signal trips the guard
// while bare calls stay broad.
func TestHasQualifiedScope(t *testing.T) {
	t.Parallel()

	if !HasQualifiedScope(map[string]any{"full_name": "server.serve"}, "go") {
		t.Fatal("dotted full_name should be qualified")
	}
	if !HasQualifiedScope(map[string]any{"class_context": "Worker"}, "go") {
		t.Fatal("class context should be qualified")
	}
	if !HasQualifiedScope(map[string]any{"inferred_obj_type": "Worker"}, "go") {
		t.Fatal("inferred receiver should be qualified")
	}
	rubyClass := map[string]any{"context": "Worker", "context_type": "class"}
	if !HasQualifiedScope(rubyClass, "ruby") {
		t.Fatal("ruby class context should be qualified")
	}
	if HasQualifiedScope(rubyClass, "go") {
		t.Fatal("non-ruby context must not qualify")
	}
	if HasQualifiedScope(map[string]any{"name": "serve"}, "go") {
		t.Fatal("bare call must not be qualified")
	}
	tuple := map[string]any{"context": []any{"Worker", "module"}}
	if !HasQualifiedScope(tuple, "ruby") {
		t.Fatal("ruby module tuple should be qualified")
	}
}

// TestPathKeys proves raw, relative, and base keys dedup with blanks
// contributing nothing.
func TestPathKeys(t *testing.T) {
	t.Parallel()

	got := PathKeys("/repo/src/a.js", "src/a.js")
	want := []string{"/repo/src/a.js", "src/a.js", "a.js"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PathKeys = %#v, want %#v", got, want)
	}
	if got := PathKeys("", ""); len(got) != 0 {
		t.Fatalf("blank PathKeys = %#v, want empty", got)
	}
	if got := PathKeys("/repo/a.js", "/repo/a.js"); !reflect.DeepEqual(got, []string{"/repo/a.js", "a.js"}) {
		t.Fatalf("identical PathKeys = %#v, want deduped", got)
	}
}

// TestFileKeys proves full/relative identity with the equal-key collapse
// and blank handling.
func TestFileKeys(t *testing.T) {
	t.Parallel()

	full, relative := FileKeys("/repo/src/a.js", "src/a.js")
	if full != "/repo/src/a.js" || relative != "src/a.js" {
		t.Fatalf("FileKeys = (%q, %q), want full and relative", full, relative)
	}
	full, relative = FileKeys("/repo/a.js", "/repo/a.js")
	if full != "/repo/a.js" || relative != "" {
		t.Fatalf("equal FileKeys = (%q, %q), want collapsed relative", full, relative)
	}
	full, relative = FileKeys("", "src/a.js")
	if full != "src/a.js" || relative != "" {
		t.Fatalf("blank-raw FileKeys = (%q, %q), want relative in the full slot", full, relative)
	}
	if full, relative := FileKeys("", ""); full != "" || relative != "" {
		t.Fatalf("blank FileKeys = (%q, %q), want empty", full, relative)
	}
}

// TestDirectoryKey proves directory extraction with root and blank edges.
func TestDirectoryKey(t *testing.T) {
	t.Parallel()

	if got := DirectoryKey("/repo/src/a.js"); got != "/repo/src" {
		t.Fatalf("DirectoryKey = %q, want /repo/src", got)
	}
	if got := DirectoryKey("go.mod"); got != "." {
		t.Fatalf("bare DirectoryKey = %q, want .", got)
	}
	for _, path := range []string{"", ".", "   "} {
		if got := DirectoryKey(path); got != "" {
			t.Fatalf("DirectoryKey(%q) = %q, want empty", path, got)
		}
	}
}

// TestPreferredPathAndNormalize proves relative-first preference with
// cleaning semantics.
func TestPreferredPathAndNormalize(t *testing.T) {
	t.Parallel()

	if got := PreferredPath("/repo/src/a.js", "src/a.js"); got != "src/a.js" {
		t.Fatalf("PreferredPath = %q, want src/a.js", got)
	}
	if got := PreferredPath("/repo/src/a.js", ""); got != "/repo/src/a.js" {
		t.Fatalf("PreferredPath without relative = %q, want the raw path", got)
	}
	if got := NormalizePath("  /repo//src/a.js  "); got != "/repo/src/a.js" {
		t.Fatalf("NormalizePath = %q, want cleaned", got)
	}
	if got := NormalizePath("   "); got != "" {
		t.Fatalf("blank NormalizePath = %q, want empty", got)
	}
}

// TestPayloadInt proves first-numeric wins across int shapes with 0 as
// the no-match sentinel.
func TestPayloadInt(t *testing.T) {
	t.Parallel()

	if got := PayloadInt("x", int32(3), 7); got != 3 {
		t.Fatalf("PayloadInt = %d, want 3", got)
	}
	if got := PayloadInt(7, 8); got != 7 {
		t.Fatalf("PayloadInt = %d, want 7", got)
	}
	if got := PayloadInt(int64(9), float32(2.5), float64(4.0)); got != 9 {
		t.Fatalf("PayloadInt = %d, want 9", got)
	}
	if got := PayloadInt("x", nil, true); got != 0 {
		t.Fatalf("PayloadInt without numerics = %d, want 0", got)
	}
	if got := PayloadInt(); got != 0 {
		t.Fatalf("PayloadInt() = %d, want 0", got)
	}
}

// TestCodeCallTrailingSegments proves trailing-segment extraction with
// the count floor.
func TestCodeCallTrailingSegments(t *testing.T) {
	t.Parallel()

	if got := codeCallTrailingSegments("a.b.c", 2); got != "b.c" {
		t.Fatalf("trailing 2 = %q, want b.c", got)
	}
	if got := codeCallTrailingSegments("a#b#c", 1); got != "c" {
		t.Fatalf("hash trailing = %q, want c", got)
	}
	if got := codeCallTrailingSegments("abc", 2); got != "" {
		t.Fatalf("short value = %q, want empty", got)
	}
	if got := codeCallTrailingSegments("a.b", 0); got != "" {
		t.Fatalf("zero count = %q, want empty", got)
	}
	if got := codeCallTrailingSegments("   ", 2); got != "" {
		t.Fatalf("blank value = %q, want empty", got)
	}
	if got := codeCallPathLineKey("/repo/a.js", 12); got != "/repo/a.js#12" {
		t.Fatalf("path line key = %q, want /repo/a.js#12", got)
	}
}

// TestUniqueCodeCallNamesByDirectory proves only single-candidate names
// survive the directory collapse.
func TestUniqueCodeCallNamesByDirectory(t *testing.T) {
	t.Parallel()

	dirs := map[string]map[string]map[string]struct{}{
		"src": {
			"serve": {"e1": {}},
			"run":   {"e1": {}, "e2": {}},
		},
	}
	got := uniqueCodeCallNamesByDirectory(dirs)
	if got["src"]["serve"] != "e1" {
		t.Fatalf("unique names = %#v, want serve -> e1", got)
	}
	if _, ok := got["src"]["run"]; ok {
		t.Fatalf("ambiguous run survived = %#v, want absent", got)
	}
}
