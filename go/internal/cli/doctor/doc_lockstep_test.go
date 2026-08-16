// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctor

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// allowedOSSelectors is the complete set of os identifiers this package's
// production code may use. It is a SET EQUALITY, not a deny-list, and that
// distinction is the whole point: a deny-list can only ever enumerate the
// failures someone already imagined, so os.LookupEnv, os.Environ, os.WriteFile
// or os.Create would each read the environment or write outside the caller's
// writer while passing a list that happened not to name them.
//
// Stat and FileInfo are what the Deps seam needs: Deps.Stat defaults to os.Stat
// and returns an os.FileInfo. ErrNotExist and friends appear only in tests.
var allowedOSSelectors = map[string]bool{
	"Stat":     true,
	"FileInfo": true,
}

// bannedFmtSelectors are the fmt calls that write to a process stream rather
// than to the caller's io.Writer. fmt is otherwise a legitimate dependency, so
// this one stays a deny-list -- the allowed surface (Fprintf, Fprintln,
// Errorf, Sprintf) is open-ended in a way os is not.
var bannedFmtSelectors = map[string]bool{
	"Print": true, "Printf": true, "Println": true,
}

// TestPackageStaysProcessNeutral is the standing guard behind the ownership
// claims doc.go, README.md and AGENTS.md all make: this package reads no cobra
// flag, reads no environment, touches no process stream, never exits, and
// writes only to the io.Writer its caller supplies.
//
// A one-time `go list -deps | rg spf13` in a PR body proves the cobra half on
// the day it is run and then decays the moment the PR merges. It cannot prove
// the os/fmt half at all, because both are legitimate imports here.
func TestPackageStaysProcessNeutral(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	usedOS := map[string]bool{}
	scanned, selectors := 0, 0

	for _, entry := range entries {
		name := entry.Name()
		// Production files only. Tests legitimately use os.ReadDir, os.WriteFile
		// and friends to describe a machine, and folding them in would make the
		// expected set so broad it would assert nothing.
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
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
			switch pkg.Name {
			case "os":
				usedOS[sel.Sel.Name] = true
				if !allowedOSSelectors[sel.Sel.Name] {
					t.Errorf("%s uses os.%s at %s; the package's os surface is fixed at %v -- "+
						"process state belongs in go/cmd/eshu's doctor.go",
						name, sel.Sel.Name, fset.Position(sel.Pos()), sortedKeys(allowedOSSelectors))
				}
			case "fmt":
				if bannedFmtSelectors[sel.Sel.Name] {
					t.Errorf("%s uses fmt.%s at %s; the report goes to the caller's io.Writer",
						name, sel.Sel.Name, fset.Position(sel.Pos()))
				}
			}
			return true
		})
	}

	// A scan that read nothing, or walked no selectors, is not evidence. Both
	// floors are MEASURED, not estimated: at the time of writing the package
	// has 2 production files and 46 qualified selectors. They sit below that
	// with margin so a legitimate refactor does not trip them, and exist only
	// to fail loudly if the walk stops reaching code -- for instance if someone
	// switches to parser.ImportsOnly, which stops at the import block and makes
	// ast.Inspect find nothing while every assertion above passes vacuously.
	if scanned < 1 {
		t.Fatalf("scanned %d production .go files; the walk found no sources at all", scanned)
	}
	if selectors < 30 {
		t.Fatalf("walked only %d qualified selectors (expected ~46); the AST walk is not reaching the code", selectors)
	}
	// The equality has two directions. The loop above fails on anything used
	// but not allowed; this fails on anything allowed but no longer used, so
	// the expected set cannot quietly rot into a list of identifiers the
	// package stopped needing years ago.
	for name := range allowedOSSelectors {
		if !usedOS[name] {
			t.Errorf("allowedOSSelectors lists os.%s but the package no longer uses it; "+
				"narrow the set rather than leaving it permissive", name)
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
