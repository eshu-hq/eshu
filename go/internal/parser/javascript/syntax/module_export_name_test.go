// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"reflect"
	"testing"
)

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
		{name: "braced surrogate pair", literal: `'\u{D83D}\u{DE00}'`, want: "😀"},
		{name: "braced high then four-digit low", literal: `'\u{d83d}\ude00'`, want: "😀"},
		{name: "four-digit high then braced low", literal: `'\ud83d\u{de00}'`, want: "😀"},
		{name: "braced high then non-low falls back to body", literal: `'\u{D83D}\u{41}'`, want: `\u{D83D}\u{41}`},
		{name: "surrogate pair escapes", literal: `'\ud83d\ude00'`, want: "😀"},
		{name: "surrogate pair uppercase", literal: `'\uD83D\uDE00'`, want: "😀"},
		{name: "lone high surrogate falls back to body", literal: `'\ud83d'`, want: `\ud83d`},
		{name: "lone low surrogate falls back to body", literal: `'\ude00'`, want: `\ude00`},
		{name: "low surrogate then low surrogate falls back to body", literal: `'\ude00\udc00'`, want: `\ude00\udc00`},
		{name: "high surrogate then non-low falls back to body", literal: `'\ud83d\u0041'`, want: `\ud83d\u0041`},
		{name: "high surrogate then literal falls back to body", literal: `'\ud83dx'`, want: `\ud83dx`},
		{name: "high surrogate then two characters and low-surrogate digits falls back to body", literal: `'\ud83dxxdc00'`, want: `\ud83dxxdc00`},
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

func TestUnquoteModuleSpecifierName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input  string
		want   string
		wantOK bool
	}{
		{input: "  plain  ", want: "plain", wantOK: true},
		{input: ` 'b c' `, want: "b c", wantOK: true},
		{input: ` "b c" `, want: "b c", wantOK: true},
		{input: `'type'`, want: "type", wantOK: true},
		{input: "", want: "", wantOK: true},
		{input: `' padded '`, want: " padded ", wantOK: false},
		{input: `'a\0b'`, want: "a\x00b", wantOK: false},
		{input: `'\ud800'`, want: `\ud800`, wantOK: false},
		{input: `'\1'`, want: `\1`, wantOK: false},
	}
	for _, tt := range tests {
		got, ok := unquoteModuleSpecifierName(tt.input)
		if got != tt.want || ok != tt.wantOK {
			t.Fatalf("unquoteModuleSpecifierName(%q) = (%q, %v), want (%q, %v)", tt.input, got, ok, tt.want, tt.wantOK)
		}
	}
}

// TestDecodeStringLiteralReportsUndecodableEscapes pins the validity flag: a
// literal whose escape cannot be decoded yields its raw body, which is also the
// value of a different, valid literal (`'\ud800'` against `'\\ud800'`), so the
// caller must be told to skip the specifier instead of resolving that name.
func TestDecodeStringLiteralReportsUndecodableEscapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		literal string
		want    string
		wantOK  bool
	}{
		{literal: `'plain'`, want: "plain", wantOK: true},
		{literal: `'\u0066'`, want: "f", wantOK: true},
		{literal: `'\ud800'`, want: `\ud800`, wantOK: false},
		{literal: `'\\ud800'`, want: `\ud800`, wantOK: true},
		{literal: `'\1'`, want: `\1`, wantOK: false},
		{literal: `'\u12'`, want: `\u12`, wantOK: false},
		{literal: `plain`, want: "plain", wantOK: true},
	}
	for _, tt := range tests {
		got, ok := decodeStringLiteral(tt.literal)
		if got != tt.want || ok != tt.wantOK {
			t.Fatalf("decodeStringLiteral(%q) = (%q, %v), want (%q, %v)", tt.literal, got, ok, tt.want, tt.wantOK)
		}
	}
}

// TestExportSpecifierWithoutLineCommentsKeepsQuotedMarkers pins the quote-aware
// comment strip: a comment marker inside a quoted name is part of the name.
func TestExportSpecifierWithoutLineCommentsKeepsQuotedMarkers(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"a // note\n as b":               `a   as b`,
		"a /* note */ as b":              `a   as b`,
		`'a//b' as c`:                    `'a//b' as c`,
		`'a/*x*/b' as c`:                 `'a/*x*/b' as c`,
		`"it's // not" as c // trailing`: `"it's // not" as c`,
		"a\nas\r\nb":                     `a as  b`,
	}
	for input, want := range tests {
		if got := exportSpecifierWithoutLineComments(input); got != want {
			t.Fatalf("exportSpecifierWithoutLineComments(%q) = %q, want %q", input, got, want)
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
		{raw: ` 'a b' `, wantOriginal: "a b", wantExported: "a b"},
		{raw: ` 'a as b' `, wantOriginal: "a as b", wantExported: "a as b"},
		{raw: ` 'a b' as c `, wantOriginal: "a b", wantExported: "c"},
		{raw: ` a as 'b c' `, wantOriginal: "a", wantExported: "b c"},
		{raw: ` 'a as b' as c `, wantOriginal: "a as b", wantExported: "c"},
		{raw: ` "it's" as c `, wantOriginal: "it's", wantExported: "c"},
		{raw: ` 'a b c' d e `},
		{raw: ` a as b as c `},
		{raw: ` ...rest `},
		{raw: ` ...'a' as b `},
		{raw: ` '...' as dots `, wantOriginal: "...", wantExported: "dots"},
		{raw: ` a as '...' `, wantOriginal: "a", wantExported: "..."},
		{raw: ` 'a\0b' as c `},
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

// TestBraceClauseSpecifiersReadsOutsideQuotes pins the quote-aware split: a name
// holding a comma, a brace or the word as must not end its specifier early.
func TestBraceClauseSpecifiersReadsOutsideQuotes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text string
		want []string
		ok   bool
	}{
		{`export { a, b as c } from "m"`, []string{" a", " b as c "}, true},
		{`export { 'a, b' as c, 'x}' } from "m"`, []string{" 'a, b' as c", " 'x}' "}, true},
		{`export { "q{r" } from "m"`, []string{` "q{r" `}, true},
		{`export { 'it\'s, ok' as d } from "m"`, []string{` 'it\'s, ok' as d `}, true},
		{`export { } from "m"`, []string{" "}, true},
		{"export { a // it's\n, b } from \"m\"", []string{" a // it's\n", " b "}, true},
		{"export { a /* it's, } */, b } from \"m\"", []string{" a /* it's, } */", " b "}, true},
		{"export { a // unterminated line comment }", nil, false},
		{`export { a /* unterminated block comment } from "m"`, nil, false},
		{`export { 'a // b', c } from "m"`, []string{" 'a // b'", " c "}, true},
		{`export { a / b } from "m"`, []string{" a / b "}, true},
		{`export { 'unterminated } from "m"`, nil, false},
		{`export * from "m"`, nil, false},
	}
	for _, tt := range tests {
		got, ok := braceClauseSpecifiers(tt.text)
		if ok != tt.ok || !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("braceClauseSpecifiers(%q) = (%q, %v), want (%q, %v)", tt.text, got, ok, tt.want, tt.ok)
		}
	}
}

