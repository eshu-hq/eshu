// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Tests for the dirgate custom linter's directory-level primitives: the
// New constructor, skipDir, qualifyingFiles, namingSubpackages,
// representativeFile, qualifyingDigest, and normalizeDir. Naming-violation
// detection, nolint parsing, and the grandfather evaluation each have their
// own test file (naming_test.go, nolint_test.go, grandfather_eval_test.go).
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewReturnsExpectedAnalyzer(t *testing.T) {
	analyzers, err := New(nil)
	if err != nil {
		t.Fatalf("New(nil) returned error: %v", err)
	}
	if len(analyzers) != 1 {
		t.Fatalf("New returned %d analyzers, want 1", len(analyzers))
	}
	if got, want := analyzers[0].Name, "dirgate"; got != want {
		t.Fatalf("analyzer name = %q, want %q", got, want)
	}
	if analyzers[0].Doc == "" {
		t.Fatal("analyzer Doc must not be empty; golangci-lint uses it for --help output")
	}
}

func TestSkipDir(t *testing.T) {
	cases := []struct {
		dir  string
		want bool
	}{
		{dir: "internal/foo", want: false},
		{dir: "internal/foo/bar", want: false},
		{dir: "internal/foo/vendor", want: true},
		{dir: "internal/foo/testdata", want: true},
		{dir: "internal/foo/generated", want: true},
		{dir: "internal/foo/vendor/nested", want: true},
		{dir: "internal/foo/testdata/nested", want: true},
		{dir: "internal/.hidden", want: true},
		{dir: ".git", want: true},
		{dir: "vendor", want: true},
		// Substring-but-not-a-path-segment must NOT skip: "vendorish" is a
		// legitimate package name, not the vendor/ tree.
		{dir: "internal/vendorish", want: false},
		{dir: "internal/testdataset", want: false},
	}
	for _, c := range cases {
		t.Run(c.dir, func(t *testing.T) {
			if got := skipDir(c.dir); got != c.want {
				t.Fatalf("skipDir(%q) = %v, want %v", c.dir, got, c.want)
			}
		})
	}
}

func TestQualifyingFiles(t *testing.T) {
	dir := t.TempDir()
	names := []string{"a.go", "b.go", "b_test.go", "c.txt", "z_test.go"}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("package x\n"), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", n, err)
		}
	}
	// A subdirectory named "sub.go" (unlikely, but qualifyingFiles must only
	// ever list regular files, never directories).
	if err := os.Mkdir(filepath.Join(dir, "sub.go"), 0o750); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	got, err := qualifyingFiles(dir)
	if err != nil {
		t.Fatalf("qualifyingFiles: %v", err)
	}
	want := []string{"a.go", "b.go"}
	if !equalStrings(got, want) {
		t.Fatalf("qualifyingFiles = %v, want %v", got, want)
	}
}

func TestQualifyingFilesMissingDir(t *testing.T) {
	_, err := qualifyingFiles(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("qualifyingFiles on a missing directory returned nil error")
	}
}

func TestNamingSubpackages(t *testing.T) {
	dir := t.TempDir()
	mustMkdirGo(t, dir, "bar", "bar.go")
	mustMkdirGo(t, dir, "empty") // no .go file: not a package, must be excluded
	if err := os.Mkdir(filepath.Join(dir, "vendor"), 0o750); err != nil {
		t.Fatalf("mkdir vendor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vendor", "x.go"), []byte("package x\n"), 0o600); err != nil {
		t.Fatalf("write vendor fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "baz.go"), []byte("package x\n"), 0o600); err != nil {
		t.Fatalf("write baz.go: %v", err)
	}

	got, err := namingSubpackages(dir)
	if err != nil {
		t.Fatalf("namingSubpackages: %v", err)
	}
	want := []string{"bar"}
	if !equalStrings(got, want) {
		t.Fatalf("namingSubpackages = %v, want %v", got, want)
	}
}

func TestRepresentativeFile(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		want  string
	}{
		{name: "doc.go preferred", files: []string{"a.go", "doc.go", "z.go"}, want: "doc.go"},
		{name: "first sorted without doc.go", files: []string{"z.go", "a.go"}, want: "a.go"},
		{name: "single file", files: []string{"only.go"}, want: "only.go"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := representativeFile(c.files); got != c.want {
				t.Fatalf("representativeFile(%v) = %q, want %q", c.files, got, c.want)
			}
		})
	}
}

func TestQualifyingDigest(t *testing.T) {
	d1 := qualifyingDigest([]string{"a.go", "b.go"})
	d2 := qualifyingDigest([]string{"a.go", "b.go"})
	if d1 != d2 {
		t.Fatalf("qualifyingDigest is not deterministic: %q vs %q", d1, d2)
	}
	if d1 == "" || len(d1) != 64 {
		t.Fatalf("qualifyingDigest returned non-sha256-hex value: %q", d1)
	}

	// Order in the input must not matter -- the digest is defined over the
	// SORTED list, so callers passing an already-sorted or unsorted slice
	// of the same set get the same answer.
	d3 := qualifyingDigest([]string{"b.go", "a.go"})
	if d1 != d3 {
		t.Fatalf("qualifyingDigest depends on input order: %q vs %q", d1, d3)
	}

	// Adding or removing a file must change the digest.
	d4 := qualifyingDigest([]string{"a.go", "b.go", "c.go"})
	if d4 == d1 {
		t.Fatal("qualifyingDigest did not change when a file was added")
	}
	d5 := qualifyingDigest([]string{"a.go"})
	if d5 == d1 {
		t.Fatal("qualifyingDigest did not change when a file was removed")
	}
}

func TestNormalizeDir(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{path: "internal/query/foo.go", want: "internal/query"},
		{path: "./internal/query/foo.go", want: "internal/query"},
		{path: "/Users/dev/repos/eshu/go/internal/query/foo.go", want: "internal/query"},
		{path: "/Users/dev/repos/eshu/go/cmd/eshu/main.go", want: "cmd/eshu"},
		{path: "foo.go", want: "."},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if got := normalizeDir(c.path); got != c.want {
				t.Fatalf("normalizeDir(%q) = %q, want %q", c.path, got, c.want)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mustMkdirGo creates dir/name as a directory and, for each extra basename
// given, writes an empty-package .go file inside it.
func mustMkdirGo(t *testing.T, dir, name string, goFiles ...string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.Mkdir(p, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
	for _, f := range goFiles {
		if err := os.WriteFile(filepath.Join(p, f), []byte("package "+name+"\n"), 0o600); err != nil {
			t.Fatalf("write %s/%s: %v", p, f, err)
		}
	}
}
