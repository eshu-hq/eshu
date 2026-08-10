// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchdocs

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestBoundedContextNeverSplitsARune is the #5052 regression.
//
// boundedContext sliced at a BYTE offset (value[:maxContextBytes]). A
// multi-byte rune straddling that offset was cut in half, so the stored
// ContextText ended in an invalid UTF-8 fragment.
//
// Two things break downstream, both quietly:
//
//   - searchhybrid.documentText checks utf8.ValidString and, when it fails,
//     rewrites the text through []rune — which replaces the broken tail with
//     U+FFFD. The searchable text therefore differs from what was indexed, and
//     documentText's own comment says that canonicalization exists to keep the
//     hash stable, so a mid-rune cut silently changes it.
//   - The document is persisted and served through the API, so an invalid
//     fragment reaches consumers that never asked for a truncated document.
//
// This is not the whole of #5052 — it does not explain a term absent from the
// text entirely — but it is a real, corpus-independent defect in the same
// function, and truncation is invisible in the API response, which reports
// indexed_document_count as though the whole document were searchable.
func TestBoundedContextNeverSplitsARune(t *testing.T) {
	t.Parallel()

	// Land a 3-byte rune across the boundary: fill to one byte short of the
	// cap, then append a rune that cannot fit whole.
	filler := strings.Repeat("a", maxContextBytes-1)
	input := filler + "→tail"

	got := boundedContext(input)

	if !utf8.ValidString(got) {
		t.Fatalf("boundedContext returned invalid UTF-8: last bytes = %q", tailBytes(got))
	}
	if len(got) > maxContextBytes {
		t.Fatalf("boundedContext returned %d bytes, over the %d cap", len(got), maxContextBytes)
	}
	if !strings.HasPrefix(input, got) {
		t.Fatal("boundedContext must return a prefix of its input, not rewritten text")
	}
}

// TestBoundedContextLeavesShortValuesAlone keeps the fix from changing the
// ordinary path: anything within the cap is returned untouched apart from the
// existing trim.
func TestBoundedContextLeavesShortValuesAlone(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"", "  ", "func main() {}", "héllo → wörld"} {
		want := strings.TrimSpace(in)
		if got := boundedContext(in); got != want {
			t.Errorf("boundedContext(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBoundedContextIsIdempotent pins that re-bounding an already-bounded value
// cannot shrink it further, so a document re-projected from stored text keeps a
// stable hash.
func TestBoundedContextIsIdempotent(t *testing.T) {
	t.Parallel()

	once := boundedContext(strings.Repeat("a", maxContextBytes-1) + "→tail")
	twice := boundedContext(once)
	if once != twice {
		t.Fatalf("boundedContext is not idempotent: %d bytes then %d bytes", len(once), len(twice))
	}
}

func tailBytes(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[len(s)-8:]
}
