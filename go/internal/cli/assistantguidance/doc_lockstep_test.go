// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package assistantguidance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// processWiringSelectors are the qualified identifiers that would make this
// package process-bound. They are matched against parsed selector expressions,
// not raw file text, so a doc comment that merely NAMES one does not trip the
// guard while real code does.
var processWiringSelectors = map[string]map[string]bool{
	"os": {
		"Stdout": true, "Stderr": true, "Stdin": true,
		"Exit": true, "Getenv": true, "Getwd": true, "Args": true,
	},
	"fmt": {
		"Print": true, "Printf": true, "Println": true,
	},
}

// TestPackageStaysProcessNeutral is the standing guard behind the ownership
// claim doc.go, README.md, and AGENTS.md all make: this package reads no cobra
// flag, touches no process stream, reads no environment, and never exits.
// `go list -deps | rg spf13` proves the cobra half transitively; this test adds
// what a dependency scan cannot catch, because os and fmt are legitimate
// dependencies for file IO and formatting -- it is the specific process-bound
// selectors that are banned.
func TestPackageStaysProcessNeutral(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	scanned, selectors := 0, 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Clean(name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++

		for _, imp := range file.Imports {
			if strings.Contains(imp.Path.Value, "spf13/cobra") {
				t.Errorf("%s imports cobra; flag handling belongs in go/cmd/eshu's assistant.go", name)
			}
		}

		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			selectors++
			if names, tracked := processWiringSelectors[pkg.Name]; tracked && names[sel.Sel.Name] {
				t.Errorf("%s uses %s.%s at %s; process wiring belongs in go/cmd/eshu's assistant.go",
					name, pkg.Name, sel.Sel.Name, fset.Position(sel.Pos()))
			}
			return true
		})
	}

	// A scan that read nothing, or walked no selectors, is not evidence.
	if scanned < 4 {
		t.Fatalf("scanned only %d .go files; expected the package's sources to be present", scanned)
	}
	if selectors < 100 {
		t.Fatalf("walked only %d qualified selectors; the AST walk is not reaching the code", selectors)
	}
}
