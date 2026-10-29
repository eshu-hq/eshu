// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidredact

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// This file pins the S1 seam invariant: internal/cli/evidredact never
// imports internal/cli/mcpsetup, so this package's dependency closure
// stays urlredact-only for the eshu-cli population census (#8874). A
// later edit that re-adds the import fails here instead of silently
// dragging the mcp tool surface back into every consumer.
//
// It follows the doc-lockstep scanner shape: a pure scanner over the
// package's non-test sources plus a caller, so a fixture proves the
// scanner reports what it should. Every assertion that loops proves it
// examined something, because a loop over an empty set passes while
// guarding nothing.

// forbiddenImportOwner is the package path fragment this package must
// never import. The match is a fragment, not the full module path, so a
// vendored or renamed module path cannot smuggle the edge back.
const forbiddenImportOwner = "cli/mcpsetup"

// scanNonTestImports returns the import paths of every non-test .go file
// under dir, keyed by file name, plus the count of files examined.
func scanNonTestImports(t *testing.T, dir string) (map[string][]string, int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	fset := token.NewFileSet()
	got := map[string][]string{}
	examined := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly|parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("ParseFile(%s): %v", name, err)
		}
		examined++
		for _, imp := range parsed.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("unquote import in %s: %v", name, err)
			}
			got[name] = append(got[name], path)
		}
	}
	return got, examined
}

// TestNoMcpsetupImport pins the cut edge: no non-test source in this
// package may import the mcpsetup package.
func TestNoMcpsetupImport(t *testing.T) {
	t.Parallel()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	got, examined := scanNonTestImports(t, filepath.Dir(thisFile))
	if examined == 0 {
		t.Fatal("examined no non-test files; the guard would pass while guarding nothing")
	}
	for name, imports := range got {
		for _, path := range imports {
			if strings.Contains(path, forbiddenImportOwner) {
				t.Errorf("%s imports %q; evidredact must not depend on mcpsetup (#8874 S1 seam)", name, path)
			}
		}
	}
}

// TestScanNonTestImportsReportsForbiddenOwner proves the scanner names
// a forbidden import when one is present, using a fixture directory.
func TestScanNonTestImportsReportsForbiddenOwner(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const fixture = "package evidredact\n\nimport \"github.com/eshu-hq/eshu/go/internal/cli/mcpsetup\"\n\nvar _ = mcpsetup.RedactToken\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(fixture), 0o600); err != nil {
		t.Fatalf("WriteFile(fixture.go): %v", err)
	}
	got, examined := scanNonTestImports(t, dir)
	if examined != 1 {
		t.Fatalf("examined = %d, want 1", examined)
	}
	found := false
	for _, imports := range got {
		for _, path := range imports {
			if strings.Contains(path, forbiddenImportOwner) {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("scanner missed the fixture import %v", got)
	}
}
