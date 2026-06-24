// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Tests for the filelength custom linter. The tests exercise the
// `skip` predicate and the `countLines` helper directly (so they
// run in the normal `go test` flow without needing the
// `-buildmode=plugin` build) and then verify the public `New`
// constructor returns the expected single analyzer.
package main

import (
	"os"
	"path/filepath"
	"strings"
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
	if got, want := analyzers[0].Name, "filelength"; got != want {
		t.Fatalf("analyzer name = %q, want %q", got, want)
	}
	if analyzers[0].Doc == "" {
		t.Fatal("analyzer Doc must not be empty; golangci-lint uses it for --help output")
	}
}

func TestSkip(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{path: "go/internal/foo/foo.go", want: false},
		{path: "go/internal/foo/foo_test.go", want: true},
		{path: "go/internal/foo/testdata/case.go", want: true},
		{path: "go/internal/foo/vendor/lib/lib.go", want: true},
		{path: "go/internal/foo/generated/x.go", want: true},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if got := skip(c.path); got != c.want {
				t.Fatalf("skip(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func TestCountLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.go")

	const want = 1234
	var b strings.Builder
	for i := 0; i < want; i++ {
		b.WriteString("package x\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := countLines(path)
	if err != nil {
		t.Fatalf("countLines: %v", err)
	}
	if got != want {
		t.Fatalf("countLines = %d, want %d", got, want)
	}
}

func TestCountLinesMissing(t *testing.T) {
	_, err := countLines(filepath.Join(t.TempDir(), "does-not-exist.go"))
	if err == nil {
		t.Fatal("countLines on missing file returned nil error")
	}
}
