// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctor

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
// package process-bound. They are matched against parsed selector expressions
// rather than raw file text, so a doc comment that merely NAMES one does not
// trip the guard while real code does.
//
// os.Stat and os.FileInfo are deliberately absent: this package inspects the
// filesystem through an injected Deps.Stat, and os is a legitimate dependency
// for the error values that seam returns. It is the process-bound selectors
// that are banned, which is precisely what a dependency scan cannot see.
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
// flag, touches no process stream, reads no environment, and never exits. The
// report goes to the io.Writer its caller supplies.
//
// Without this, that claim is only a sentence. A one-time `go list -deps |
// rg spf13` in a PR body proves the cobra half on the day it is run and then
// decays the moment the PR merges, and it cannot catch the os/fmt half at all
// because both are legitimate imports here.
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
				t.Errorf("%s imports cobra; flag handling belongs in go/cmd/eshu's doctor.go", name)
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
				t.Errorf("%s uses %s.%s at %s; process wiring belongs in go/cmd/eshu's doctor.go",
					name, pkg.Name, sel.Sel.Name, fset.Position(sel.Pos()))
			}
			return true
		})
	}

	// A scan that read nothing, or walked no selectors, is not evidence. These
	// floors are below the package's current counts and exist to fail loudly if
	// the walk stops reaching the code -- not to track its size.
	if scanned < 3 {
		t.Fatalf("scanned only %d .go files; expected the package's sources to be present", scanned)
	}
	if selectors < 40 {
		t.Fatalf("walked only %d qualified selectors; the AST walk is not reaching the code", selectors)
	}
}
