// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Tests for the dirgate custom linter's directory-level primitives: the
// New constructor, skipDir, qualifyingFiles, namingSubpackages,
// representativeFile, qualifyingDigest, and normalizeDir. Naming-violation
// detection, nolint parsing, and the grandfather evaluation each have their
// own test file (naming_test.go, nolint_test.go, grandfather_eval_test.go).
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis"
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

// TestNormalizeDir pins normalizeDir's contract: it receives a DIRECTORY
// (never a file path). normalizeDir's only caller, run(), already reduced
// a file position to its containing directory via filepath.Dir before
// calling this function (packageFilePositions), so these cases are
// directory shapes, not file shapes -- an earlier version of this test fed
// file paths (e.g. "internal/query/foo.go") and normalizeDir called
// filepath.Dir a second time to compensate, which produced the wrong key
// the moment run() (correctly) started passing it an already-resolved
// directory. See TestRunRecognizesGrandfatheredDirectoryThroughRealKeyDerivation
// for the regression test that exercises the real call chain end to end.
func TestNormalizeDir(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{path: "internal/query", want: "internal/query"},
		{path: "./internal/query", want: "internal/query"},
		{path: "/Users/dev/repos/eshu/go/internal/query", want: "internal/query"},
		{path: "/Users/dev/repos/eshu/go/cmd/eshu", want: "cmd/eshu"},
		// The package sits directly at the go/ module root (no subdirectory).
		{path: "/Users/dev/repos/eshu/go", want: "."},
		{path: "go", want: "."},
		{path: ".", want: "."},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if got := normalizeDir(c.path); got != c.want {
				t.Fatalf("normalizeDir(%q) = %q, want %q", c.path, got, c.want)
			}
		})
	}
}

// TestRunRecognizesGrandfatheredDirectoryThroughRealKeyDerivation is the
// regression test for the #6054 P1 defect found in codex review on PR
// #6081: run() calls evaluateDirectory(normalizeDir(dir), dir, ...) where
// dir is ALREADY a directory (packageFilePositions resolved it via
// filepath.Dir), but normalizeDir used to call filepath.Dir a SECOND time,
// stripping the package's own directory segment. "go/internal/query"
// normalized to "internal" instead of "internal/query", so the derived key
// never matched any row in grandfatheredDirectories and every pinned
// directory would have been reported as newly, un-grandfatheredly over
// cap. Every other test in this package calls evaluateDirectory directly
// with an already-correct key and so enters below this seam; this test
// goes through run() itself -- golangci-lint's actual entry point -- with
// a real *ast.File/*token.FileSet pair rooted at a directory whose
// grandfather-ledger key has more than one path segment, which is exactly
// the shape the double-Dir bug corrupted.
func TestRunRecognizesGrandfatheredDirectoryThroughRealKeyDerivation(t *testing.T) {
	const wantCount = maxDirFiles + 5
	names := numberedFiles(wantCount)
	digest := qualifyingDigest(names)

	orig := grandfatheredDirectories
	grandfatheredDirectories = map[string]grandfatherEntry{
		"internal/fakepkg": {FileCount: wantCount, Digest: digest},
	}
	t.Cleanup(func() { grandfatheredDirectories = orig })

	root := t.TempDir()
	pkgDir := filepath.Join(root, "go", "internal", "fakepkg")
	if err := os.MkdirAll(pkgDir, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", pkgDir, err)
	}
	writeGoFiles(t, pkgDir, names...)

	fset := token.NewFileSet()
	astFiles := make([]*ast.File, 0, len(names))
	for _, n := range names {
		p := filepath.Join(pkgDir, n)
		af, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		astFiles = append(astFiles, af)
	}

	var diags []analysis.Diagnostic
	pass := &analysis.Pass{
		Fset:  fset,
		Files: astFiles,
		Report: func(d analysis.Diagnostic) {
			diags = append(diags, d)
		},
	}

	if _, err := run(pass); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("run() findings = %v, want none: internal/fakepkg is pinned at exactly its live "+
			"count and digest and must be recognised as grandfathered through run()'s real key "+
			"derivation, not un-grandfathered by normalizeDir stripping its own directory segment",
			diags)
	}
}

