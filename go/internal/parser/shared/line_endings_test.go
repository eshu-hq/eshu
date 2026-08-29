// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

// TestNormalizeLineEndingsRewritesOnlyANewlineFreeSourceTable pins the
// substitution rule across the whole input space: a source with no '\n'
// anywhere has every '\r' rewritten, a source with any '\n' is left exactly
// as it was, and the result is always the same length as the input (issue
// #6306). The length invariant is what keeps every downstream byte offset --
// the JSONC offset translator, SQL entity spans, IndexSource snippets --
// valid against the on-disk file.
func TestNormalizeLineEndingsRewritesOnlyANewlineFreeSourceTable(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{name: "empty", source: "", want: ""},
		{name: "no carriage return", source: "a\nb\nc\n", want: "a\nb\nc\n"},
		{name: "no terminator at all", source: "alpha", want: "alpha"},
		{name: "crlf only", source: "a\r\nb\r\nc\r\n", want: "a\r\nb\r\nc\r\n"},
		{name: "bare cr only", source: "a\rb\rc\r", want: "a\nb\nc\n"},
		{name: "consecutive bare cr", source: "a\r\r\rb", want: "a\n\n\nb"},
		{name: "lone cr is the whole source", source: "\r", want: "\n"},
		// Every case below carries a '\n', so its line convention is already
		// established and its '\r' bytes are data or half a CRLF pair. None
		// of them may be rewritten.
		{name: "mixed crlf then cr keeps the cr", source: "a\r\nb\rc\n", want: "a\r\nb\rc\n"},
		{name: "mixed cr then crlf keeps the cr", source: "a\rb\r\nc\n", want: "a\rb\r\nc\n"},
		{name: "trailing cr at eof after a newline", source: "a\nb\r", want: "a\nb\r"},
		{name: "cr cr lf keeps both", source: "a\r\r\nb", want: "a\r\r\nb"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := NormalizeLineEndings([]byte(testCase.source))
			if string(got) != testCase.want {
				t.Fatalf("NormalizeLineEndings(%q) = %q, want %q", testCase.source, got, testCase.want)
			}
			if len(got) != len(testCase.source) {
				t.Fatalf("NormalizeLineEndings(%q) length = %d, want %d (the rewrite must preserve byte offsets)",
					testCase.source, len(got), len(testCase.source))
			}
		})
	}
}

// TestNormalizeLineEndingsLeavesLFAndCRLFByteIdentical is the no-regression
// proof for the two line-ending styles that already worked. It asserts more
// than value equality: an LF or CRLF source must come back as the caller's
// OWN backing array, which proves no copy was taken and therefore that not a
// single byte could have moved.
func TestNormalizeLineEndingsLeavesLFAndCRLFByteIdentical(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{name: "lf", source: "package main\n\nfunc main() {}\n"},
		{name: "crlf", source: "package main\r\n\r\nfunc main() {}\r\n"},
		{name: "lf no trailing newline", source: "alpha"},
		{name: "crlf ending at eof", source: "alpha\r\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			source := []byte(testCase.source)
			got := NormalizeLineEndings(source)
			if !bytes.Equal(got, source) {
				t.Fatalf("NormalizeLineEndings(%q) = %q, want the input unchanged", testCase.source, got)
			}
			if len(source) > 0 && unsafe.SliceData(got) != unsafe.SliceData(source) {
				t.Fatalf("NormalizeLineEndings(%q) copied the buffer; an LF/CRLF source must be returned as the same slice",
					testCase.source)
			}
		})
	}
}

// TestNormalizeLineEndingsDoesNotMutateInput guards the property the git
// collector depends on: it hands one []byte to both the parser and the
// content snapshot whose digest becomes content identity, so normalization
// must never write through the caller's slice.
func TestNormalizeLineEndingsDoesNotMutateInput(t *testing.T) {
	source := []byte("alpha\rbeta\rgamma\r")
	original := append([]byte(nil), source...)
	got := NormalizeLineEndings(source)
	if !bytes.Equal(source, original) {
		t.Fatalf("NormalizeLineEndings mutated its input: got %q, want %q", source, original)
	}
	if string(got) != "alpha\nbeta\ngamma\n" {
		t.Fatalf("NormalizeLineEndings(%q) = %q, want %q", original, got, "alpha\nbeta\ngamma\n")
	}
}

