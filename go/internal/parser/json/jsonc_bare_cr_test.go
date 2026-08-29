// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// bareCRJSONCDocument is a JSONC document written with classic-Mac line
// endings: every line break is a bare '\r' and the document contains no '\n'
// at all. A line-comment scan that only stops at '\n' runs from the `//` to
// EOF and silently swallows the rest of the file (#6268).
const bareCRJSONCDocument = "{\r// note\r\"compilerOptions\": {\"strict\": true},\r\"extends\": \"./base.json\"\r}"

// crlfJSONCDocument is the same document with CRLF endings. It is the control
// for the bare-CR cases: CRLF already worked before #6268, so a shared
// regression would show up here too and tell us the fix broke ordinary files
// rather than repairing the classic-Mac one.
const crlfJSONCDocument = "{\r\n// note\r\n\"compilerOptions\": {\"strict\": true},\r\n\"extends\": \"./base.json\"\r\n}"

// assertJSONCOffsetMapIsFaithful checks the parallel offset-map contract
// stripJSONCCommentsWithOffsets documents: one entry per result byte plus a
// len(source) sentinel, and every entry pointing at the source byte that
// produced it. A comment-scan change that writes the wrong terminator byte
// would keep the text assertions green while corrupting every downstream
// line_number lookup, so both halves are asserted together.
func assertJSONCOffsetMapIsFaithful(t *testing.T, source string, stripped string, offsets []int64) {
	t.Helper()

	if got, want := len(offsets), len(stripped)+1; got != want {
		t.Fatalf("offset map length = %d, want %d", got, want)
	}
	if got, want := offsets[len(stripped)], int64(len(source)); got != want {
		t.Fatalf("offset map sentinel = %d, want %d", got, want)
	}
	for index := range len(stripped) {
		at := offsets[index]
		if at < 0 || int(at) >= len(source) {
			t.Fatalf("offsets[%d] = %d is outside source", index, at)
		}
		if source[at] != stripped[index] {
			t.Fatalf("offsets[%d] = %d points at %q, want %q", index, at, source[at], stripped[index])
		}
	}
}

// TestStripJSONCCommentsEndsLineCommentAtBareCR is the #6268 regression: a
// bare '\r' must end a `//` comment exactly as '\n' does, so the document
// after the first comment survives the strip.
func TestStripJSONCCommentsEndsLineCommentAtBareCR(t *testing.T) {
	t.Parallel()

	stripped, offsets := stripJSONCCommentsWithOffsets(bareCRJSONCDocument)
	for _, want := range []string{`"compilerOptions"`, `"extends"`, "}"} {
		if !strings.Contains(stripped, want) {
			t.Fatalf("stripJSONCCommentsWithOffsets(%q) = %q, want it to keep %s", bareCRJSONCDocument, stripped, want)
		}
	}
	if strings.Contains(stripped, "note") {
		t.Fatalf("stripJSONCCommentsWithOffsets(%q) = %q, want the comment body removed", bareCRJSONCDocument, stripped)
	}
	assertJSONCOffsetMapIsFaithful(t, bareCRJSONCDocument, stripped, offsets)
}

// TestStripJSONCCommentsKeepsCRLFDocumentIntact is the control half of the
// bare-CR pin: CRLF documents were never broken, so this must stay green both
// before and after the #6268 fix.
func TestStripJSONCCommentsKeepsCRLFDocumentIntact(t *testing.T) {
	t.Parallel()

	stripped, offsets := stripJSONCCommentsWithOffsets(crlfJSONCDocument)
	for _, want := range []string{`"compilerOptions"`, `"extends"`, "}"} {
		if !strings.Contains(stripped, want) {
			t.Fatalf("stripJSONCCommentsWithOffsets(CRLF) = %q, want it to keep %s", stripped, want)
		}
	}
	if strings.Contains(stripped, "note") {
		t.Fatalf("stripJSONCCommentsWithOffsets(CRLF) = %q, want the comment body removed", stripped)
	}
	assertJSONCOffsetMapIsFaithful(t, crlfJSONCDocument, stripped, offsets)
}

