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

// TestNormalizeLineEndingsRewritesOnlyBareCR pins the substitution rule: a
// '\r' with no '\n' after it becomes '\n', a CRLF pair survives intact, and
// the result is always the same length as the input (issue #6306). The
// length invariant is what keeps every downstream byte offset -- the JSONC
// offset translator, SQL entity spans, IndexSource snippets -- valid against
// the on-disk file.
func TestNormalizeLineEndingsRewritesOnlyBareCR(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{name: "empty", source: "", want: ""},
		{name: "no carriage return", source: "a\nb\nc\n", want: "a\nb\nc\n"},
		{name: "crlf only", source: "a\r\nb\r\nc\r\n", want: "a\r\nb\r\nc\r\n"},
		{name: "bare cr only", source: "a\rb\rc\r", want: "a\nb\nc\n"},
		{name: "mixed crlf then bare", source: "a\r\nb\rc\n", want: "a\r\nb\nc\n"},
		{name: "mixed bare then crlf", source: "a\rb\r\nc\n", want: "a\nb\r\nc\n"},
		{name: "trailing bare cr at eof", source: "a\nb\r", want: "a\nb\n"},
		{name: "consecutive bare cr", source: "a\r\r\rb", want: "a\n\n\nb"},
		{name: "cr cr lf keeps the pair", source: "a\r\r\nb", want: "a\n\r\nb"},
		{name: "lone cr is the whole source", source: "\r", want: "\n"},
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
	source := []byte("alpha\rbeta\r\ngamma\r")
	original := append([]byte(nil), source...)
	got := NormalizeLineEndings(source)
	if !bytes.Equal(source, original) {
		t.Fatalf("NormalizeLineEndings mutated its input: got %q, want %q", source, original)
	}
	if string(got) != "alpha\nbeta\r\ngamma\n" {
		t.Fatalf("NormalizeLineEndings(%q) = %q, want %q", original, got, "alpha\nbeta\r\ngamma\n")
	}
}

// TestReadSourceNormalizesBareCRFromDiskAndCache proves ReadSource applies
// the normalization on BOTH of its paths. The cached path is the one that
// matters in production: the git collector reads the file itself and calls
// PrimeSource, so a disk-only normalization would leave the real parse path
// broken (issue #6306).
func TestReadSourceNormalizesBareCRFromDiskAndCache(t *testing.T) {
	raw := []byte("alpha\rbeta\r\ngamma\r")
	want := "alpha\nbeta\r\ngamma\n"

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
