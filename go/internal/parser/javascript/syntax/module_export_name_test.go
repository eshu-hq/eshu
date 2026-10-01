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
		{name: "unicode escape", literal: `'from'`, want: "from"},
		{name: "braced unicode escape", literal: `'\u{66}rom'`, want: "from"},
		{name: "hex escape", literal: `'\x41'`, want: "A"},
		{name: "nul escape", literal: `'a\0b'`, want: "a\x00b"},
		{name: "newline escape", literal: `'a\nb'`, want: "a\nb"},
		{name: "line continuation", literal: "'a\\\nb'", want: "ab"},
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
