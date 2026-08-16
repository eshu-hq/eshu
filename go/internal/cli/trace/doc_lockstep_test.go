// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package trace

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageImportsStayStandardLibraryOnly is the standing guard behind the
// dependency claim README.md makes: "Standard library only ... No cobra, and
// no other internal package."
//
// The README calls that rule machine-checkable and then gives a shell command
// (`go list -deps ./internal/cli/trace | rg spf13`) that nothing runs. A
// command in a document is not a gate: it passes for as long as someone
// remembers to type it, and the claim silently stops being true the first time
// nobody does. This test is the check the README describes.
//
// The rule is structural rather than a fixed allow-list, so adding another
// standard-library import does not need this test edited, while adding cobra,
// another internal package, or any module dependency does turn it red.
func TestPackageImportsStayStandardLibraryOnly(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	scanned, imports := 0, 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Clean(name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++

		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			imports++
			// A standard-library path's first segment carries no dot; every
			// module path's does (github.com/..., gopkg.in/...). That is the
			// same distinction `go list` draws, without shelling out to it.
			if strings.Contains(strings.SplitN(path, "/", 2)[0], ".") {
				t.Errorf("%s imports %q; this package is standard-library only — the cobra flags, API client and process wiring belong in go/cmd/eshu's trace.go",
					name, path)
			}
		}
	}

	// A scan that read no files, or walked no imports, is not evidence.
	if scanned < 3 {
		t.Fatalf("scanned only %d non-test .go files; expected the package's sources to be present", scanned)
	}
	if imports < 4 {
		t.Fatalf("walked only %d imports; the parse is not reaching the import blocks", imports)
	}
}
