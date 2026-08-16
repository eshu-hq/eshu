// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package playbooks

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageStaysProcessNeutral is the standing guard behind the invariant
// AGENTS.md and doc.go both state: this package "declares no cobra flag, reads
// no environment variable, opens no file, and decides no exit status", and
// "never calls os.Exit or touches os.Stdout".
//
// Nothing enforced that. The package genuinely satisfies it today -- its whole
// import set is encoding/json, errors, fmt, io and strings -- but a claim with
// no test behind it is true only until someone changes the code, and the
// reviewer who would have to notice is reading a diff, not the README.
//
// Reading an environment variable, opening a file, exiting, and writing to
// os.Stdout all require the os package here, so refusing that import enforces
// all four clauses at once and keeps the check honest as new code arrives. The
// fmt.Print* rule is separate: those write to the process's stdout rather than
// to the io.Writer the caller hands in, which is the same boundary stated a
// different way.
func TestPackageStaysProcessNeutral(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	bannedImports := map[string]string{
		"os":                 `reads no environment variable, opens no file, and decides no exit status`,
		"github.com/spf13/c": `declares no cobra flag`,
	}
	bannedPrints := map[string]bool{"Print": true, "Printf": true, "Println": true}

	fset := token.NewFileSet()
	scanned, selectors := 0, 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Clean(name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++

		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for banned, clause := range bannedImports {
				if path == banned || strings.HasPrefix(path, banned) {
					t.Errorf("%s imports %q; the package %s — that belongs in go/cmd/eshu's playbooks.go",
						name, path, clause)
				}
			}
		}

		// Matched against parsed selector expressions, not raw file text, so a
		// doc comment that merely names fmt.Println does not trip the guard.
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
			if pkg.Name == "fmt" && bannedPrints[sel.Sel.Name] {
				t.Errorf("%s uses fmt.%s at %s; this package renders to the io.Writer it is handed, never to the process stdout",
					name, sel.Sel.Name, fset.Position(sel.Pos()))
			}
			return true
		})
	}

	// A scan that read no files, or walked no selectors, is not evidence. The
	// floors sit under the measured values (2 files, 17 selectors) so they catch
	// a walk that stopped reaching the code rather than ordinary drift.
	if scanned < 1 {
		t.Fatalf("scanned only %d non-test .go files; expected the package's sources to be present", scanned)
	}
	if selectors < 10 {
		t.Fatalf("walked only %d qualified selectors; the AST walk is not reaching the code", selectors)
	}
}
