// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package trace

import "testing"

// TestMapValue covers the three outcomes: a nested object, a key holding some
// other type, and a nil parent. The nil-parent case matters because the
// renderers chain these readers, so an absent block must read as empty rather
// than panic.
func TestMapValue(t *testing.T) {
	t.Parallel()

	nested := map[string]any{"state": "fresh"}
	parent := map[string]any{"freshness": nested, "wrong": "string"}

	if got := mapValue(parent, "freshness"); got["state"] != "fresh" {
		t.Fatalf("mapValue(freshness) = %v, want the nested object", got)
	}
	if got := mapValue(parent, "wrong"); got != nil {
		t.Fatalf("mapValue over a string = %v, want nil", got)
	}
	if got := mapValue(parent, "absent"); got != nil {
		t.Fatalf("mapValue(absent) = %v, want nil", got)
	}
	if got := mapValue(nil, "freshness"); got != nil {
		t.Fatalf("mapValue over nil = %v, want nil", got)
	}
}

// TestSliceValue pins the []map[string]any arm as well as the []any arm. Both
// exist: a decoded API response yields []any, while a test or caller building an
// envelope in Go writes []map[string]any, and the renderers must count either.
func TestSliceValue(t *testing.T) {
	t.Parallel()

	parent := map[string]any{
		"any":   []any{1, 2, 3},
		"typed": []map[string]any{{"a": 1}, {"b": 2}},
		"wrong": "string",
	}

	if got := sliceValue(parent, "any"); len(got) != 3 {
		t.Fatalf("sliceValue([]any) length = %d, want 3", len(got))
	}
	typed := sliceValue(parent, "typed")
	if len(typed) != 2 {
		t.Fatalf("sliceValue([]map[string]any) length = %d, want 2", len(typed))
	}
	if _, ok := typed[0].(map[string]any); !ok {
		t.Fatalf("sliceValue converted element type = %T, want map[string]any", typed[0])
	}
	if got := sliceValue(parent, "wrong"); got != nil {
		t.Fatalf("sliceValue over a string = %v, want nil", got)
	}
	if got := sliceValue(nil, "any"); got != nil {
		t.Fatalf("sliceValue over nil = %v, want nil", got)
	}
}

// TestStringValue pins the trim, which the renderers rely on to treat a
// whitespace-only field as absent rather than printing a blank line.
func TestStringValue(t *testing.T) {
	t.Parallel()

	parent := map[string]any{"name": "  api  ", "blank": "   ", "wrong": 7}

	if got := stringValue(parent, "name"); got != "api" {
		t.Fatalf("stringValue = %q, want %q", got, "api")
	}
	if got := stringValue(parent, "blank"); got != "" {
		t.Fatalf("stringValue over whitespace = %q, want empty", got)
	}
	if got := stringValue(parent, "wrong"); got != "" {
		t.Fatalf("stringValue over an int = %q, want empty", got)
	}
	if got := stringValue(nil, "name"); got != "" {
		t.Fatalf("stringValue over nil = %q, want empty", got)
	}
}

// TestIntValue pins the float64 arm, which is the one that matters in
// production: encoding/json decodes every JSON number into float64, so an
// int-only reader would report every evidence count as zero.
func TestIntValue(t *testing.T) {
	t.Parallel()

	parent := map[string]any{"json": float64(4), "native": 7, "wrong": "9"}

	if got := intValue(parent, "json"); got != 4 {
		t.Fatalf("intValue(float64) = %d, want 4", got)
	}
	if got := intValue(parent, "native"); got != 7 {
		t.Fatalf("intValue(int) = %d, want 7", got)
	}
	if got := intValue(parent, "wrong"); got != 0 {
		t.Fatalf("intValue over a string = %d, want 0", got)
	}
	if got := intValue(nil, "json"); got != 0 {
		t.Fatalf("intValue over nil = %d, want 0", got)
	}
}

// TestStringsValue pins the filtering: a non-string entry and a blank string are
// both dropped, so the "What to worry about" list never renders an empty bullet.
func TestStringsValue(t *testing.T) {
	t.Parallel()

	got := stringsValue([]any{"  first  ", 42, "", "   ", "second"})
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("stringsValue = %#v, want [first second]", got)
	}
	if got := stringsValue([]string{"kept"}); len(got) != 1 || got[0] != "kept" {
		t.Fatalf("stringsValue([]string) = %#v, want [kept]", got)
	}
	if got := stringsValue("not a list"); got != nil {
		t.Fatalf("stringsValue over a string = %#v, want nil", got)
	}
	if got := stringsValue(nil); got != nil {
		t.Fatalf("stringsValue(nil) = %#v, want nil", got)
	}
}

// TestFirstString pins the fallback order the identity fields depend on, and
// that an all-blank call reports empty rather than a whitespace string.
func TestFirstString(t *testing.T) {
	t.Parallel()

	if got := firstString("", "   ", "  chosen  ", "later"); got != "chosen" {
		t.Fatalf("firstString = %q, want %q", got, "chosen")
	}
	if got := firstString("", "   "); got != "" {
		t.Fatalf("firstString over blanks = %q, want empty", got)
	}
	if got := firstString(); got != "" {
		t.Fatalf("firstString() = %q, want empty", got)
	}
}