// TestReadSourceNormalizesBareCRFromDiskAndCache proves ReadSource applies
// the normalization on BOTH of its paths. The cached path is the one that
// matters in production: the git collector reads the file itself and calls
// PrimeSource, so a disk-only normalization would leave the real parse path
// broken (issue #6306).
func TestReadSourceNormalizesBareCRFromDiskAndCache(t *testing.T) {
	raw := []byte("alpha\rbeta\rgamma\r")
	want := "alpha\nbeta\ngamma\n"

	path := filepath.Join(t.TempDir(), "classic.txt")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
	}

	fromDisk, err := ReadSource(path)
	if err != nil {
		t.Fatalf("ReadSource(%q) error = %v, want nil", path, err)
	}
	if string(fromDisk) != want {
		t.Fatalf("ReadSource(%q) from disk = %q, want %q", path, fromDisk, want)
	}

	primed := append([]byte(nil), raw...)
	PrimeSource(path, primed)
	defer ClearSource(path)
	fromCache, err := ReadSource(path)
	if err != nil {
		t.Fatalf("ReadSource(%q) after PrimeSource error = %v, want nil", path, err)
	}
	if string(fromCache) != want {
		t.Fatalf("ReadSource(%q) from the primed cache = %q, want %q", path, fromCache, want)
	}
	if !bytes.Equal(primed, raw) {
		t.Fatalf("ReadSource mutated the primed bytes: got %q, want %q", primed, raw)
	}
}

// TestNormalizeLineEndingsPreservesCarriageReturnDataInLFAuthoredSource is the
// boundary this normalization deliberately refuses to cross (issue #6306, PR
// review). A source that already contains a '\n' has an established LF or
// CRLF line convention, so a '\r' in it is either half of a CRLF pair or
// literal data inside a string, char, regex or wire-payload constant --
// never a line terminator. Rewriting it would silently change the parsed
// VALUE of that literal in a file whose line numbers were already correct.
//
// Byte identity is the assertion, not value equality: the source must come
// back as the caller's OWN backing array, which proves no copy was taken and
// therefore that not a single byte could have moved.
func TestNormalizeLineEndingsPreservesCarriageReturnDataInLFAuthoredSource(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{name: "cr inside a go raw string", source: "package p\n\nconst Route = `/foo\rbar`\n"},
		{name: "cr inside an interpreted string", source: "package p\n\nvar s = \"a\rb\"\n"},
		{name: "cr inside a crlf authored file", source: "package p\r\n\r\nconst Route = `/foo\rbar`\r\n"},
		{name: "cr before the first newline", source: "alpha\rbeta\ngamma\n"},
		{name: "cr after the last newline", source: "alpha\nbeta\rgamma"},
		{name: "consecutive data crs", source: "alpha\n\r\r\rbeta\n"},
		{name: "cr cr lf", source: "a\r\r\nb"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			source := []byte(testCase.source)
			got := NormalizeLineEndings(source)
			if string(got) != testCase.source {
				t.Fatalf("NormalizeLineEndings(%q) = %q, want the input unchanged", testCase.source, got)
			}
			if unsafe.SliceData(got) != unsafe.SliceData(source) {
				t.Fatalf("NormalizeLineEndings(%q) copied the buffer; a source that already contains '\\n' must be returned as the same slice",
					testCase.source)
			}
		})
	}
}

// TestNormalizeLineEndingsRewritesOnlyANewlineFreeSource pins the positive
// half of the same rule: a source with a '\r' and NO '\n' anywhere cannot be
// using '\r' for anything but line separation, because it has no other
// terminator. That is the classic-Mac file issue #6306 is about, and it is
// the only shape this function rewrites.
func TestNormalizeLineEndingsRewritesOnlyANewlineFreeSource(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{name: "classic mac file", source: "alpha\rbeta\rgamma\r", want: "alpha\nbeta\ngamma\n"},
		{name: "classic mac blank lines", source: "alpha\r\r\rbeta\r", want: "alpha\n\n\nbeta\n"},
		{name: "lone cr is the whole source", source: "\r", want: "\n"},
		{name: "no trailing terminator", source: "alpha\rbeta", want: "alpha\nbeta"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := NormalizeLineEndings([]byte(testCase.source))
			if string(got) != testCase.want {
				t.Fatalf("NormalizeLineEndings(%q) = %q, want %q", testCase.source, got, testCase.want)
			}
			if len(got) != len(testCase.source) {
				t.Fatalf("NormalizeLineEndings(%q) length = %d, want %d (the rewrite must preserve byte offsets)",
					testCase.source, len(got), len(testCase.source))
			}
		})
	}
}