// TestParseBareCRJSONCKeepsKeysAfterLineComment drives the real parser entry
// point, not just the strip helper: a bare-CR tsconfig.json must still yield
// its top-level keys. Before #6268 the normalized buffer was `{` alone and
// Parse returned a decode error, so every fact this file should produce was
// absent from the graph.
func TestParseBareCRJSONCKeepsKeysAfterLineComment(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "tsconfig.json", bareCRJSONCDocument)
	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if got, want := topLevelKeys(t, payload), []string{"compilerOptions", "extends"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("top-level keys = %#v, want %#v", got, want)
	}
}

// TestParseBareCRJSONCReportsRealLineNumbers is the #6268 follow-up found in
// review: recovering the document is not enough if every recovered row is
// stamped line 1. `"extends"` sits on physical line 4 of the bare-CR
// document, and buildNewlineIndex must count the lone '\r' bytes for the
// emitted row (and the canonical content identity derived from it) to be
// truthful.
func TestParseBareCRJSONCReportsRealLineNumbers(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "tsconfig.json", bareCRJSONCDocument)
	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	for _, row := range rows {
		if name, _ := row["name"].(string); name != "extends" {
			continue
		}
		if got, want := row["line_number"], 4; got != want {
			t.Fatalf("extends line_number = %#v, want %d (its real bare-CR source line); row = %#v", got, want, row)
		}
		return
	}
	t.Fatalf("no extends row emitted; rows = %#v", rows)
}

// TestParseCRLFJSONCReportsRealLineNumbers is the control for the line
// counting change: CRLF must still count one break per pair, so `"extends"`
// stays on line 4 rather than doubling to 7.
func TestParseCRLFJSONCReportsRealLineNumbers(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "tsconfig.json", crlfJSONCDocument)
	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	for _, row := range rows {
		if name, _ := row["name"].(string); name != "extends" {
			continue
		}
		if got, want := row["line_number"], 4; got != want {
			t.Fatalf("extends line_number = %#v, want %d: CRLF must count one break per pair; row = %#v", got, want, row)
		}
		return
	}
	t.Fatalf("no extends row emitted; rows = %#v", rows)
}

// TestNewlineIndexCountsBareCRAndCRLFOnce is the unit-level pin under both
// Parse tests above: a lone '\r' is a line break, a CRLF pair is exactly one
// break, and a mixed file gets both right.
func TestNewlineIndexCountsBareCRAndCRLFOnce(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
		offset int64
		want   int
	}{
		{name: "bare CR ends a line", source: "a\rb\rc", offset: 4, want: 3},
		{name: "CRLF counts once", source: "a\r\nb\r\nc", offset: 6, want: 3},
		{name: "LF unchanged", source: "a\nb\nc", offset: 4, want: 3},
		{name: "mixed endings", source: "a\rb\r\nc\nd", offset: 7, want: 4},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			idx := buildNewlineIndex([]byte(testCase.source))
			if got := idx.lineAt(testCase.offset); got != testCase.want {
				t.Fatalf("buildNewlineIndex(%q).lineAt(%d) = %d, want %d", testCase.source, testCase.offset, got, testCase.want)
			}
		})
	}
}

// TestStripJSONCCommentsCRLFBytesAreExact replaces the PR's unproven "the
// same bytes come out" claim with the bytes that actually come out. The '\r'
// of a CRLF line comment used to be swallowed by the scan and is now emitted,
// so the stripped result really does differ by that one byte -- harmless
// (it is JSON whitespace and the offset map stays faithful) but not
// "unchanged", and only an exact-byte assertion can say so.
func TestStripJSONCCommentsCRLFBytesAreExact(t *testing.T) {
	t.Parallel()

	stripped, _ := stripJSONCCommentsWithOffsets(crlfJSONCDocument)
	want := "{\r\n\r\n\"compilerOptions\": {\"strict\": true},\r\n\"extends\": \"./base.json\"\r\n}"
	if stripped != want {
		t.Fatalf("stripJSONCCommentsWithOffsets(CRLF) = %q, want %q", stripped, want)
	}
}
