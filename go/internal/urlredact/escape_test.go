// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package urlredact

import "testing"

// TestDecodedByteAtReadsOneLayer covers the reader both walks scan with. The
// width matters as much as the byte: a caller that advanced by 1 after reading
// an escape would restart inside it, where the "3D" of a "%3D" is ordinary text.
func TestDecodedByteAtReadsOneLayer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		s         string
		i         int
		wantByte  byte
		wantWidth int
	}{
		{name: "literal byte", s: "a=b", i: 1, wantByte: '=', wantWidth: 1},
		{name: "uppercase escape", s: "a%3Db", i: 1, wantByte: '=', wantWidth: EscapeWidth},
		{name: "lowercase escape", s: "a%3db", i: 1, wantByte: '=', wantWidth: EscapeWidth},
		{name: "mixed case escape", s: "a%3Bb", i: 1, wantByte: ';', wantWidth: EscapeWidth},
		{name: "malformed escape is a literal percent", s: "a%ZZb", i: 1, wantByte: '%', wantWidth: 1},
		{name: "truncated escape is a literal percent", s: "a%3", i: 1, wantByte: '%', wantWidth: 1},
		{name: "double-encoded reads as a percent sign", s: "%253D", i: 0, wantByte: '%', wantWidth: EscapeWidth},
		{name: "past the end", s: "ab", i: 2, wantByte: 0, wantWidth: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotByte, gotWidth := DecodedByteAt(tt.s, tt.i)
			if gotByte != tt.wantByte || gotWidth != tt.wantWidth {
				t.Fatalf("DecodedByteAt(%q, %d) = (%q, %d), want (%q, %d)",
					tt.s, tt.i, gotByte, gotWidth, tt.wantByte, tt.wantWidth)
			}
		})
	}
}

// TestDecodedEscapeBeforeOnlyFiresOnAWholeEscape is the backwards half. It has
// to report false for ordinary text, or the key walk would take three bytes of a
// name for one character.
func TestDecodedEscapeBeforeOnlyFiresOnAWholeEscape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		s        string
		i        int
		wantByte byte
		wantOK   bool
	}{
		{name: "escape ends here", s: "api%5F", i: 6, wantByte: '_', wantOK: true},
		{name: "ordinary text", s: "api_key", i: 7, wantOK: false},
		{name: "escape ends one byte later", s: "api%5Fk", i: 7, wantOK: false},
		{name: "malformed", s: "api%ZZ", i: 6, wantOK: false},
		{name: "too near the start", s: "%5", i: 2, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotByte, gotOK := DecodedEscapeBefore(tt.s, tt.i)
			if gotOK != tt.wantOK || (tt.wantOK && gotByte != tt.wantByte) {
				t.Fatalf("DecodedEscapeBefore(%q, %d) = (%q, %v), want (%q, %v)",
					tt.s, tt.i, gotByte, gotOK, tt.wantByte, tt.wantOK)
			}
		})
	}
}

// TestIndexBoundaryHonoursTheDepth pins both halves of the reader. At
// LiteralOrEscaped a scan cannot be fooled by an escape sitting where a
// separator is expected, and it never reports a hit inside one: "%3D" holds a
// "3", so a naive scan for "3" would land mid-escape. At LiteralOnly the same
// escape is ordinary text, which is what keeps a credential containing "%26"
// from being cut in half.
func TestIndexBoundaryHonoursTheDepth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		s         string
		set       string
		depth     BoundaryDepth
		wantIndex int
		wantWidth int
	}{
		{name: "literal", s: "ab=c", set: "=", depth: LiteralOrEscaped, wantIndex: 2, wantWidth: 1},
		{name: "encoded", s: "ab%3Dc", set: "=", depth: LiteralOrEscaped, wantIndex: 2, wantWidth: EscapeWidth},
		{name: "first of two spellings wins", s: "a%3Db=c", set: "=", depth: LiteralOrEscaped, wantIndex: 1, wantWidth: EscapeWidth},
		{name: "no match", s: "abc", set: "=", depth: LiteralOrEscaped, wantIndex: -1},
		{name: "does not match inside an escape", s: "%3D", set: "3", depth: LiteralOrEscaped, wantIndex: -1},
		{name: "any byte of the set", s: "a;b", set: "?&;", depth: LiteralOrEscaped, wantIndex: 1, wantWidth: 1},
		{name: "empty input", s: "", set: "=", depth: LiteralOrEscaped, wantIndex: -1},

		{name: "literal depth reads the literal byte", s: "aa&bb", set: "?&;", depth: LiteralOnly, wantIndex: 2, wantWidth: 1},
		{name: "literal depth ignores the escape", s: "aa%26bb", set: "?&;", depth: LiteralOnly, wantIndex: -1},
		{name: "literal depth finds a literal past an escape", s: "aa%26bb;cc", set: "?&;", depth: LiteralOnly, wantIndex: 7, wantWidth: 1},
		{name: "literal depth skips a multi-byte rune", s: "aé;b", set: ";", depth: LiteralOnly, wantIndex: 3, wantWidth: 1},
		{name: "literal depth on empty input", s: "", set: "&", depth: LiteralOnly, wantIndex: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotIndex, gotWidth := IndexBoundary(tt.s, tt.set, tt.depth)
			if gotIndex != tt.wantIndex || gotWidth != tt.wantWidth {
				t.Fatalf("IndexBoundary(%q, %q, %v) = (%d, %d), want (%d, %d)",
					tt.s, tt.set, tt.depth, gotIndex, gotWidth, tt.wantIndex, tt.wantWidth)
			}
		})
	}
}

// TestDecodeLeavesPlusAlone is the one difference from url.QueryUnescape that
// this package depends on. QueryUnescape reads "token+%3Dx" as "token =x",
// whose key holds whitespace and is therefore skipped by every scan here — the
// decoder would be losing matches, not finding them.
func TestDecodeLeavesPlusAlone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		s           string
		want        string
		wantChanged bool
	}{
		{name: "escape is unwrapped", s: "api%5Fkey", want: "api_key", wantChanged: true},
		{name: "plus survives", s: "token+%3Dx", want: "token+=x", wantChanged: true},
		{name: "nothing to do", s: "api_key", want: "api_key"},
		{name: "malformed is refused", s: "api%ZZkey", want: "api%ZZkey"},
		{name: "one layer only", s: "%253D", want: "%3D", wantChanged: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, changed := Decode(tt.s)
			if got != tt.want || changed != tt.wantChanged {
				t.Fatalf("Decode(%q) = (%q, %v), want (%q, %v)", tt.s, got, changed, tt.want, tt.wantChanged)
			}
		})
	}
}