// TestRunReportsCapFindingOnlyFromThePassThatLoadedTheNamedFile is the
// regression test for the #6054 P2 gap identified in review of PR #6081:
// run()'s "if !ok { continue }" branch (the fix for reporting a finding
// against an arbitrary loaded file when the named one is absent from the
// current go/analysis Pass's file set) had zero dedicated coverage.
// TestRunRecognizesGrandfatheredDirectoryThroughRealKeyDerivation goes
// through run() too, but it is built so evaluateDirectory returns no
// findings, so it never reaches the byBase lookup miss at all.
//
// This test builds TWO analysis.Pass values over the SAME real directory
// with DISJOINT file sets, mirroring the shape go/analysis actually
// produces: the production package variant, the internal test variant, and
// the external _test variant all share one directory but each loads a
// different subset of its files. One pass loads every qualifying file
// EXCEPT the cap finding's representative file (simulating a variant that
// cannot see it); the other loads ONLY the representative file (simulating
// the variant that owns it). The finding must be reported exactly once, by
// the pass that loaded the named file, and not at all by the pass that did
// not -- never against a substitute file, which is what the pre-fix
// anyPos(byBase) fallback did. Confirmed against a scratch copy with the
// pre-fix anyPos(byBase) fallback restored: that copy fails this test's
// first assertion (the pass without the representative file reports 1
// finding, attributed to an arbitrary loaded file, instead of 0).
func TestRunReportsCapFindingOnlyFromThePassThatLoadedTheNamedFile(t *testing.T) {
	const wantCount = maxDirFiles + 3
	names := numberedFiles(wantCount)

	orig := grandfatheredDirectories
	grandfatheredDirectories = map[string]grandfatherEntry{}
	t.Cleanup(func() { grandfatheredDirectories = orig })

	root := t.TempDir()
	pkgDir := filepath.Join(root, "go", "internal", "splitpkg")
	if err := os.MkdirAll(pkgDir, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", pkgDir, err)
	}
	writeGoFiles(t, pkgDir, names...)

	// representativeFile(names) is deterministic: with no doc.go present it
	// is the alphabetically-first name, which numberedFiles' zero-padded
	// scheme guarantees is names[0] ("file0000.go").
	rep := representativeFile(names)
	if rep != names[0] {
		t.Fatalf("test setup: representative file = %q, want %q (numberedFiles must sort first-to-last)", rep, names[0])
	}

	fset := token.NewFileSet()
	astByName := make(map[string]*ast.File, len(names))
	for _, n := range names {
		p := filepath.Join(pkgDir, n)
		af, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		astByName[n] = af
	}

	// "withoutRep" mimics a package variant (e.g. an external _test
	// package) that loaded every qualifying file EXCEPT the representative
	// one.
	withoutRep := make([]*ast.File, 0, len(names)-1)
	for _, n := range names[1:] {
		withoutRep = append(withoutRep, astByName[n])
	}
	// "withRep" mimics the variant that DOES load the representative file
	// (e.g. the production package variant).
	withRep := []*ast.File{astByName[rep]}

	runPass := func(files []*ast.File) []analysis.Diagnostic {
		var diags []analysis.Diagnostic
		pass := &analysis.Pass{
			Fset:  fset,
			Files: files,
			Report: func(d analysis.Diagnostic) {
				diags = append(diags, d)
			},
		}
		if _, err := run(pass); err != nil {
			t.Fatalf("run: %v", err)
		}
		return diags
	}

	withoutDiags := runPass(withoutRep)
	if len(withoutDiags) != 0 {
		t.Fatalf("pass without %s reported %d finding(s), want 0: the byBase lookup miss must stay silent, "+
			"not report against an arbitrary loaded file (got %v)", rep, len(withoutDiags), withoutDiags)
	}

	withDiags := runPass(withRep)
	if len(withDiags) != 1 {
		t.Fatalf("pass with %s reported %d finding(s), want exactly 1", rep, len(withDiags))
	}
	gotFile := filepath.Base(fset.Position(withDiags[0].Pos).Filename)
	if gotFile != rep {
		t.Fatalf("finding reported against %q, want %q (the file it names)", gotFile, rep)
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
