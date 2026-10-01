// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import "testing"

func TestStringLiteralValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		literal string
		want    string
	}{
		{name: "single quotes", literal: `'x y'`, want: "x y"},
		{name: "double quotes", literal: `"x y"`, want: "x y"},
		{name: "same value either quote", literal: `"from"`, want: "from"},
		{name: "escaped quote", literal: `'a\'b'`, want: "a'b"},
		{name: "escaped backslash", literal: `"a\\b"`, want: `a\b`},
		{name: "unicode escape", literal: `'\u0066rom'`, want: "from"},
		{name: "unicode escape non-ascii", literal: `'caf\u00e9'`, want: "café"},
		{name: "unicode escape uppercase hex", literal: `'\u00E9'`, want: "é"},
		{name: "braced unicode escape", literal: `'\u{66}rom'`, want: "from"},
		{name: "braced astral code point", literal: `'\u{1F600}'`, want: "😀"},
		{name: "surrogate pair escapes", literal: `'\ud83d\ude00'`, want: "😀"},
		{name: "surrogate pair uppercase", literal: `'\uD83D\uDE00'`, want: "😀"},
		{name: "lone high surrogate falls back to body", literal: `'\ud83d'`, want: `\ud83d`},
		{name: "lone low surrogate falls back to body", literal: `'\ude00'`, want: `\ude00`},
		{name: "low surrogate then low surrogate falls back to body", literal: `'\ude00\udc00'`, want: `\ude00\udc00`},
		{name: "high surrogate then non-low falls back to body", literal: `'\ud83d\u0041'`, want: `\ud83d\u0041`},
		{name: "high surrogate then literal falls back to body", literal: `'\ud83dx'`, want: `\ud83dx`},
		{name: "hex escape", literal: `'\x41'`, want: "A"},
		{name: "nul escape", literal: `'a\0b'`, want: "a\x00b"},
		{name: "newline escape", literal: `'a\nb'`, want: "a\nb"},
		{name: "line continuation", literal: "'a\\\nb'", want: "ab"},
		{name: "line separator continuation", literal: "'a\\\u2028b'", want: "ab"},
		{name: "multibyte escaped char", literal: `'\é'`, want: "é"},
		{name: "truncated unicode escape falls back to body", literal: `'\u12'`, want: `\u12`},
		{name: "legacy octal escape falls back to body", literal: `'\1'`, want: `\1`},
		{name: "unterminated braced escape falls back to body", literal: `'\u{66'`, want: `\u{66`},
		{name: "out of range code point falls back to body", literal: `'\u{110000}'`, want: `\u{110000}`},
		{name: "empty string", literal: `''`, want: ""},
		{name: "unquoted text is unchanged", literal: `plain`, want: "plain"},
		{name: "mismatched quotes are unchanged", literal: `'x"`, want: `'x"`},
		{name: "lone quote is unchanged", literal: `'`, want: `'`},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := stringLiteralValue(tt.literal); got != tt.want {
				t.Fatalf("stringLiteralValue(%q) = %q, want %q", tt.literal, got, tt.want)
			}
		})
	}
}

func TestUnquoteModuleExportName(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"  plain  ": "plain",
		` 'b c' `:   "b c",
		` "b c" `:   "b c",
		`'type'`:    "type",
		"":          "",
	}
	for input, want := range tests {
		if got := unquoteModuleExportName(input); got != want {
			t.Fatalf("unquoteModuleExportName(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestReExportSpecifierNamesUnquotesTextFallbackNames pins the brace-text
// fallback: when the grammar yields no export_specifier the specifier text is
// split by hand, and a quoted name must read as its value there too (#7461).
func TestReExportSpecifierNamesUnquotesTextFallbackNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw          string
		wantOriginal string
		wantExported string
	}{
		{raw: ` 'from' `, wantOriginal: "from", wantExported: "from"},
		{raw: ` "from" `, wantOriginal: "from", wantExported: "from"},
		{raw: ` a as 'b' `, wantOriginal: "a", wantExported: "b"},
		{raw: ` 'a' as "b" `, wantOriginal: "a", wantExported: "b"},
		{raw: ` type 'a' as b `, wantOriginal: "a", wantExported: "b"},
		{raw: ` '\u0066rom' as b `, wantOriginal: "from", wantExported: "b"},
		{raw: ` 'x y' as 'z w' `, wantOriginal: "x y", wantExported: "z w"},
		{raw: ` plain as bare `, wantOriginal: "plain", wantExported: "bare"},
	}
	for _, tt := range tests {
		original, exported := reExportSpecifierNames(tt.raw)
		if original != tt.wantOriginal || exported != tt.wantExported {
			t.Fatalf("reExportSpecifierNames(%q) = (%q, %q), want (%q, %q)",
				tt.raw, original, exported, tt.wantOriginal, tt.wantExported)
		}
	}
}

// TestReExportSpecifiersFromTextUnquotesNames drives the brace-text fallback
// through a parsed export statement, so the quoted names it reads reach
// ReExportSpecifier the way the dead-code walk consumes them (#7461).
func TestReExportSpecifiersFromTextUnquotesNames(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "export { 'a' as 'b c', \"d\" as e } from \"./m\";\n")
	defer closeFn()
	got := reExportSpecifiersFromText(firstExportStatement(t, root), source)
	if len(got) != 2 ||
		got[0].OriginalName != "a" || got[0].ExportedName != "b c" ||
		got[1].OriginalName != "d" || got[1].ExportedName != "e" {
		t.Fatalf("reExportSpecifiersFromText() = %#v, want a as b c and d as e", got)
	}
}