// TestReExportSpecifiersFromTextKeepsQuotedNamesWhole drives the fallback through
// a parsed statement: the names that used to be dropped or split (a space, the
// word as, a comma) reach ReExportSpecifier whole (#7461).
func TestReExportSpecifiersFromTextKeepsQuotedNamesWhole(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "export { 'a b', 'c as d' as e, 'f, g' as h } from \"./m\";\n")
	defer closeFn()
	got := reExportSpecifiersFromText(firstExportStatement(t, root), source)
	want := [][2]string{{"a b", "a b"}, {"c as d", "e"}, {"f, g", "h"}}
	if len(got) != len(want) {
		t.Fatalf("reExportSpecifiersFromText() = %#v, want %d specifiers", got, len(want))
	}
	for i, pair := range want {
		if got[i].OriginalName != pair[0] || got[i].ExportedName != pair[1] {
			t.Fatalf("specifier %d = %q as %q, want %q as %q", i, got[i].OriginalName, got[i].ExportedName, pair[0], pair[1])
		}
	}
}

// TestReExportSpecifiersDecidesAliasByPresence: an alias written as an empty
// string is still an alias, and an empty original name is still a name. Both are
// valid ECMAScript, and neither may be read as "no alias" (#7461).
func TestReExportSpecifiersDecidesAliasByPresence(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "export { value as '' } from \"./a\";\n")
	got := ReExportSpecifiers(firstExportStatement(t, root), source)
	closeFn()
	if len(got) != 1 || got[0].OriginalName != "value" || got[0].ExportedName != "" {
		t.Fatalf("ReExportSpecifiers(value as '') = %#v, want value exported as the empty name", got)
	}

	// An empty original name is skipped: the reducer reads a missing original as
	// "the same as the exported name", so recording it would resolve
	// export { '' as local } to the module's local.
	root, source, closeFn = parseRootForTest(t, "export { '' as local, kept } from \"./b\";\n")
	got = ReExportSpecifiers(firstExportStatement(t, root), source)
	closeFn()
	if len(got) != 1 || got[0].OriginalName != "kept" {
		t.Fatalf("ReExportSpecifiers('' as local, kept) = %#v, want only kept", got)
	}

	// A name with surrounding whitespace cannot reach the reducer unchanged (it
	// trims names), so the specifier is skipped rather than resolved as "x".
	for _, body := range []string{
		"export { ' x ' } from \"./c\";\n",
		"export { y as ' x ' } from \"./c\";\n",
		"export { ' x ' as y } from \"./c\";\n",
		"export { ' x ', kept } from \"./c\";\n",
	} {
		root, source, closeFn = parseRootForTest(t, body)
		got = ReExportSpecifiers(firstExportStatement(t, root), source)
		closeFn()
		for _, specifier := range got {
			if specifier.OriginalName != "kept" {
				t.Fatalf("ReExportSpecifiers(%q) kept %#v, want the whitespace-bearing specifier skipped", body, specifier)
			}
		}
	}
}

func TestRepresentableModuleName(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{
		"x": true, "a b": true, "": true, " x": false, "x ": false, " x ": false, "\tx": false, "x\n": false,
	} {
		if got := representableModuleName(name); got != want {
			t.Fatalf("representableModuleName(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestReExportSpecifiersFromTextReadsPastCommentsWithQuotes: an apostrophe in a
// comment inside the clause must not open a string and swallow the rest of it
// (#7461).
func TestReExportSpecifiersFromTextReadsPastCommentsWithQuotes(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		"export { , a // don't\n } from \"./m\";\n",
		"export { , a /* it's */ } from \"./m\";\n",
		"export { , a, b // fine\n } from \"./m\";\n",
	} {
		root, source, closeFn := parseRootForTest(t, body)
		got := reExportSpecifiersFromText(firstExportStatement(t, root), source)
		closeFn()
		if len(got) == 0 || got[0].OriginalName != "a" {
			t.Fatalf("reExportSpecifiersFromText(%q) = %#v, want a first", body, got)
		}
	}
}
